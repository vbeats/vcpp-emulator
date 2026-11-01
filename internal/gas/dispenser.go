package gas

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
)

// 模拟器运行参数
const (
	tickInterval = 1 * time.Second // 状态机推进周期
	cmdChanSize  = 32              // 外部指令队列容量

	// 加油流速（L/min）：起机走大流量，接近目标自动收尾小流量，模拟跳枪前的减速
	rateGasoline = 45.0
	rateDiesel   = 60.0
	rateMethanol = 50.0
	rateMin      = 6.0  // 收尾最小流速
	rateJitter   = 0.06 // 流速随机波动幅度 ±6%

	pumpBaseStep = 1000.0 // 同一油机各枪起始泵码的间隔
)

// cmdKind 外部指令类型
type cmdKind int

const (
	cmdFuelStart cmdKind = iota // 开始加油（自动提枪）
	cmdFuelStop                 // 停止加油
	cmdLift                     // 提枪
	cmdHangUp                   // 挂枪
	cmdFault                    // 置故障
	cmdClearFault               // 清除故障
)

// command 外部指令
type command struct {
	kind     cmdKind
	req      StartFuelRequest
	nozzleNo string
}

// nozzle 单把油枪的运行状态，仅由所属油机的事件循环访问
type nozzle struct {
	cfg    store.NozzleConfig
	fuel   store.FuelOption
	price  float64
	status string
	lifted bool
	target FuelTarget
	order  *FuelOrder
	rate   float64 // 基准流速 L/min
	// pumpBase 为下一次加油的起始泵码，随每次结算累加，模拟加油机累计泵码
	pumpBase   float64
	tankVolume float64 // 油箱容积 L，用于「加满」自动跳枪
	startedAt  time.Time
	lastPush   time.Time
}

// Dispenser 单台加油机模拟器：一个事件循环 goroutine + 多把油枪。
// 加油机为本地模拟（不依赖 TCP 对端），加油数据经 Pusher 对外推送。
type Dispenser struct {
	cfg    store.DispenserConfig
	gas    store.GasConfig
	log    *logger.Bus
	pusher Pusher

	mu      sync.Mutex
	status  string
	running bool
	nozzles []*nozzle

	// 以下字段仅由事件循环访问
	cmds   chan command
	seq    int
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	notify func()
}

// NewDispenser 创建加油机模拟器，notify 用于状态变更时通知上层刷新界面
func NewDispenser(cfg store.DispenserConfig, gas store.GasConfig, log *logger.Bus, notify func(), pusher Pusher) *Dispenser {
	if notify == nil {
		notify = func() {}
	}
	if pusher == nil {
		pusher = &nonePusher{}
	}
	d := &Dispenser{
		cfg:    cfg,
		gas:    gas,
		log:    log,
		pusher: pusher,
		status: StatusOffline,
		cmds:   make(chan command, cmdChanSize),
		notify: notify,
	}
	d.initNozzles()
	return d
}

func (d *Dispenser) initNozzles() {
	count := d.cfg.NozzleCount
	if count <= 0 {
		count = 1
	}
	base := pumpSeed(d.cfg.ID + "|" + d.cfg.DispenserNo)
	d.nozzles = make([]*nozzle, 0, count)
	for i := 0; i < count; i++ {
		nc := store.NozzleConfig{No: strconv.Itoa(i + 1), FuelCode: store.FuelDefaultCode}
		if i < len(d.cfg.Nozzles) && strings.TrimSpace(d.cfg.Nozzles[i].FuelCode) != "" {
			nc.FuelCode = strings.TrimSpace(d.cfg.Nozzles[i].FuelCode)
		}
		d.nozzles = append(d.nozzles, newNozzle(nc, d.gas, base+float64(i)*pumpBaseStep))
	}
}

// newNozzle 按油号目录装配油枪运行态
func newNozzle(cfg store.NozzleConfig, gas store.GasConfig, pumpBase float64) *nozzle {
	fuel, ok := store.FuelByCode(cfg.FuelCode)
	if !ok {
		fuel, _ = store.FuelByCode(store.FuelDefaultCode)
		cfg.FuelCode = fuel.Code
	}
	return &nozzle{
		cfg:        cfg,
		fuel:       fuel,
		price:      round2(store.FuelPriceOf(gas.Prices, fuel.Code)),
		status:     NozzleIdle,
		target:     normalizeTarget(toFuelTarget(gas.LastStartFuel.Target)),
		rate:       baseRate(fuel.Category),
		pumpBase:   round4(pumpBase),
		tankVolume: gas.TankVolumeL,
	}
}

// Reconfigure 油站在运配置变更（油价 / 推送通道 / 枪数）时热更新，不打断进行中的加油。
// 油号变更仅在油枪空闲时生效，避免改写正在结算的订单油品。
func (d *Dispenser) Reconfigure(cfg store.DispenserConfig, gas store.GasConfig) {
	d.mu.Lock()
	d.cfg = cfg
	d.gas = gas
	if len(d.nozzles) != cfg.NozzleCount {
		d.initNozzles()
	} else {
		for i, n := range d.nozzles {
			n.price = round2(store.FuelPriceOf(gas.Prices, n.cfg.FuelCode))
			n.tankVolume = gas.TankVolumeL
			if i < len(cfg.Nozzles) {
				if f, ok := store.FuelByCode(cfg.Nozzles[i].FuelCode); ok {
					n.cfg.FuelCode = f.Code
					n.fuel = f
					n.price = round2(store.FuelPriceOf(gas.Prices, f.Code))
				}
			}
		}
	}
	d.mu.Unlock()
	d.notify()
}

// Start 启用加油机（进入事件循环）
func (d *Dispenser) Start() {
	d.mu.Lock()
	if d.cancel != nil {
		d.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx, d.cancel = ctx, cancel
	d.running = true
	d.status = StatusIdle
	d.mu.Unlock()

	d.wg.Add(1)
	go d.run(ctx)
}

// Stop 停用加油机：中断进行中的加油并结算后退出事件循环
func (d *Dispenser) Stop() {
	d.mu.Lock()
	cancel := d.cancel
	d.cancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	d.wg.Wait()

	d.mu.Lock()
	for _, n := range d.nozzles {
		if n.status == NozzleFueling {
			d.settle(n, StopPowerOff)
		}
		n.lifted = false
	}
	d.status = StatusOffline
	d.running = false
	d.mu.Unlock()
	d.notify()
}

// Submit 提交外部指令（非阻塞，队列满时丢弃）
func (d *Dispenser) Submit(c command) bool {
	select {
	case d.cmds <- c:
		return true
	default:
		return false
	}
}

// ---------------- 事件循环 ----------------

func (d *Dispenser) run(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-d.cmds:
			for _, item := range d.handleCommand(c) {
				d.dispatch(item)
			}
		case now := <-ticker.C:
			for _, item := range d.onTick(now) {
				d.dispatch(item)
			}
		}
	}
}

// pushItem 待投递的推送内容，由事件循环在解锁后统一投出
type pushItem struct {
	kind     string
	progress *FuelProgress
	order    *FuelOrder
}

// onTick 推进各枪加油状态并按需推送进度
func (d *Dispenser) onTick(now time.Time) []pushItem {
	var out []pushItem

	d.mu.Lock()
	fuelling := false
	interval := d.pushInterval()
	for _, n := range d.nozzles {
		if n.status != NozzleFueling || n.order == nil {
			continue
		}
		if reason := d.advance(n, tickInterval.Seconds(), now); reason != "" {
			d.settle(n, reason)
			snapshot := *n.order
			out = append(out, pushItem{kind: PushKindOrder, order: &snapshot})
			continue
		}
		fuelling = true
		if now.Sub(n.lastPush) >= interval {
			n.lastPush = now
			p := d.buildProgress(n, now)
			out = append(out, pushItem{kind: PushKindProgress, progress: &p})
		}
	}
	switch {
	case fuelling:
		d.status = StatusFueling
	case d.status != StatusFault:
		d.status = StatusIdle
	}
	d.mu.Unlock()

	d.notify()
	return out
}

// advance 按瞬时流速累加升数与金额，返回非空停止原因表示已达到加油目标
func (d *Dispenser) advance(n *nozzle, dt float64, now time.Time) string {
	o := n.order
	target := d.targetVolume(n)
	dv := currentRate(n) * dt / 60

	done := false
	if target > 0 && o.Volume+dv >= target {
		dv = target - o.Volume
		done = true
	}
	o.Volume = round4(o.Volume + dv)
	o.Amount = round2(o.Volume * o.Price)
	o.PumpEnd = round4(n.pumpBase + o.Volume)
	o.Seconds = int(now.Sub(n.startedAt).Seconds())
	if target > 0 {
		o.Progress = clamp01(o.Volume / target)
	}
	if done {
		return stopReasonOf(n.target)
	}
	return ""
}

// targetVolume 目标升数：定额按金额折算，加满按油箱容积
func (d *Dispenser) targetVolume(n *nozzle) float64 {
	switch n.target.Mode {
	case TargetAmount:
		if n.price <= 0 || n.target.Amount <= 0 {
			return 0
		}
		return round4(n.target.Amount / n.price)
	case TargetVolume:
		if n.target.Volume <= 0 {
			return 0
		}
		return n.target.Volume
	default:
		if n.tankVolume <= 0 {
			return 0
		}
		return n.tankVolume
	}
}

// pushInterval 加油过程推送周期
func (d *Dispenser) pushInterval() time.Duration {
	sec := d.gas.PushIntervalSec
	if sec <= 0 {
		sec = 5
	}
	return time.Duration(sec) * time.Second
}

// handleCommand 处理外部指令，返回本次产生的待推送内容
func (d *Dispenser) handleCommand(c command) []pushItem {
	var out []pushItem

	d.mu.Lock()
	defer d.mu.Unlock()

	n := d.nozzleByNo(c.nozzleNo)
	if n == nil {
		d.log.Errorf("油枪 %s 不存在", c.nozzleNo)
		return nil
	}
	switch c.kind {
	case cmdFuelStart:
		if p, ok := d.startFueling(n, c.req); ok {
			out = append(out, pushItem{kind: PushKindProgress, progress: &p})
		}
	case cmdFuelStop:
		if o, ok := d.stopFueling(n); ok {
			out = append(out, pushItem{kind: PushKindOrder, order: &o})
		}
	case cmdLift:
		if n.status == NozzleFueling {
			return nil
		}
		n.lifted = true
		n.status = NozzleLifted
	case cmdHangUp:
		if o, ok := d.hangUp(n); ok {
			out = append(out, pushItem{kind: PushKindOrder, order: &o})
		}
	case cmdFault:
		if n.status == NozzleFueling {
			if o, ok := d.stopFueling(n); ok {
				out = append(out, pushItem{kind: PushKindOrder, order: &o})
			}
		}
		n.status = NozzleFault
	case cmdClearFault:
		if n.status == NozzleFault {
			n.status = NozzleIdle
			n.lifted = false
		}
	}
	return out
}

// startFueling 自动提枪并开始加油，同时推送第一条进度
func (d *Dispenser) startFueling(n *nozzle, req StartFuelRequest) (FuelProgress, bool) {
	if n.status == NozzleFueling {
		d.log.Warnf("枪 %s 正在加油，忽略重复启动", n.cfg.No)
		return FuelProgress{}, false
	}
	now := time.Now()
	n.lifted = true
	n.status = NozzleFueling
	n.startedAt = now
	n.lastPush = now
	n.target = normalizeTarget(req.Target)
	n.rate = baseRate(n.fuel.Category)
	n.order = &FuelOrder{
		OrderNo:     d.nextOrderNo(n.cfg.No),
		DispenserNo: d.cfg.DispenserNo,
		NozzleNo:    n.cfg.No,
		FuelCode:    n.fuel.Code,
		FuelName:    n.fuel.Name,
		FuelColor:   n.fuel.Color,
		Price:       n.price,
		PumpStart:   round4(n.pumpBase),
		PumpEnd:     round4(n.pumpBase),
		StartAt:     now.Format("15:04:05"),
		Status:      OrderFueling,
		CardNo:      strings.TrimSpace(req.CardNo),
		PlateNo:     strings.TrimSpace(req.PlateNo),
	}
	d.log.Frame(logger.LevelInfo, d.cfg.DispenserNo, nozzleInt(n.cfg.No), "TX", "开始加油",
		fmt.Sprintf("枪 %s · %s · 单价 %.2f 元/L · 目标 %s", n.cfg.No, n.fuel.Name, n.price, targetText(n.target)), "")
	return d.buildProgress(n, now), true
}

// stopFueling 手动停止加油并结算
func (d *Dispenser) stopFueling(n *nozzle) (FuelOrder, bool) {
	switch n.status {
	case NozzleFueling:
		d.settle(n, StopManual)
		return *n.order, true
	case NozzleLifted:
		n.lifted = false
		n.status = NozzleIdle
	}
	return FuelOrder{}, false
}

// hangUp 挂枪：正在加油时先结算，随后回到挂枪待机
func (d *Dispenser) hangUp(n *nozzle) (FuelOrder, bool) {
	if n.status == NozzleFueling {
		d.settle(n, StopManual)
		return *n.order, true
	}
	n.lifted = false
	n.status = NozzleIdle
	return FuelOrder{}, false
}

// settle 结算订单：补全结束时间 / 结束泵码并推送订单数据
func (d *Dispenser) settle(n *nozzle, reason string) {
	o := n.order
	if o == nil {
		return
	}
	now := time.Now()
	o.Status = OrderSettled
	o.StopReason = reason
	o.EndAt = now.Format("15:04:05")
	if o.Seconds <= 0 {
		o.Seconds = int(now.Sub(n.startedAt).Seconds())
	}
	o.Volume = round4(o.Volume)
	o.Amount = round2(o.Volume * o.Price)
	o.PumpEnd = round4(o.PumpStart + o.Volume)
	o.Progress = 1

	n.status = NozzleSettled
	n.pumpBase = round4(o.PumpEnd)
	d.log.Frame(logger.LevelInfo, d.cfg.DispenserNo, nozzleInt(n.cfg.No), "TX", "加油完成",
		fmt.Sprintf("枪 %s · %.4f L · %.2f 元 · 泵码 %.4f → %.4f · %s",
			o.NozzleNo, o.Volume, o.Amount, o.PumpStart, o.PumpEnd, reason), "")
}

// buildProgress 组装加油进度报文（需在锁内调用）
func (d *Dispenser) buildProgress(n *nozzle, now time.Time) FuelProgress {
	o := n.order
	target := d.targetVolume(n)
	prog := 0.0
	if target > 0 {
		prog = clamp01(o.Volume / target)
	}
	return FuelProgress{
		DispenserNo: d.cfg.DispenserNo,
		NozzleNo:    n.cfg.No,
		FuelCode:    n.fuel.Code,
		FuelName:    n.fuel.Name,
		FuelColor:   n.fuel.Color,
		Volume:      round4(o.Volume),
		Amount:      round2(o.Amount),
		PumpStart:   round4(o.PumpStart),
		PumpCode:    round4(o.PumpEnd),
		Price:       round2(o.Price),
		Seconds:     o.Seconds,
		Progress:    prog,
		Status:      n.status,
		Time:        now.Format("15:04:05"),
	}
}

// dispatch 投递待推送内容，屏蔽「尚未实现实际网络发送」的预留状态
func (d *Dispenser) dispatch(item pushItem) {
	var err error
	switch item.kind {
	case PushKindProgress:
		if item.progress != nil {
			err = d.pusher.PushProgress(*item.progress)
		}
	case PushKindOrder:
		if item.order != nil {
			err = d.pusher.PushOrder(*item.order)
		}
	}
	if err != nil && !errors.Is(err, ErrNotImplemented) {
		d.log.Errorf("推送加油数据失败: %v", err)
	}
}

// nextOrderNo 订单号：日期时间(14) + 枪号(2) + 序号(3)
func (d *Dispenser) nextOrderNo(nozzleNo string) string {
	d.seq++
	seq := d.seq
	if seq > 999 {
		seq = seq % 1000
	}
	return fmt.Sprintf("%s%02d%03d", time.Now().Format("20060102150405"), nozzleInt(nozzleNo), seq)
}

func (d *Dispenser) nozzleByNo(no string) *nozzle {
	for _, n := range d.nozzles {
		if n.cfg.No == no {
			return n
		}
	}
	return nil
}

// ---------------- 状态快照 ----------------

// Snapshot 生成前端展示用的油机状态快照
func (d *Dispenser) Snapshot() DispenserView {
	d.mu.Lock()
	defer d.mu.Unlock()

	nozzles := make([]NozzleView, 0, len(d.nozzles))
	for _, n := range d.nozzles {
		nozzles = append(nozzles, n.view())
	}
	return DispenserView{
		ID:          d.cfg.ID,
		DispenserNo: d.cfg.DispenserNo,
		Status:      d.status,
		StatusText:  dispenserStatusText(d.status),
		NozzleCount: len(d.nozzles),
		UpdatedAt:   nowStamp(),
		Nozzles:     nozzles,
	}
}

func (n *nozzle) view() NozzleView {
	v := NozzleView{
		No:         n.cfg.No,
		Status:     n.status,
		StatusText: nozzleStatusText(n.status),
		Lifted:     n.lifted,
		FuelCode:   n.fuel.Code,
		FuelName:   n.fuel.Name,
		FuelColor:  n.fuel.Color,
		Price:      round2(n.price),
		Target:     n.target,
	}
	if n.order != nil {
		o := n.order
		v.Volume = round4(o.Volume)
		v.Amount = round2(o.Amount)
		v.PumpStart = round4(o.PumpStart)
		v.PumpEnd = round4(o.PumpEnd)
		v.Seconds = o.Seconds
		v.Progress = o.Progress
		v.OrderNo = o.OrderNo
		v.StopReason = o.StopReason
		v.StartAt = o.StartAt
		v.EndAt = o.EndAt
		v.CardNo = o.CardNo
		v.PlateNo = o.PlateNo
		v.Settled = o.Status == OrderSettled
	}
	if n.status == NozzleFueling {
		v.FlowRate = round2(currentRate(n))
	}
	return v
}

// currentRate 含收尾减速与随机波动的瞬时流速
func currentRate(n *nozzle) float64 {
	rate := n.rate
	if target := targetVolumeOf(n); target > 0 {
		prog := clamp01(n.order.Volume / target)
		rate = math.Max(rateMin, n.rate*(1-math.Pow(prog, 3)*0.85))
	}
	return rate * (1 + (rand.Float64()-0.5)*2*rateJitter)
}

// targetVolumeOf 油枪目标升数（供瞬时流速计算，逻辑与 Dispenser.targetVolume 一致）
func targetVolumeOf(n *nozzle) float64 {
	switch n.target.Mode {
	case TargetAmount:
		if n.price <= 0 || n.target.Amount <= 0 {
			return 0
		}
		return n.target.Amount / n.price
	case TargetVolume:
		return n.target.Volume
	default:
		return n.tankVolume
	}
}

// ---------------- 展示文案 ----------------

func dispenserStatusText(s string) string {
	switch s {
	case StatusOffline:
		return "未启用"
	case StatusIdle:
		return "待机"
	case StatusFueling:
		return "加油中"
	case StatusFault:
		return "故障"
	default:
		return s
	}
}

func nozzleStatusText(s string) string {
	switch s {
	case NozzleIdle:
		return "挂枪待机"
	case NozzleLifted:
		return "已提枪"
	case NozzleFueling:
		return "加油中"
	case NozzleSettled:
		return "已结算"
	case NozzleFault:
		return "故障"
	default:
		return s
	}
}

func targetText(t FuelTarget) string {
	switch t.Mode {
	case TargetAmount:
		return fmt.Sprintf("定额 %.2f 元", t.Amount)
	case TargetVolume:
		return fmt.Sprintf("定量 %.2f L", t.Volume)
	default:
		return "加满"
	}
}

// stopReasonOf 按加油目标给出自动停止原因
func stopReasonOf(t FuelTarget) string {
	switch t.Mode {
	case TargetAmount:
		return StopAmount
	case TargetVolume:
		return StopVolume
	default:
		return StopTankFull
	}
}

// toFuelTarget 把配置层的加油目标转换为模拟器内部目标（两层各自定义以保持分层边界）
func toFuelTarget(t store.FuelTarget) FuelTarget {
	return FuelTarget{Mode: t.Mode, Amount: t.Amount, Volume: t.Volume}
}

// normalizeTarget 修补加油目标取值，非法模式回退加满
func normalizeTarget(t FuelTarget) FuelTarget {
	switch t.Mode {
	case TargetFree, TargetAmount, TargetVolume:
	default:
		t.Mode = TargetFree
	}
	if t.Amount < 0 {
		t.Amount = 0
	}
	if t.Volume < 0 {
		t.Volume = 0
	}
	return t
}

// baseRate 按油品类别给出基准流速（L/min）
func baseRate(category string) float64 {
	switch category {
	case "柴油":
		return rateDiesel
	case "甲醇":
		return rateMethanol
	default:
		return rateGasoline
	}
}

// nozzleInt 枪号字符串转数字，用于日志排序与订单号拼接
func nozzleInt(no string) int {
	v, err := strconv.Atoi(no)
	if err != nil {
		return 0
	}
	return v
}

// pumpSeed 由油机标识派生稳定的起始泵码基数，保证重启后泵码不跳变
func pumpSeed(key string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return float64(h.Sum32()%90000) + 10000
}

func nowStamp() string { return time.Now().Format("15:04:05") }

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
