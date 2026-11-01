package gas

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
)

// notifyThrottle 状态变更推送的最小间隔，避免高频刷新前端
const notifyThrottle = 300 * time.Millisecond

// orderHistorySize 保留的最近订单条数
const orderHistorySize = 50

// Manager 多油机管理器：维护油机实例生命周期与订单历史
type Manager struct {
	mu      sync.RWMutex
	units   map[string]*Dispenser
	log     *logger.Bus
	st      *store.Store
	cfg     store.AppConfig
	pusher  Pusher
	onChange func()
	onPush   func(PushMessage)

	throttleMu sync.Mutex
	pending    bool

	seq   int64
	orders []FuelOrder // 最近订单，最新在前
}

// NewManager 创建管理器。
// onChange 用于状态快照刷新，onPush 用于把待推送内容回传给界面。
func NewManager(log *logger.Bus, st *store.Store, onChange func(), onPush func(PushMessage)) *Manager {
	if onChange == nil {
		onChange = func() {}
	}
	if onPush == nil {
		onPush = func(PushMessage) {}
	}
	m := &Manager{
		units:    make(map[string]*Dispenser),
		log:      log,
		st:       st,
		cfg:      st.Get(),
		onChange: onChange,
		onPush:   onPush,
		orders:   make([]FuelOrder, 0, orderHistorySize),
	}
	m.pusher = NewPusher(m.cfg.Gas, m.onPushed)
	return m
}

// onPushed 推送通道回调：订单类报文顺带记入历史，其余原样回传界面
func (m *Manager) onPushed(msg PushMessage) {
	if msg.Kind == PushKindOrder {
		var o FuelOrder
		if err := json.Unmarshal([]byte(msg.Payload), &o); err == nil {
			m.recordOrder(o)
		}
	}
	m.emitPush(msg)
}

// emitPush 给推送内容编号后回传给界面（编号用于前端排序 / 去重）
func (m *Manager) emitPush(msg PushMessage) {
	msg.ID = atomic.AddInt64(&m.seq, 1)
	if msg.Time == "" {
		msg.Time = nowStamp()
	}
	m.onPush(msg)
}

// Apply 按配置同步油机实例：新增 / 热更新 / 移除
func (m *Manager) Apply(cfg store.AppConfig) {
	m.mu.Lock()
	m.cfg = cfg
	newPusher := NewPusher(cfg.Gas, m.onPushed)

	for id, u := range m.units {
		found := false
		for _, c := range cfg.Gas.Dispensers {
			if c.ID == id {
				found = true
				break
			}
		}
		if !found {
			u.Stop()
			delete(m.units, id)
			m.log.Infof("加油机 %s 已移除", u.cfg.DispenserNo)
		}
	}

	for _, c := range cfg.Gas.Dispensers {
		old, ok := m.units[c.ID]
		if ok && dispenserEqual(old.cfg, c) {
			old.setPusher(newPusher)
			old.Reconfigure(c, cfg.Gas)
			continue
		}
		if ok {
			old.Stop()
			delete(m.units, c.ID)
		}
		m.units[c.ID] = NewDispenser(c, cfg.Gas, m.log, m.notify, newPusher)
	}

	snapshot := make([]*Dispenser, 0, len(m.units))
	for _, u := range m.units {
		snapshot = append(snapshot, u)
	}
	m.pusher = newPusher
	m.mu.Unlock()

	// 油机无启用 / 停用状态：配置中存在的油机一律投入运行
	for _, u := range snapshot {
		u.Start()
	}
	m.notify()
}

// setPusher 事件循环内换用新推送通道
func (d *Dispenser) setPusher(p Pusher) {
	d.mu.Lock()
	if p != nil {
		d.pusher = p
	}
	d.mu.Unlock()
}

// Reload 重新加载配置并应用
func (m *Manager) Reload() { m.Apply(m.st.Get()) }

// StartFuel 开始加油（自动提枪并推进加油过程，达到定额自动停止）
func (m *Manager) StartFuel(req StartFuelRequest) error {
	u := m.get(req.DispenserID)
	if u == nil {
		return fmt.Errorf("加油机不存在: %s", req.DispenserID)
	}
	if !u.Submit(command{kind: cmdFuelStart, req: req, nozzleNo: req.NozzleNo}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// StopFuel 停止加油并结算
func (m *Manager) StopFuel(dispenserID, nozzleNo string) error {
	u := m.get(dispenserID)
	if u == nil {
		return fmt.Errorf("加油机不存在: %s", dispenserID)
	}
	if !u.Submit(command{kind: cmdFuelStop, nozzleNo: nozzleNo}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// SetNozzle 提枪 / 挂枪
func (m *Manager) SetNozzle(dispenserID, nozzleNo string, lifted bool) error {
	u := m.get(dispenserID)
	if u == nil {
		return fmt.Errorf("加油机不存在: %s", dispenserID)
	}
	kind := cmdHangUp
	if lifted {
		kind = cmdLift
	}
	if !u.Submit(command{kind: kind, nozzleNo: nozzleNo}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// SetFault 置故障 / 清除故障
func (m *Manager) SetFault(dispenserID, nozzleNo string, fault bool) error {
	u := m.get(dispenserID)
	if u == nil {
		return fmt.Errorf("加油机不存在: %s", dispenserID)
	}
	kind := cmdClearFault
	if fault {
		kind = cmdFault
	}
	if !u.Submit(command{kind: kind, nozzleNo: nozzleNo}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// UpdateNozzleFuel 修改单把油枪的油号并持久化。
// 加油中的油枪不允许改油号，避免订单油品与实际不符。
func (m *Manager) UpdateNozzleFuel(dispenserID, nozzleNo, fuelCode string) error {
	if _, ok := store.FuelByCode(fuelCode); !ok {
		return fmt.Errorf("油号 %s 不在目录中", fuelCode)
	}
	cfg := m.st.Get()
	found := false
	for i := range cfg.Gas.Dispensers {
		if cfg.Gas.Dispensers[i].ID != dispenserID {
			continue
		}
		for j := range cfg.Gas.Dispensers[i].Nozzles {
			if cfg.Gas.Dispensers[i].Nozzles[j].No == nozzleNo {
				cfg.Gas.Dispensers[i].Nozzles[j].FuelCode = fuelCode
				found = true
			}
		}
	}
	if !found {
		return fmt.Errorf("油枪不存在: %s 号机 %s 号枪", dispenserID, nozzleNo)
	}
	if err := m.st.Save(cfg); err != nil {
		return err
	}
	m.Apply(cfg)
	return nil
}

// Snapshot 获取全部油机状态快照
func (m *Manager) Snapshot() []DispenserView {
	m.mu.RLock()
	list := make([]*Dispenser, 0, len(m.units))
	for _, u := range m.units {
		list = append(list, u)
	}
	m.mu.RUnlock()

	out := make([]DispenserView, 0, len(list))
	for _, u := range list {
		out = append(out, u.Snapshot())
	}
	// 保持界面顺序稳定：按油机号升序
	sortDispensers(out)
	return out
}

// Orders 最近订单（最新在前）
func (m *Manager) Orders() []FuelOrder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]FuelOrder, len(m.orders))
	copy(out, m.orders)
	return out
}

// recordOrder 记录已结算订单（仅保留最近 orderHistorySize 条）
func (m *Manager) recordOrder(o FuelOrder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders = append([]FuelOrder{o}, m.orders...)
	if len(m.orders) > orderHistorySize {
		m.orders = m.orders[:orderHistorySize]
	}
}

// PushTarget 当前推送通道描述，供界面展示
func (m *Manager) PushTarget() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.pusher == nil {
		return "未启用数据推送"
	}
	return m.pusher.Describe()
}

// Shutdown 停止全部油机并释放协程
func (m *Manager) Shutdown() {
	m.mu.RLock()
	list := make([]*Dispenser, 0, len(m.units))
	for _, u := range m.units {
		list = append(list, u)
	}
	m.mu.RUnlock()
	for _, u := range list {
		u.Stop()
	}
}

func (m *Manager) get(id string) *Dispenser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.units[id]
}

// notify 节流推送状态变更
func (m *Manager) notify() {
	m.throttleMu.Lock()
	if m.pending {
		m.throttleMu.Unlock()
		return
	}
	m.pending = true
	m.throttleMu.Unlock()

	time.AfterFunc(notifyThrottle, func() {
		m.throttleMu.Lock()
		m.pending = false
		m.throttleMu.Unlock()
		m.onChange()
	})
}

// dispenserEqual 判断油机结构是否变化（编号 / 枪数）。
// 油号与油价变更属于热更新项，不走重建，避免打断进行中的加油。
func dispenserEqual(a, b store.DispenserConfig) bool {
	return a.ID == b.ID && a.DispenserNo == b.DispenserNo && a.NozzleCount == b.NozzleCount
}

func sortDispensers(list []DispenserView) {
	n := len(list)
	for i := 1; i < n; i++ {
		for j := i; j > 0 && dispenserLess(list[j], list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func dispenserLess(a, b DispenserView) bool {
	ai, aerr := strconv.Atoi(a.DispenserNo)
	bi, berr := strconv.Atoi(b.DispenserNo)
	if aerr == nil && berr == nil && ai != bi {
		return ai < bi
	}
	return a.DispenserNo < b.DispenserNo
}
