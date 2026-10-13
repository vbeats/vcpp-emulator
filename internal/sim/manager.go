package sim

import (
	"context"
	"fmt"
	"sync"

	"github.com/csf/vcpp-emulator/internal/billing"
	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/framelog"
	"github.com/csf/vcpp-emulator/internal/protocol"
)

// Manager 模拟桩注册表：负责桩生命周期、启停与状态查询。
type Manager struct {
	cfg    *config.Config
	codec  protocol.Codec
	bill   *billing.Cache
	logger *framelog.Logger

	mu      sync.Mutex
	piles   map[uint64]*Pile
	order   []uint64
	cancels map[uint64]context.CancelFunc
}

// NewManager 按配置创建所有模拟桩（创建后未自动连接，需调用 StartDevice）。
func NewManager(cfg *config.Config) (*Manager, error) {
	codec, err := protocol.New(cfg.Protocol.Type, cfg.Protocol.Version)
	if err != nil {
		return nil, err
	}
	logger := &framelog.Logger{Hex: cfg.FrameLog.Hex, Parsed: cfg.FrameLog.Parsed}
	m := &Manager{
		cfg:     cfg,
		codec:   codec,
		bill:    &billing.Cache{},
		logger:  logger,
		piles:   make(map[uint64]*Pile, len(cfg.Devices)),
		cancels: make(map[uint64]context.CancelFunc, len(cfg.Devices)),
	}
	for _, d := range cfg.Devices {
		no := uint64(d.DeviceNo)
		m.piles[no] = NewPile(cfg, d, codec, m.bill, logger)
		m.order = append(m.order, no)
	}
	return m, nil
}

// Start 启动配置为 AutoStart 的桩。
func (m *Manager) Start(ctx context.Context) int {
	var n int
	for _, d := range m.cfg.Devices {
		if d.AutoStart {
			if err := m.StartDevice(ctx, uint64(d.DeviceNo)); err == nil {
				n++
			}
		}
	}
	return n
}

// StartDevice 启动指定桩。
func (m *Manager) StartDevice(ctx context.Context, deviceNo uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.piles[deviceNo]
	if !ok {
		return fmt.Errorf("桩 %014d 不存在", deviceNo)
	}
	if _, running := m.cancels[deviceNo]; running {
		return nil
	}
	pctx, cancel := context.WithCancel(ctx)
	m.cancels[deviceNo] = cancel
	go p.Run(pctx)
	m.logger.Eventf(deviceNo, "桩已启动，目标 server=%s 协议=%s %s", m.cfg.Addr(),
		m.cfg.Protocol.Type, m.codec.Version())
	return nil
}

// StopDevice 停止指定桩（正在充电的枪会先走终止与交易记录上送）。
func (m *Manager) StopDevice(deviceNo uint64) error {
	p, err := m.pile(deviceNo)
	if err != nil {
		return err
	}
	if err = p.Submit(Command{Kind: CmdDeviceStop}); err != nil {
		return err
	}
	m.mu.Lock()
	if cancel, ok := m.cancels[deviceNo]; ok {
		cancel()
		delete(m.cancels, deviceNo)
	}
	m.mu.Unlock()
	m.logger.Eventf(deviceNo, "桩已停止")
	return nil
}

// TriggerStart 手动触发指定枪启机。
func (m *Manager) TriggerStart(deviceNo uint64, gunNo int) error {
	p, err := m.pile(deviceNo)
	if err != nil {
		return err
	}
	return p.Submit(Command{Kind: CmdGunStart, Gun: byte(gunNo)})
}

// TriggerStop 手动终止指定枪充电。
func (m *Manager) TriggerStop(deviceNo uint64, gunNo int) error {
	p, err := m.pile(deviceNo)
	if err != nil {
		return err
	}
	return p.Submit(Command{Kind: CmdGunStop, Gun: byte(gunNo), Reason: "HTTP 主动终止"})
}

// StopAll 停止所有桩，用于优雅退出。
func (m *Manager) StopAll() {
	m.mu.Lock()
	nos := append([]uint64(nil), m.order...)
	m.mu.Unlock()
	for _, no := range nos {
		if err := m.StopDevice(no); err != nil {
			m.logger.Eventf(no, "停止桩失败: %v", err)
		}
	}
}

// Status 返回所有桩的状态快照。
func (m *Manager) Status() []DeviceStatus {
	list := make([]DeviceStatus, 0, len(m.order))
	m.mu.Lock()
	for _, no := range m.order {
		if p, ok := m.piles[no]; ok {
			list = append(list, p.Status())
		}
	}
	m.mu.Unlock()
	return list
}

// DeviceNos 返回全部桩号。
func (m *Manager) DeviceNos() []uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uint64(nil), m.order...)
}

// Server 返回当前连接的 server 地址（同一时刻仅一个）。
func (m *Manager) Server() string { return m.cfg.Addr() }

func (m *Manager) pile(deviceNo uint64) (*Pile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.piles[deviceNo]
	if !ok {
		return nil, fmt.Errorf("桩 %014d 不存在", deviceNo)
	}
	return p, nil
}
