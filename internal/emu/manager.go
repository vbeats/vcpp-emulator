package emu

import (
	"fmt"
	"sync"
	"time"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
)

// notifyThrottle 状态变更推送的最小间隔，避免高频刷新前端
const notifyThrottle = 300 * time.Millisecond

// Manager 多桩管理器：维护模拟器实例生命周期，保证连接与协程可收敛
type Manager struct {
	mu       sync.RWMutex
	piles    map[string]*Pile
	log      *logger.Bus
	st       *store.Store
	cfg      store.AppConfig
	onChange func()

	throttleMu sync.Mutex
	pending    bool
}

// NewManager 创建管理器
func NewManager(log *logger.Bus, st *store.Store, onChange func()) *Manager {
	return &Manager{
		piles:    make(map[string]*Pile),
		log:      log,
		st:       st,
		cfg:      st.Get(),
		onChange: onChange,
	}
}

// Apply 按配置同步桩实例：新增 / 重建 / 移除。
// 只有在显式开启 auto-connect 时才会自动连接，默认保持未连接状态由用户手动发起，
// 已经手动连接的桩不受影响（配置未变的桩不会重建）。
func (m *Manager) Apply(cfg store.AppConfig) {
	m.mu.Lock()
	m.cfg = cfg

	// 移除配置中已不存在的桩
	for id, p := range m.piles {
		found := false
		for _, c := range cfg.Piles {
			if c.ID == id {
				found = true
				break
			}
		}
		if !found {
			p.Stop()
			delete(m.piles, id)
			m.log.Infof("桩 %s 已移除", p.cfg.PileNo)
		}
	}

	// 新增或重建
	for _, c := range cfg.Piles {
		old, ok := m.piles[c.ID]
		if ok && pileEqual(old.cfg, c) && serverEqual(old.server, cfg.Server) {
			continue
		}
		if ok {
			old.Stop()
			delete(m.piles, c.ID)
		}
		p := NewPile(c, cfg.Server, m.log, m.notify)
		m.piles[c.ID] = p
	}
	snapshot := make([]*Pile, 0, len(m.piles))
	for _, p := range m.piles {
		snapshot = append(snapshot, p)
	}
	m.mu.Unlock()

	for _, p := range snapshot {
		if cfg.AutoConnect && p.cfg.Enabled {
			p.Start(cfg)
		}
	}
	m.notify()
}

// Reload 重新加载配置并应用
func (m *Manager) Reload() {
	m.Apply(m.st.Get())
}

// Start 启动指定桩
func (m *Manager) Start(id string) error {
	p := m.get(id)
	if p == nil {
		return fmt.Errorf("桩不存在: %s", id)
	}
	p.Start(m.cfg)
	m.notify()
	return nil
}

// Stop 停止指定桩
func (m *Manager) Stop(id string) error {
	p := m.get(id)
	if p == nil {
		return fmt.Errorf("桩不存在: %s", id)
	}
	p.Stop()
	m.notify()
	return nil
}

// StartAll 启动全部桩
func (m *Manager) StartAll() {
	m.mu.RLock()
	list := make([]*Pile, 0, len(m.piles))
	for _, p := range m.piles {
		list = append(list, p)
	}
	cfg := m.cfg
	m.mu.RUnlock()
	for _, p := range list {
		p.Start(cfg)
	}
	m.notify()
}

// StopAll 停止全部桩
func (m *Manager) StopAll() {
	m.mu.RLock()
	list := make([]*Pile, 0, len(m.piles))
	for _, p := range m.piles {
		list = append(list, p)
	}
	m.mu.RUnlock()
	for _, p := range list {
		p.Stop()
	}
	m.notify()
}

// StartCharge 发起充电
func (m *Manager) StartCharge(req StartRequest) error {
	p := m.get(req.PileID)
	if p == nil {
		return fmt.Errorf("桩不存在: %s", req.PileID)
	}
	if !p.IsRunning() {
		return fmt.Errorf("桩未启动，请先连接")
	}
	if !p.Submit(command{kind: cmdStart, req: req}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// StopCharge 停止充电
func (m *Manager) StopCharge(pileID string, gun int) error {
	p := m.get(pileID)
	if p == nil {
		return fmt.Errorf("桩不存在: %s", pileID)
	}
	if !p.Submit(command{kind: cmdStop, gun: gun}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// SetPlug 插枪 / 拔枪
func (m *Manager) SetPlug(pileID string, gun int, plugged bool) error {
	p := m.get(pileID)
	if p == nil {
		return fmt.Errorf("桩不存在: %s", pileID)
	}
	kind := cmdUnplug
	if plugged {
		kind = cmdPlug
	}
	if !p.Submit(command{kind: kind, gun: gun}) {
		return fmt.Errorf("指令队列繁忙，请稍后重试")
	}
	return nil
}

// Snapshot 获取全部桩状态快照
func (m *Manager) Snapshot() []PileView {
	m.mu.RLock()
	list := make([]*Pile, 0, len(m.piles))
	for _, p := range m.piles {
		list = append(list, p)
	}
	m.mu.RUnlock()

	out := make([]PileView, 0, len(list))
	for _, p := range list {
		out = append(out, p.Snapshot())
	}
	return out
}

// Shutdown 关闭全部桩并释放资源：逐个取消运行协程，等价销毁已建立的全部 TCP 连接
func (m *Manager) Shutdown() {
	if n := m.ConnectedCount(); n > 0 {
		m.log.Infof("正在断开 %d 条 TCP 连接", n)
	}
	m.StopAll()
}

// ConnectedCount 当前已建立 TCP 连接的桩数量
func (m *Manager) ConnectedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, p := range m.piles {
		if p.IsConnected() {
			n++
		}
	}
	return n
}

func (m *Manager) get(id string) *Pile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.piles[id]
}

// notify 节流推送状态变更
func (m *Manager) notify() {
	if m.onChange == nil {
		return
	}
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
		if m.onChange != nil {
			m.onChange()
		}
	})
}

func pileEqual(a, b store.PileConfig) bool {
	return a.PileNo == b.PileNo && a.GunCount == b.GunCount && a.PileType == b.PileType &&
		a.RatedPower == b.RatedPower && a.ProgramVer == b.ProgramVer && a.Enabled == b.Enabled
}

func serverEqual(a, b store.ServerConfig) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Version == b.Version
}
