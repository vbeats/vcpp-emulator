package emu

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"sync"
	"time"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
	"vcpp-emulator/internal/ykc"
)

// 流程超时与重试策略
const (
	dialTimeout      = 5 * time.Second
	writeTimeout     = 5 * time.Second
	loginTimeout     = 10 * time.Second
	startAckTimeout  = 30 * time.Second
	tradeAckTimeout  = 5 * time.Second
	tradeMaxRetry    = 3
	tradeRetryAfter  = 30 * time.Second
	tickInterval     = 1 * time.Second
	eventChanSize    = 256
	cmdChanSize      = 32
	readBufSize      = 4096
)

// defaultReconnectSec 断线自动重连的默认间隔（配置缺省或越界时使用）
const defaultReconnectSec = 30

// cmdKind 外部指令类型
type cmdKind int

const (
	cmdStart cmdKind = iota
	cmdStop
	cmdPlug
	cmdUnplug
	cmdFault
	cmdClearFault
)

// command 外部指令
type command struct {
	kind cmdKind
	req  StartRequest
	gun  int
}

// gun 单枪运行状态
type gun struct {
	no            byte
	status        string
	plugged       bool
	session       *Session
	lastResult    *Session // 最近一次充电的结算快照，会话释放后仍可展示
	lastHeartbeat time.Time
	lastRealTime  time.Time
	maxVoltage    float64
	maxCurrent    float64
	dirty         bool // 状态变位，需要立即上送实时数据
}

// Pile 单个充电桩模拟器：一条 TCP 连接 + 多把枪
type Pile struct {
	cfg    store.PileConfig
	server store.ServerConfig
	spec   *ykc.Spec
	log    *logger.Bus

	mu         sync.Mutex
	status     string
	lastError  string
	loggedIn   bool
	connected  bool // 已建立 TCP 连接，供退出时统计与销毁
	sentFrames int64
	recvFrames int64
	billing    *ykc.BillingModel
	connectedAt *time.Time
	guns       []*gun

	// 以下字段仅由运行协程访问
	conn   net.Conn
	seq    uint16
	events chan ykc.Frame
	cmds   chan command

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	notify func()
}

// NewPile 创建充电桩模拟器，notify 用于状态变更时通知上层刷新界面
func NewPile(cfg store.PileConfig, server store.ServerConfig, log *logger.Bus, notify func()) *Pile {
	spec := ykc.GetSpec(ykc.VersionCode(server.Version))
	if notify == nil {
		notify = func() {}
	}
	p := &Pile{
		cfg:    cfg,
		server: server,
		spec:   spec,
		log:    log,
		status: StatusOffline,
		events: make(chan ykc.Frame, eventChanSize),
		cmds:   make(chan command, cmdChanSize),
		notify: notify,
	}
	p.initGuns()
	return p
}

func (p *Pile) initGuns() {
	count := p.cfg.GunCount
	if count <= 0 {
		count = 1
	}
	if count > 16 {
		count = 16
	}
	// 直流桩 400V 平台 / 交流桩 220V
	maxVoltage, maxCurrent := 440.0, 250.0
	if p.cfg.PileType == 1 {
		maxVoltage, maxCurrent = 220.0, 32.0
	}
	p.guns = make([]*gun, 0, count)
	for i := 0; i < count; i++ {
		p.guns = append(p.guns, &gun{
			no:         byte(i + 1),
			status:     GunIdle,
			maxVoltage: maxVoltage,
			maxCurrent: maxCurrent,
		})
	}
}

// Addr 连接地址
func (p *Pile) Addr() string {
	return net.JoinHostPort(p.server.Host, strconv.Itoa(p.server.Port))
}

// Start 启动桩（建立连接并进入事件循环）
func (p *Pile) Start(appCfg store.AppConfig) {
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx, p.cancel = ctx, cancel
	p.status = StatusConnecting
	p.loggedIn = false
	p.lastError = ""
	p.mu.Unlock()

	p.wg.Add(1)
	go p.run(ctx, appCfg)
}

// Stop 停止桩（断开连接并释放协程）
func (p *Pile) Stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.wg.Wait()
	p.mu.Lock()
	p.status = StatusOffline
	p.loggedIn = false
	p.connected = false
	p.connectedAt = nil
	p.mu.Unlock()
}

// IsConnected 是否已建立 TCP 连接
func (p *Pile) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connected
}

// IsRunning 是否在运行
func (p *Pile) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancel != nil
}

// Submit 提交外部指令（非阻塞，队列满时丢弃）
func (p *Pile) Submit(c command) bool {
	select {
	case p.cmds <- c:
		return true
	default:
		return false
	}
}

// ---------------- 运行主循环 ----------------

func (p *Pile) run(ctx context.Context, appCfg store.AppConfig) {
	defer p.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		serveCtx, serveCancel := context.WithCancel(ctx)
		p.serveOnce(serveCtx, appCfg)
		serveCancel()

		if ctx.Err() != nil {
			return
		}
		if !appCfg.AutoReconnect {
			p.setState(StatusOffline, "连接已断开")
			return
		}
		p.setState(StatusConnecting, "等待重连")
		wait := time.Duration(appCfg.ReconnectSec) * time.Second
		if wait <= 0 {
			wait = defaultReconnectSec * time.Second
		}
		p.log.Infof("桩 %s 连接中断，%.0f 秒后自动重连 %s", p.cfg.PileNo, wait.Seconds(), p.Addr())
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// serveOnce 完成一次「连接 → 登录 → 业务交互」，返回时连接必然已关闭
func (p *Pile) serveOnce(ctx context.Context, appCfg store.AppConfig) {
	addr := p.Addr()
	p.log.Infof("桩 %s 正在连接 %s（%s）", p.cfg.PileNo, addr, p.spec.Desc)

	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		p.log.Errorf("桩 %s 连接失败: %v", p.cfg.PileNo, err)
		p.setState(StatusError, err.Error())
		return
	}
	p.conn = conn
	now := time.Now()
	p.mu.Lock()
	p.connected = true
	p.connectedAt = &now
	p.mu.Unlock()

	// 读协程：只负责拆帧与投递，保障退出时不会泄漏
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		p.readLoop(ctx, conn)
	}()

	// 退出前确保连接与读协程完全释放
	defer func() {
		_ = conn.Close()
		<-readDone
		p.conn = nil
		p.mu.Lock()
		p.loggedIn = false
		p.connected = false
		p.connectedAt = nil
		p.mu.Unlock()
		p.log.Infof("桩 %s 连接已断开 %s", p.cfg.PileNo, addr)
	}()

	if !p.login(ctx) {
		return
	}
	p.setState(StatusOnline, "")
	p.log.Infof("桩 %s 登录成功，枪数量 %d", p.cfg.PileNo, len(p.guns))
	p.serveLoop(ctx, appCfg)
}

// readLoop 读取 TCP 数据并拆帧投递到事件队列
func (p *Pile) readLoop(ctx context.Context, conn net.Conn) {
	reader := ykc.NewFrameReader(p.spec)
	buf := make([]byte, readBufSize)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := conn.Read(buf)
		if n > 0 {
			frames, _ := reader.Feed(buf[:n])
			for _, f := range frames {
				p.recvFrames++
				p.pushEvent(f)
			}
		}
		if err == nil {
			continue
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		// 连接关闭或异常：清空缓冲，结束读协程
		reader.Reset()
		return
	}
}

// pushEvent 投递下行帧，队列满时丢弃最旧的一条，避免阻塞读协程
func (p *Pile) pushEvent(f ykc.Frame) {
	select {
	case p.events <- f:
	default:
		select {
		case <-p.events:
		default:
		}
		select {
		case p.events <- f:
		default:
		}
	}
}

// login 发送登录认证并等待应答
func (p *Pile) login(ctx context.Context) bool {
	body := p.spec.BuildLogin(ykc.LoginInfo{
		PileNo:     p.cfg.PileNo,
		PileType:   byte(p.cfg.PileType),
		GunCount:   byte(len(p.guns)),
		ProgramVer: p.cfg.ProgramVer,
		NetType:    1,
		Sim:        "89860123456789012345",
		Supplier:   4,
		Token:      p.cfg.PileNo,
		Phone:      "13800138000",
		Secret:     "",
	})
	if err := p.send(ykc.CmdLogin, body, "登录认证"); err != nil {
		p.setState(StatusError, err.Error())
		return false
	}

	deadline := time.Now().Add(loginTimeout)
	for {
		wait := time.Until(deadline)
		if wait <= 0 {
			p.setState(StatusError, "登录应答超时")
			p.log.Errorf("桩 %s 登录应答超时", p.cfg.PileNo)
			return false
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case f := <-p.events:
			timer.Stop()
			if f.Cmd == ykc.CmdLoginAck {
				ack := ykc.ParseLoginAck(f.Body)
				if ack.Result == 0x00 {
					p.mu.Lock()
					p.loggedIn = true
					p.mu.Unlock()
					p.log.Infof("桩 %s 平台时间: %s", p.cfg.PileNo, ack.Time.Format("2006-01-02 15:04:05"))
					return true
				}
				p.setState(StatusError, "平台拒绝登录")
				p.log.Errorf("桩 %s 登录被拒绝(结果码 0x%02X)", p.cfg.PileNo, ack.Result)
				return false
			}
			// 登录前的其它下行帧直接处理
			p.handleDown(f)
		case <-timer.C:
			p.setState(StatusError, "登录应答超时")
			p.log.Errorf("桩 %s 登录应答超时", p.cfg.PileNo)
			return false
		}
	}
}

// serveLoop 业务主循环：定时上送 + 下行帧处理 + 外部指令
func (p *Pile) serveLoop(ctx context.Context, appCfg store.AppConfig) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	p.requestBillingModel()
	last := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case f := <-p.events:
			p.handleDown(f)
		case c := <-p.cmds:
			p.handleCommand(c)
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			p.onTick(now, dt, appCfg)
		}
	}
}

// onTick 周期任务：推进充电模拟、上送心跳与实时数据、补发交易记录
func (p *Pile) onTick(now time.Time, dt float64, appCfg store.AppConfig) {
	p.mu.Lock()
	billing := p.billing
	guns := p.guns
	p.mu.Unlock()

	price := 0.0
	seg := 0
	if billing != nil {
		price = billing.PriceAt(now)
		seg = billing.SegmentAt(now)
	}

	heartbeat := time.Duration(appCfg.HeartbeatSec) * time.Second
	if heartbeat <= 0 {
		heartbeat = 10 * time.Second
	}
	idlePeriod := time.Duration(appCfg.RealTimeIdleSec) * time.Second
	if idlePeriod <= 0 {
		idlePeriod = 300 * time.Second
	}
	chargePeriod := time.Duration(appCfg.RealTimeChargeSec) * time.Second
	if chargePeriod <= 0 {
		chargePeriod = 15 * time.Second
	}

	for _, g := range guns {
		// 心跳：每枪独立周期，错开 200ms 避免同帧突发
		if now.Sub(g.lastHeartbeat) >= heartbeat {
			g.lastHeartbeat = now.Add(time.Duration(g.no) * 200 * time.Millisecond)
			p.sendHeartbeat(g)
		}

		// 充电过程推进
		if g.session != nil && g.session.Phase == PhaseCharging {
			g.session.Advance(dt, price, seg, p.cfg.RatedPower)
			if done, reason := g.session.CheckTarget(); done {
				p.finishCharge(g, reason)
			}
		}

		// 实时数据：充电中按短周期，空闲按长周期，状态变位立即上送
		period := idlePeriod
		charging := g.session != nil && g.session.Phase == PhaseCharging
		if charging {
			period = chargePeriod
		}
		if g.dirty || now.Sub(g.lastRealTime) >= period {
			g.lastRealTime = now
			g.dirty = false
			p.sendRealTime(g)
		}

		// 启动确认超时
		if g.session != nil && g.session.Phase == PhaseWaitAck && now.After(g.session.WaitUntil) {
			p.log.Errorf("桩 %s 枪 %d 启动确认超时，取消本次充电", p.cfg.PileNo, g.no)
			p.mu.Lock()
			g.session = nil
			g.status = GunPlugged
			p.mu.Unlock()
			p.notify()
		}

		// 交易记录补发（未收到平台确认）
		if g.session != nil && g.session.Phase == PhaseStopping &&
			now.After(g.session.TradeSentAt.Add(tradeRetryAfter)) {
			if g.session.TradeRetry >= tradeMaxRetry {
				p.log.Warnf("桩 %s 枪 %d 交易记录确认超时已达上限，结束本次会话", p.cfg.PileNo, g.no)
				p.mu.Lock()
				g.session = nil
				g.status = GunPlugged
				p.mu.Unlock()
				p.notify()
				continue
			}
			p.sendTrade(g)
		}
	}

	// 存在进行中的会话时周期性刷新界面（管理器内部有节流）
	for _, g := range guns {
		if g.session != nil {
			p.notify()
			break
		}
	}
}

// ---------------- 上行报文 ----------------

func (p *Pile) send(cmd byte, body []byte, desc string) error {
	if p.conn == nil {
		return fmt.Errorf("连接未建立")
	}
	p.seq++
	raw := p.spec.Encode(p.seq, cmd, body, time.Now())
	// 写超时保证退出时不会因为对端不读而卡住连接销毁
	_ = p.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := p.conn.Write(raw); err != nil {
		p.log.Errorf("桩 %s 发送 0x%02X(%s) 失败: %v", p.cfg.PileNo, cmd, desc, err)
		return err
	}
	p.sentFrames++
	p.log.Frame(logger.LevelInfo, p.cfg.PileNo, 0, "TX", fmt.Sprintf("0x%02X %s", cmd, desc), desc, ykc.ToHex(raw))
	return nil
}

func (p *Pile) sendHeartbeat(g *gun) {
	status := byte(0x00)
	p.mu.Lock()
	if g.status == GunFault {
		status = 0x01
	}
	p.mu.Unlock()
	body := ykc.BuildHeartbeat(p.cfg.PileNo, g.no, status)
	_ = p.send(ykc.CmdHeartbeat, body, fmt.Sprintf("心跳 枪%d", g.no))
}

// requestBillingModel 登录后请求计费模型
func (p *Pile) requestBillingModel() {
	body := ykc.BuildBillingVerify(p.cfg.PileNo, 0x0100)
	_ = p.send(ykc.CmdBillingVerify, body, "计费模型验证请求")
}

func (p *Pile) sendBillingRequest() {
	body := p.spec.BuildBillingRequest(p.cfg.PileNo, 0x0100)
	_ = p.send(ykc.CmdBillingModel, body, "计费模型请求")
}

// sendRealTime 上送实时监测数据
func (p *Pile) sendRealTime(g *gun) {
	p.mu.Lock()
	s := g.session
	status := byte(ykc.GunStatusIdle)
	switch g.status {
	case GunFault:
		status = ykc.GunStatusFault
	case GunCharging:
		status = ykc.GunStatusCharging
	}
	if !p.loggedIn {
		status = ykc.GunStatusOffline
	}
	plugged := byte(0x00)
	if g.plugged || s != nil {
		plugged = 0x01
	}
	data := ykc.RealTimeData{
		TradeNo:     "",
		PileNo:      p.cfg.PileNo,
		GunNo:       g.no,
		Status:      status,
		GunHome:     ykc.GunHomeUnkown,
		Plugged:     plugged,
		GunTemp:     0,
		BatteryMaxTemp: 0,
		Soc:         0,
		RemainMinutes: 0,
	}
	if s != nil {
		data.TradeNo = s.TradeNo
		data.Voltage = s.Voltage
		data.Current = s.Current
		data.GunTemp = s.GunTemp
		data.BatteryMaxTemp = s.BatteryTemp
		data.Soc = int(s.Soc)
		data.ChargeMinutes = s.Minutes()
		data.RemainMinutes = s.RemainMinutes()
		data.Energy = s.Energy
		data.LossEnergy = s.LossEnergy
		data.Amount = s.Amount
		data.MeterValue = s.MeterEnd
		data.PileTemp = 30
		data.Smoke = 0x02
	}
	p.mu.Unlock()

	body := p.spec.BuildRealTimeData(data)
	_ = p.send(ykc.CmdRealTimeData, body, fmt.Sprintf("实时数据 枪%d", g.no))
}

// startCharge 发起启动充电申请
func (p *Pile) startCharge(g *gun, req StartRequest) {
	p.mu.Lock()
	if g.session != nil {
		p.mu.Unlock()
		p.log.Warnf("桩 %s 枪 %d 正在充电，忽略本次启动", p.cfg.PileNo, g.no)
		return
	}
	if p.status != StatusOnline {
		p.mu.Unlock()
		p.log.Warnf("桩 %s 未在线，无法启动充电", p.cfg.PileNo)
		return
	}
	g.plugged = true
	g.status = GunStarting
	p.mu.Unlock()

	tradeNo := p.nextTradeNo(g.no)
	target := req.Target
	battery := req.BatteryKwh
	if battery <= 0 {
		battery = 60
	}
	startSoc := req.StartSoc
	if startSoc <= 0 {
		startSoc = 20
	}
	p.mu.Lock()
	if req.MaxVoltage > 0 {
		g.maxVoltage = req.MaxVoltage
	}
	if req.MaxCurrent > 0 {
		g.maxCurrent = req.MaxCurrent
	}
	s := NewSession(tradeNo, req.StartType, req.CardNo, req.Vin, target,
		battery, startSoc, g.maxVoltage, g.maxCurrent, p.meterBase())
	g.session = s
	g.dirty = true
	p.mu.Unlock()

	body := ykc.BuildApplyCharge(ykc.ApplyCharge{
		TradeNo:   tradeNo,
		PileNo:    p.cfg.PileNo,
		GunNo:     g.no,
		StartType: req.StartType,
		NeedPwd:   0,
		CardNo:    req.CardNo,
		Vin:       req.Vin,
	})
	_ = p.send(p.spec.ApplyCmd, body, fmt.Sprintf("申请启动充电 枪%d 流水号 %s", g.no, tradeNo))
	p.log.Infof("桩 %s 枪 %d 发起启动(%s)，目标 %s", p.cfg.PileNo, g.no,
		startTypeDesc(req.StartType), targetDesc(target))
	p.notify()
}

// finishCharge 结束充电并上送交易记录
func (p *Pile) finishCharge(g *gun, reason byte) {
	p.mu.Lock()
	s := g.session
	if s == nil {
		p.mu.Unlock()
		return
	}
	s.EndTime = time.Now()
	s.StopReason = reason
	s.Phase = PhaseStopping
	g.status = GunStopping
	g.lastResult = s
	g.dirty = true
	p.mu.Unlock()

	p.log.Infof("桩 %s 枪 %d 结束充电：%s，累计 %.4f kWh / %.2f 元 / %d 分钟 / SOC %d%%",
		p.cfg.PileNo, g.no, ykc.StopReasonDesc(reason), s.Energy, s.Amount, s.Minutes(), int(s.Soc))
	p.notify()
	p.sendRealTime(g)
	p.sendTrade(g)
}

// sendTrade 上送交易记录
func (p *Pile) sendTrade(g *gun) {
	p.mu.Lock()
	s := g.session
	billing := p.billing
	p.mu.Unlock()
	if s == nil {
		return
	}
	s.TradeRetry++
	s.TradeSentAt = time.Now()

	tradeType := ykc.TradeTypeCard
	if s.StartType == ykc.StartTypeVin {
		tradeType = ykc.TradeTypeVin
	}
	rec := ykc.TradeRecord{
		TradeNo:     s.TradeNo,
		PileNo:      p.cfg.PileNo,
		GunNo:       g.no,
		StartTime:   s.StartTime,
		EndTime:     s.EndTime,
		MeterNo:     "20260501",
		Rates:       s.BuildRates(billing),
		MeterStart:  s.MeterStart,
		MeterEnd:    s.MeterEnd,
		TotalEnergy: s.Energy,
		LossEnergy:  s.LossEnergy,
		Amount:      s.Amount,
		Vin:         s.Vin,
		TradeType:   tradeType,
		TradeTime:   time.Now(),
		StopReason:  s.StopReason,
		HalfHourKwh: s.HalfHour,
	}
	body := p.spec.BuildTrade(rec)
	_ = p.send(p.spec.TradeCmd, body, fmt.Sprintf("交易记录 枪%d 流水号 %s", g.no, s.TradeNo))
	p.notify()
}

// ---------------- 下行报文处理 ----------------

func (p *Pile) handleDown(f ykc.Frame) {
	desc := ykc.CmdDesc(f.Cmd)
	p.log.Frame(logger.LevelInfo, p.cfg.PileNo, 0, "RX", fmt.Sprintf("0x%02X %s", f.Cmd, desc), desc, "")

	switch f.Cmd {
	case ykc.CmdLoginAck:
		ack := ykc.ParseLoginAck(f.Body)
		p.log.Infof("桩 %s 登录应答：结果 0x%02X", p.cfg.PileNo, ack.Result)
	case ykc.CmdHeartbeatAck:
		// 心跳应答无需处理
	case ykc.CmdBillingVerifyAck:
		modelNo, result := ykc.ParseBillingVerifyAck(f.Body)
		p.log.Infof("桩 %s 计费模型验证应答：编号 0x%04X 结果 %d", p.cfg.PileNo, modelNo, result)
		if result != 0x00 {
			p.sendBillingRequest()
		}
	case ykc.CmdBillingModelAck:
		model := p.spec.ParseBillingModel(f.Body)
		p.mu.Lock()
		p.billing = model
		p.mu.Unlock()
		p.log.Infof("桩 %s 计费模型：%s", p.cfg.PileNo, model.Describe())
		p.notify()
	case ykc.CmdBillingSetting:
		model := p.spec.ParseBillingModel(f.Body)
		p.mu.Lock()
		p.billing = model
		p.mu.Unlock()
		_ = p.send(ykc.CmdBillingSettingAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "计费模型设置应答")
		p.log.Infof("桩 %s 收到计费模型设置：%s", p.cfg.PileNo, model.Describe())
		p.notify()
	case ykc.CmdTimeSync:
		pileNo, t := ykc.ParseTimeSync(f.Body)
		_ = p.send(ykc.CmdTimeSyncAck, ykc.BuildTimeSyncAck(pileNo, time.Now()), "对时设置应答")
		p.log.Infof("桩 %s 对时设置：平台时间 %s", p.cfg.PileNo, t.Format("2006-01-02 15:04:05"))
	case ykc.CmdReadRealTime:
		pileNo, gunNo := ykc.ParseReadRealTime(f.Body)
		p.log.Infof("桩 %s 平台召唤实时数据 枪%d", pileNo, gunNo)
		if g := p.gunByNo(gunNo); g != nil {
			p.sendRealTime(g)
		}
	case ykc.CmdApplyChargeAck, ykc.CmdApplyCharge2Ack:
		p.handleApplyChargeAck(f)
	case ykc.CmdRemoteCharge, ykc.CmdRemoteCharge2:
		p.handleRemoteCharge(f)
	case ykc.CmdRemoteStop:
		p.handleRemoteStop(f)
	case ykc.CmdTradeAck:
		p.handleTradeAck(f)
	case ykc.CmdBalanceUpdate:
		_ = p.send(ykc.CmdBalanceUpdateAck, append(append(ykc.BcdEncode(p.cfg.PileNo, 7), make([]byte, 8)...), 0x00), "余额更新应答")
	case ykc.CmdCardSync:
		_ = p.send(ykc.CmdCardSyncAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00, 0x00), "卡数据同步应答")
	case ykc.CmdCardClear:
		_ = p.send(ykc.CmdCardClearAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00, 0x00), "卡数据清除应答")
	case ykc.CmdCardQuery:
		_ = p.send(ykc.CmdCardQueryAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "卡数据查询应答")
	case ykc.CmdParamSetting:
		if len(f.Body) >= 8 {
			_ = p.send(ykc.CmdParamSettingAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), f.Body[7], 0x00), "工作参数设置应答")
		}
	case ykc.CmdMaxPower:
		if len(f.Body) >= 8 {
			_ = p.send(ykc.CmdMaxPowerAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), f.Body[7], 0x00), "最大功率下发应答")
		}
	case ykc.CmdRemoteRestart:
		_ = p.send(ykc.CmdRemoteRestartAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "远程重启应答")
	case ykc.CmdRemoteUpdate:
		_ = p.send(ykc.CmdRemoteUpdateAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "远程更新应答")
	case ykc.CmdKeyUpdate:
		_ = p.send(ykc.CmdKeyUpdateAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "密钥更新应答")
	case ykc.CmdLog:
		body := append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
		_ = p.send(ykc.CmdLogAck, body, "日志召唤应答")
	case ykc.CmdTradeLog:
		if len(f.Body) >= 24 {
			body := append(ykc.BcdEncode(ykc.BcdDecode(f.Body[:16]), 16),
				append(ykc.BcdEncode(p.cfg.PileNo, 7), f.Body[23], 0x01, 0x00)...)
			_ = p.send(ykc.CmdTradeLogAck, body, "交易记录召唤确认")
		}
	case ykc.CmdRemoteLock:
		_ = p.send(ykc.CmdRemoteLockAck, append(append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), make([]byte, 4)...), "遥控地锁应答")
	case ykc.CmdQrPrefix:
		_ = p.send(ykc.CmdQrPrefixAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "二维码前缀应答")
	case ykc.CmdQrSetting:
		_ = p.send(ykc.CmdQrSettingAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00, 0x00), "二维码设置应答")
	case ykc.CmdPlatformConnect:
		_ = p.send(ykc.CmdPlatformConnectAck, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00), "平台连接设置应答")
	case ykc.CmdParamSetting2:
		_ = p.send(ykc.CmdParamSetting2Ack, append(ykc.BcdEncode(p.cfg.PileNo, 7), 0x00, 0x00), "参数设置应答")
	case ykc.CmdMotorStartAck, ykc.CmdDeviceErrorAck:
		// 平台确认类帧，无需应答
	default:
		p.log.Warnf("桩 %s 收到未处理下行帧 0x%02X", p.cfg.PileNo, f.Cmd)
	}
}

// handleApplyChargeAck 平台确认启动充电
func (p *Pile) handleApplyChargeAck(f ykc.Frame) {
	ack := ykc.ParseApplyChargeAck(f.Body)
	g := p.gunByNo(ack.GunNo)
	if g == nil {
		p.log.Warnf("桩 %s 启动确认枪号非法: %d", p.cfg.PileNo, ack.GunNo)
		return
	}
	p.log.Infof("桩 %s 枪 %d 启动确认：流水号 %s 余额 %.2f 元 鉴权标志 0x%02X 失败原因 %d",
		p.cfg.PileNo, g.no, ack.TradeNo, ack.Balance, ack.AuthFlag, ack.FailReason)

	p.mu.Lock()
	s := g.session
	if s == nil {
		p.mu.Unlock()
		return
	}
	// 服务端实现中鉴权标志位恒为 0x00，以失败原因位作为判定依据
	if ack.FailReason != 0x00 {
		p.mu.Unlock()
		p.log.Errorf("桩 %s 枪 %d 启动失败，失败原因 %d", p.cfg.PileNo, g.no, ack.FailReason)
		p.mu.Lock()
		g.session = nil
		g.status = GunPlugged
		p.mu.Unlock()
		p.notify()
		return
	}
	s.Phase = PhaseCharging
	s.Balance = ack.Balance
	g.status = GunCharging
	g.dirty = true
	p.mu.Unlock()

	p.log.Infof("桩 %s 枪 %d 开始充电，流水号 %s", p.cfg.PileNo, g.no, s.TradeNo)
	p.sendRealTime(g)
	p.notify()
}

// handleRemoteCharge 平台远程启机
func (p *Pile) handleRemoteCharge(f ykc.Frame) {
	rc := ykc.ParseRemoteCharge(f.Body)
	g := p.gunByNo(rc.GunNo)
	if g == nil {
		_ = p.send(p.spec.RemoteStartAck, ykc.BuildRemoteStartAck(rc.TradeNo, p.cfg.PileNo, rc.GunNo, 0x00, 0x01), "远程启机回复")
		return
	}
	p.mu.Lock()
	if g.session != nil {
		p.mu.Unlock()
		_ = p.send(p.spec.RemoteStartAck, ykc.BuildRemoteStartAck(rc.TradeNo, p.cfg.PileNo, rc.GunNo, 0x00, 0x02), "远程启机回复")
		p.log.Warnf("桩 %s 枪 %d 已在充电，拒绝远程启机", p.cfg.PileNo, g.no)
		return
	}
	g.plugged = true
	g.status = GunCharging
	target := p.defaultTarget()
	s := NewSession(rc.TradeNo, ykc.StartTypeCard, rc.CardNo, "", target,
		60, 20, g.maxVoltage, g.maxCurrent, p.meterBase())
	s.Phase = PhaseCharging
	s.Balance = rc.Balance
	g.session = s
	g.dirty = true
	p.mu.Unlock()

	_ = p.send(p.spec.RemoteStartAck, ykc.BuildRemoteStartAck(rc.TradeNo, p.cfg.PileNo, rc.GunNo, 0x01, 0x00), "远程启机回复")
	p.log.Infof("桩 %s 枪 %d 远程启机成功，流水号 %s", p.cfg.PileNo, g.no, rc.TradeNo)
	p.sendRealTime(g)
	p.notify()
}

// handleRemoteStop 平台远程停机
func (p *Pile) handleRemoteStop(f ykc.Frame) {
	pileNo, gunNo := ykc.ParseRemoteStop(f.Body)
	g := p.gunByNo(gunNo)
	if g == nil {
		_ = p.send(ykc.CmdRemoteStopAck, ykc.BuildRemoteStopAck(pileNo, gunNo, 0x00, 0x01), "远程停机回复")
		return
	}
	p.mu.Lock()
	s := g.session
	p.mu.Unlock()
	if s == nil {
		_ = p.send(ykc.CmdRemoteStopAck, ykc.BuildRemoteStopAck(pileNo, gunNo, 0x00, 0x02), "远程停机回复")
		p.log.Warnf("桩 %s 枪 %d 未处于充电状态，忽略远程停机", p.cfg.PileNo, gunNo)
		return
	}
	_ = p.send(ykc.CmdRemoteStopAck, ykc.BuildRemoteStopAck(pileNo, gunNo, 0x01, 0x00), "远程停机回复")
	p.finishCharge(g, ykc.StopReasonRemote)
}

// handleTradeAck 平台确认交易记录
func (p *Pile) handleTradeAck(f ykc.Frame) {
	tradeNo, result := ykc.ParseTradeAck(f.Body)
	p.log.Infof("桩 %s 交易记录确认：流水号 %s 结果 %d", p.cfg.PileNo, tradeNo, result)

	p.mu.Lock()
	var hit *gun
	for _, g := range p.guns {
		if g.session != nil && g.session.TradeNo == tradeNo {
			hit = g
			break
		}
	}
	if hit != nil {
		hit.session.Phase = PhaseSettled
		hit.session = nil
		if hit.plugged {
			hit.status = GunPlugged
		} else {
			hit.status = GunIdle
		}
		hit.dirty = true
	}
	p.mu.Unlock()
	p.notify()
}

// ---------------- 外部指令 ----------------

func (p *Pile) handleCommand(c command) {
	switch c.kind {
	case cmdStart:
		if g := p.gunByNo(byte(c.req.GunNo)); g != nil {
			p.startCharge(g, c.req)
		}
	case cmdStop:
		if g := p.gunByNo(byte(c.gun)); g != nil {
			p.mu.Lock()
			s := g.session
			p.mu.Unlock()
			if s != nil {
				p.finishCharge(g, ykc.StopReasonManual)
			} else {
				p.log.Warnf("桩 %s 枪 %d 未在充电，忽略停止指令", p.cfg.PileNo, g.no)
			}
		}
	case cmdPlug:
		if g := p.gunByNo(byte(c.gun)); g != nil {
			p.mu.Lock()
			g.plugged = true
			if g.status == GunIdle {
				g.status = GunPlugged
			}
			g.dirty = true
			p.mu.Unlock()
			p.notify()
		}
	case cmdUnplug:
		if g := p.gunByNo(byte(c.gun)); g != nil {
			p.mu.Lock()
			g.plugged = false
			if g.session == nil {
				g.status = GunIdle
			}
			g.dirty = true
			p.mu.Unlock()
			p.notify()
		}
	case cmdFault:
		if g := p.gunByNo(byte(c.gun)); g != nil {
			p.mu.Lock()
			g.status = GunFault
			g.dirty = true
			p.mu.Unlock()
			p.notify()
		}
	case cmdClearFault:
		if g := p.gunByNo(byte(c.gun)); g != nil {
			p.mu.Lock()
			g.status = GunIdle
			g.dirty = true
			p.mu.Unlock()
			p.notify()
		}
	}
}

// ---------------- 内部工具 ----------------

func (p *Pile) gunByNo(no byte) *gun {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, g := range p.guns {
		if g.no == no {
			return g
		}
	}
	return nil
}

// nextTradeNo 生成交易流水号：桩号(14) + 枪号(2) + 时间(12) + 序号(4) = 32 位 BCD
func (p *Pile) nextTradeNo(gunNo byte) string {
	p.mu.Lock()
	p.seq++
	n := p.seq
	p.mu.Unlock()
	now := time.Now()
	return fmt.Sprintf("%s%02d%02d%02d%02d%02d%02d%02d%04d",
		p.cfg.PileNo, gunNo,
		now.Year()%100, now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(),
		n%10000)
}

// meterBase 电表起值基准（按桩号派生，保证同一枪每次充电递增）
func (p *Pile) meterBase() float64 {
	return 1000 + float64(time.Now().Unix()%100000)/10
}

func (p *Pile) defaultTarget() ChargeTarget {
	return ChargeTarget{Mode: TargetAmount, Amount: 50, Minute: 60, Energy: 40, Soc: 90}
}

func (p *Pile) setState(status string, err string) {
	p.mu.Lock()
	p.status = status
	p.lastError = err
	p.mu.Unlock()
	p.notify()
}

// ---------------- 状态快照 ----------------

// Snapshot 生成前端展示用的桩状态快照
func (p *Pile) Snapshot() PileView {
	p.mu.Lock()
	defer p.mu.Unlock()

	v := PileView{
		ID:          p.cfg.ID,
		PileNo:      p.cfg.PileNo,
		Version:     string(p.spec.Code),
		VersionDesc: p.spec.Desc,
		Addr:        p.Addr(),
		Status:      p.status,
		GunCount:    len(p.guns),
		PileType:    pileTypeDesc(p.cfg.PileType),
		RatedPower:  p.cfg.RatedPower,
		ProgramVer:  p.cfg.ProgramVer,
		Enabled:     p.cfg.Enabled,
		LoggedIn:    p.loggedIn,
		LastError:   p.lastError,
		SentFrames:  p.sentFrames,
		RecvFrames:  p.recvFrames,
		Guns:        make([]GunView, 0, len(p.guns)),
	}
	if p.connectedAt != nil {
		v.ConnectedAt = p.connectedAt.Format("2006-01-02 15:04:05")
	}
	if p.billing != nil {
		v.Billing = p.billing.Describe()
	}
	for _, g := range p.guns {
		v.Guns = append(v.Guns, g.view())
	}
	return v
}

// view 枪状态快照
func (g *gun) view() GunView {
	v := GunView{
		No:       int(g.no),
		Status:   g.status,
		Plugged:  g.plugged,
		MaxVolts: g.maxVoltage,
		MaxAmps:  g.maxCurrent,
	}
	switch g.status {
	case GunFault:
		v.StatusCode = ykc.GunStatusFault
	case GunCharging:
		v.StatusCode = ykc.GunStatusCharging
	default:
		v.StatusCode = ykc.GunStatusIdle
	}
	s := g.session
	if s == nil {
		// 会话已释放时展示最近一次充电的结算数据
		s = g.lastResult
	}
	if s == nil {
		return v
	}
	v.TradeNo = s.TradeNo
	v.Voltage = round2(s.Voltage)
	v.Current = round2(s.Current)
	v.Power = round2(s.Power)
	v.Soc = int(s.Soc)
	v.Energy = round4(s.Energy)
	v.Amount = round2(s.Amount)
	v.Minutes = s.Minutes()
	v.RemainMinutes = s.RemainMinutes()
	v.GunTemp = s.GunTemp
	v.BatteryTemp = s.BatteryTemp
	v.Balance = s.Balance
	v.Progress = s.Progress()
	v.Target = s.Target
	v.StartType = startTypeDesc(s.StartType)
	v.CardNo = s.CardNo
	v.Vin = s.Vin
	if s.Phase == PhaseStopping || s.Phase == PhaseSettled {
		v.StopReason = ykc.StopReasonDesc(s.StopReason)
	}
	if s.Phase == PhaseWaitAck {
		wait := time.Until(s.WaitUntil).Seconds()
		if wait > 0 {
			v.WaitSeconds = int(wait)
		}
	}
	return v
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

func pileTypeDesc(t int) string {
	if t == 1 {
		return "交流"
	}
	return "直流"
}

func startTypeDesc(t byte) string {
	switch t {
	case ykc.StartTypeCard:
		return "刷卡"
	case ykc.StartTypeAccount:
		return "账号"
	case ykc.StartTypeVin:
		return "VIN"
	}
	return "刷卡"
}

func targetDesc(t ChargeTarget) string {
	switch t.Mode {
	case TargetAmount:
		return fmt.Sprintf("金额 %.2f 元", t.Amount)
	case TargetTime:
		return fmt.Sprintf("时长 %.0f 分钟", t.Minute)
	case TargetEnergy:
		return fmt.Sprintf("电量 %.2f kWh", t.Energy)
	case TargetSoc:
		return fmt.Sprintf("SOC %d%%", t.Soc)
	}
	return "-"
}
