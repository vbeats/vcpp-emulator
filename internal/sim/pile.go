package sim

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/csf/vcpp-emulator/internal/billing"
	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/framelog"
	"github.com/csf/vcpp-emulator/internal/protocol"
	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
	"github.com/csf/vcpp-emulator/internal/transport"
)

const (
	gunWorkIdle     = byte(0x02)
	gunWorkCharging = byte(0x03)
	gunNormal       = byte(0x00)

	stopByRemote  = byte(0x40) // APP 远程停止
	stopBySoc     = byte(0x41) // SOC 达到 100%
	stopByTimeout = byte(0x44) // 充电时间满足设定条件
	stopByManual  = byte(0x45) // 手动停止充电

	startErrBusy      = byte(0x02)
	startErrFault     = byte(0x03)
	startErrNoBilling = byte(0x03)

	watchdogInterval = 3 * time.Second
	defaultVin       = "LFVBEATS000000001"

	resultSuccess = byte(0x01)
	resultFailed  = byte(0x00)
)

// gun 单把充电枪的运行时状态。
type gun struct {
	no      byte
	session *session
}

// Pile 一个模拟桩。所有协议状态仅在 Actor 协程内修改，通过 channel 接收事件，无共享可变状态。
type Pile struct {
	cfg    *config.Config
	dev    config.DeviceConfig
	codec  protocol.Codec
	bill   *billing.Cache
	logger *framelog.Logger
	conn   *transport.Conn

	cmdCh   chan Command
	frameCh chan *ykc.Frame
	stateCh chan bool

	tradeFrame byte
	meterNo    string

	mu     sync.RWMutex
	status DeviceStatus

	// 以下字段仅由 Actor 协程访问
	running      bool
	connected    bool
	billingReady bool
	stage        Stage
	seq          uint16
	tradeSeq     uint16
	modelNo      string
	model        *billing.Model
	lastAck      time.Time
	guns         []*gun
	settling     []*session
}

// NewPile 构造一个模拟桩。
func NewPile(cfg *config.Config, dev config.DeviceConfig, codec protocol.Codec,
	bill *billing.Cache, logger *framelog.Logger) *Pile {
	p := &Pile{
		cfg:     cfg,
		dev:     dev,
		codec:   codec,
		bill:    bill,
		logger:  logger,
		cmdCh:   make(chan Command, 16),
		frameCh: make(chan *ykc.Frame, 64),
		stateCh: make(chan bool, 4),
		modelNo: "0000",
		stage:   StageDisconnected,
		lastAck: time.Now(),
		meterNo: dev.DeviceNo[2:14],
	}
	p.tradeFrame = ykc.TradeFrameType(codec.Version())
	for i := 1; i <= dev.GunCount; i++ {
		p.guns = append(p.guns, &gun{no: byte(i)})
	}
	p.conn = transport.NewConn(cfg, codec, logger, dev.DeviceNo, p.onFrame, p.pushConnState)
	// 未 Run 的桩处于已停止状态，由 Run 置为运行
	p.updateStage()
	return p
}

// DeviceNo 返回桩号。
func (p *Pile) DeviceNo() string { return p.dev.DeviceNo }

// Submit 向 Actor 提交控制指令。
func (p *Pile) Submit(cmd Command) error {
	reply := make(chan error, 1)
	cmd.Reply = reply
	select {
	case p.cmdCh <- cmd:
	case <-time.After(3 * time.Second):
		return fmt.Errorf("桩 %s 指令队列繁忙", p.dev.DeviceNo)
	}
	return <-reply
}

// Status 返回状态快照。
func (p *Pile) Status() DeviceStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	st := p.status
	st.Guns = append([]GunStatus(nil), p.status.Guns...)
	return st
}

// Run 运行桩 Actor，直到 ctx 取消。
func (p *Pile) Run(ctx context.Context) {
	p.running = true
	p.stage = StageDisconnected
	p.refreshStatus()

	hb := time.NewTicker(time.Duration(p.cfg.Timing.HeartbeatSec) * time.Second)
	defer hb.Stop()
	report := time.NewTicker(time.Duration(p.cfg.Timing.ChargeReportSec) * time.Second)
	defer report.Stop()
	watch := time.NewTicker(watchdogInterval)
	defer watch.Stop()

	go p.conn.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			p.conn.SetEnabled(false)
			p.mu.Lock()
			p.running = false
			p.stage = StageStopped
			p.mu.Unlock()
			p.refreshStatus()
			return
		case st := <-p.stateCh:
			p.applyConnState(st)
		case f := <-p.frameCh:
			p.handleFrame(f)
		case cmd := <-p.cmdCh:
			cmd.Reply <- p.handleCommand(cmd)
		case <-hb.C:
			p.tickHeartbeat()
		case <-report.C:
			p.tickReport()
		case <-watch.C:
			p.tickWatchdog()
		}
	}
}

// onFrame 读协程回调，把帧投递给 Actor。
func (p *Pile) onFrame(f *ykc.Frame) {
	select {
	case p.frameCh <- f:
	default:
		p.logger.Eventf(p.dev.DeviceNo, "帧处理队列已满，丢弃 0x%02X", f.Type)
	}
}

// pushConnState 连接状态变化回调（读/写协程调用），投递给 Actor 串行处理。
func (p *Pile) pushConnState(connected bool) {
	select {
	case p.stateCh <- connected:
	default:
	}
}

// ---------------- 连接与心跳 ----------------

// applyConnState 在 Actor 协程内应用连接状态变化。
func (p *Pile) applyConnState(connected bool) {
	if connected {
		p.handleConnUp()
	} else {
		p.handleConnDown()
	}
	p.refreshStatus()
}

func (p *Pile) handleConnUp() {
	p.connected = true
	p.seq = 0
	p.billingReady = false
	p.stage = StageHandshaking
	p.lastAck = time.Now()
	p.refreshStatus()
	p.sendLogin()
}

func (p *Pile) handleConnDown() {
	p.connected = false
	p.billingReady = false
	p.stage = StageDisconnected
	for _, g := range p.guns {
		g.session = nil
	}
	if len(p.settling) > 0 {
		p.logger.Eventf(p.dev.DeviceNo, "连接断开，%d 笔待确认交易记录丢弃", len(p.settling))
		p.settling = nil
	}
	p.refreshStatus()
}

func (p *Pile) sendLogin() {
	body, desc := ykc.EncodeLogin(ykc.LoginInfo{
		DeviceNo:   p.dev.DeviceNo,
		PileType:   byte(p.dev.PileType),
		GunCount:   byte(p.dev.GunCount),
		ProtoVer:   ykc.ProtoVerByte(p.codec.Version()),
		McuVersion: p.dev.McuVersion,
		NetType:    byte(p.dev.NetType),
		Sim:        p.dev.Sim,
		Supplier:   byte(p.dev.Supplier),
	})
	p.send(ykc.TypeLogin, body, desc, 0, nil)
}

func (p *Pile) tickHeartbeat() {
	if !p.running || !p.connected || p.stage == StageDisconnected {
		return
	}
	for _, g := range p.guns {
		status := gunNormal
		if g.session != nil {
			status = gunNormal
		}
		body, desc := ykc.EncodeHeartbeat(p.dev.DeviceNo, g.no, status)
		p.send(ykc.TypeHeartbeat, body, desc, g.no, nil)
	}
}

func (p *Pile) tickWatchdog() {
	if !p.running || !p.connected {
		return
	}
	timeout := time.Duration(p.cfg.Timing.HeartbeatTimeoutSec) * time.Second
	if time.Since(p.lastAck) > timeout {
		p.logger.Eventf(p.dev.DeviceNo, "连续 %.0f 秒未收到心跳应答，主动断开连接", timeout.Seconds())
		p.conn.Close()
		return
	}
	p.retrySettling()
}

// ---------------- 下行帧处理 ----------------

func (p *Pile) handleFrame(f *ykc.Frame) {
	gunNo := byte(0)
	desc := ""
	var err error

	switch f.Type {
	case ykc.TypeLoginAck:
		var deviceNo string
		var result byte
		deviceNo, result, desc, err = ykc.DecodeLoginAck(f.Body)
		if err == nil && result == 0x00 {
			p.logger.Eventf(deviceNo, "登录成功，开始获取计费模型")
			p.sendBillingVerify()
		}
		if err == nil && result != 0x00 {
			p.logger.Eventf(deviceNo, "登录被拒绝，结果=%d", result)
		}
	case ykc.TypeHeartbeatAck:
		_, gunNo, desc, err = ykc.DecodeHeartbeatAck(f.Body)
		if err == nil {
			p.lastAck = time.Now()
		}
	case ykc.TypeBillingVerifyAck:
		p.handleBillingVerifyAck(f)
		return
	case ykc.TypeBillingModelAck, ykc.TypeBillingSetting:
		p.handleBillingModel(f)
		return
	case ykc.TypeTimeSync:
		p.handleTimeSync(f)
		return
	case ykc.TypeReadRealTime:
		_, gunNo, desc, err = ykc.DecodeGunCommand(f.Body)
		if err == nil {
			p.sendRealTimeSnapshot(gunNo)
		}
	case ykc.TypeRemoteStart, ykc.TypeRemoteStart2:
		p.handleRemoteStart(f)
		return
	case ykc.TypeRemoteStop:
		_, gunNo, desc, err = ykc.DecodeGunCommand(f.Body)
		if err == nil {
			p.onRemoteStop(gunNo)
		}
	case ykc.TypeTradeAck:
		var tradeNo string
		var result byte
		tradeNo, result, desc, err = ykc.DecodeTradeAck(f.Body)
		if err == nil {
			p.onTradeAck(tradeNo, result)
		}
	case ykc.TypeRemoteRestart:
		var deviceNo string
		var ctrl byte
		deviceNo, ctrl, desc, err = ykc.DecodeRemoteRestart(f.Body)
		if err == nil {
			p.onRemoteRestart(ctrl)
			_ = deviceNo
		}
	default:
		p.logger.Eventf(p.dev.DeviceNo, "收到未处理的下行帧 0x%02X %s", f.Type, ykc.FrameName(f.Type))
		p.logger.Frame(ykc.DirDown, p.codec.Version(), p.dev.DeviceNo, 0, f.Type, f.Raw, "")
		return
	}

	if err != nil {
		p.logger.Eventf(p.dev.DeviceNo, "解析下行帧 0x%02X 失败: %v", f.Type, err)
	}
	p.logger.Frame(ykc.DirDown, p.codec.Version(), p.dev.DeviceNo, gunNo, f.Type, f.Raw, desc)
}

func (p *Pile) handleBillingVerifyAck(f *ykc.Frame) {
	_, modelNo, result, desc, err := ykc.DecodeBillingVerifyAck(f.Body)
	p.logger.Frame(ykc.DirDown, p.codec.Version(), p.dev.DeviceNo, 0, f.Type, f.Raw, desc)
	if err != nil {
		p.logger.Eventf(p.dev.DeviceNo, "解析计费模型验证应答失败: %v", err)
		return
	}
	p.modelNo = modelNo
	if result == 0x00 && p.model != nil {
		p.billingReady = true
		p.updateStage()
		return
	}
	// 验证结果不一致（vcpp 固定应答不一致）-> 请求最新计费模型
	p.sendBillingModelRequest()
}

func (p *Pile) handleBillingModel(f *ykc.Frame) {
	payload, desc, err := ykc.DecodeBillingModelAck(f.Body)
	p.logger.Frame(ykc.DirDown, p.codec.Version(), p.dev.DeviceNo, 0, f.Type, f.Raw, desc)
	if err != nil {
		p.logger.Eventf(p.dev.DeviceNo, "解析计费模型失败: %v", err)
		return
	}
	model := billing.FromPayload(payload)
	p.model = model
	p.modelNo = model.No
	p.bill.Set(p.dev.DeviceNo, model)
	p.billingReady = true
	p.logger.Eventf(p.dev.DeviceNo, "计费模型已缓存: %s", model.String())
	p.updateStage()

	if f.Type == ykc.TypeBillingSetting {
		body, ackDesc := ykc.EncodeSimpleAck(p.dev.DeviceNo, resultSuccess)
		p.send(ykc.TypeBillingSettingAck, body, ackDesc, 0, nil)
	}
}

func (p *Pile) handleTimeSync(f *ykc.Frame) {
	deviceNo, t, desc, err := ykc.DecodeTimeSync(f.Body)
	p.logger.Frame(ykc.DirDown, p.codec.Version(), p.dev.DeviceNo, 0, f.Type, f.Raw, desc)
	if err != nil {
		p.logger.Eventf(p.dev.DeviceNo, "解析对时设置失败: %v", err)
		return
	}
	_ = deviceNo
	body, ackDesc := ykc.EncodeTimeSyncAck(p.dev.DeviceNo, t)
	p.sendEcho(ykc.TypeTimeSyncAck, body, ackDesc, 0, f.SeqRaw)
}

func (p *Pile) handleRemoteStart(f *ykc.Frame) {
	info, desc, err := ykc.DecodeRemoteStart(f.Body)
	p.logger.Frame(ykc.DirDown, p.codec.Version(), p.dev.DeviceNo, 0, f.Type, f.Raw, desc)
	if err != nil {
		p.logger.Eventf(p.dev.DeviceNo, "解析远程启机失败: %v", err)
		return
	}
	ackType := ykc.TypeRemoteStartAck
	if f.Type == ykc.TypeRemoteStart2 {
		ackType = ykc.TypeRemoteStart2Ack
	}
	p.onRemoteStart(info, ackType)
}

// ---------------- 业务流程 ----------------

func (p *Pile) sendBillingVerify() {
	body, desc := ykc.EncodeBillingVerify(p.dev.DeviceNo, p.modelNo)
	p.send(ykc.TypeBillingVerify, body, desc, 0, nil)
}

func (p *Pile) sendBillingModelRequest() {
	body, desc := ykc.EncodeBillingModelRequest(p.dev.DeviceNo)
	p.send(ykc.TypeBillingModel, body, desc, 0, nil)
}

func (p *Pile) onRemoteStart(info *ykc.RemoteStartInfo, ackType ykc.FrameType) {
	gunNo := info.Gun
	g := p.gun(gunNo)
	if g == nil {
		body, desc := ykc.EncodeRemoteStartAck(info.TradeNo, p.dev.DeviceNo, gunNo, resultFailed, startErrFault)
		p.send(ackType, body, desc, gunNo, nil)
		return
	}
	if g.session != nil {
		body, desc := ykc.EncodeRemoteStartAck(info.TradeNo, p.dev.DeviceNo, gunNo, resultFailed, startErrBusy)
		p.send(ackType, body, desc, gunNo, nil)
		p.logger.Event(p.dev.DeviceNo, gunNo, "拒绝启机：该枪正在充电")
		return
	}
	if p.model == nil || !p.billingReady {
		body, desc := ykc.EncodeRemoteStartAck(info.TradeNo, p.dev.DeviceNo, gunNo, resultFailed, startErrNoBilling)
		p.send(ackType, body, desc, gunNo, nil)
		p.logger.Event(p.dev.DeviceNo, gunNo, "拒绝启机：尚未获取到计费模型，按协议禁止充电")
		return
	}

	body, desc := ykc.EncodeRemoteStartAck(info.TradeNo, p.dev.DeviceNo, gunNo, resultSuccess, 0x00)
	p.send(ackType, body, desc, gunNo, nil)
	p.beginCharge(gunNo, info.TradeNo)
}

func (p *Pile) beginCharge(gunNo byte, tradeNo string) {
	g := p.gun(gunNo)
	if g == nil {
		return
	}
	s := newSession(gunNo, tradeNo, defaultVin, p.cfg.Billing.MeterStartKwh,
		p.cfg.Billing.InitialSoc, p.dev.VoltageV, p.cfg.Billing.ChargePowerKw)
	g.session = s
	p.stage = StageStarting
	p.logger.Event(p.dev.DeviceNo, gunNo, "开始充电，交易流水号=%s", tradeNo)

	p.sendHandshake(s)
	p.sendParamConfig(s)
	p.stage = StageCharging
	p.refreshStatus()

	// 启机后立即上报一次，之后按周期上报
	p.reportSession(s)
}

func (p *Pile) sendHandshake(s *session) {
	body, desc := ykc.EncodeHandshake(ykc.HandshakeInfo{
		TradeNo:         s.TradeNo,
		DeviceNo:        p.dev.DeviceNo,
		Gun:             s.GunNo,
		BatteryType:     0x03,
		RatedCapacityAh: 120,
		RatedVoltageV:   p.dev.VoltageV,
		Vendor:          "ATZX",
		PackSerial:      1,
		ProduceDate:     time.Now().AddDate(-1, 0, 0),
		ChargeTimes:     128,
		Ownership:       1,
		Vin:             s.Vin,
		BmsSwSerial:     1,
		BmsSwDate:       time.Now(),
	})
	p.send(ykc.TypeHandshake, body, desc, s.GunNo, nil)
}

func (p *Pile) sendParamConfig(s *session) {
	body, desc := ykc.EncodeParamConfig(ykc.ParamConfigInfo{
		TradeNo:           s.TradeNo,
		DeviceNo:          p.dev.DeviceNo,
		Gun:               s.GunNo,
		CellMaxVoltageV:   4.00,
		MaxChargeCurrentA: s.CurrentA,
		RatedEnergyKwh:    p.cfg.Billing.PackCapacityKwh,
		MaxTotalVoltageV:  p.dev.VoltageV + 100,
		MaxTempC:          60,
		Soc:               float64(s.Soc),
		BatteryVoltageV:   p.dev.VoltageV,
		PileMaxVoltageV:   1000,
		PileMinVoltageV:   200,
		PileMaxCurrentA:   s.CurrentA,
		PileMinCurrentA:   0,
	})
	p.send(ykc.TypeParamConfig, body, desc, s.GunNo, nil)
}

func (p *Pile) tickReport() {
	if !p.running || !p.connected || p.model == nil {
		return
	}
	sec := p.cfg.Timing.ChargeReportSec
	for _, g := range p.guns {
		s := g.session
		if s == nil || s.StopReason != 0 {
			continue
		}
		s.advance(sec, p.cfg.Billing.ChargePowerKw, p.model,
			p.cfg.Billing.InitialSoc, p.cfg.Billing.PackCapacityKwh)
		p.reportSession(s)
		if s.ElapsedSec >= p.cfg.Timing.ChargeDurationSec {
			p.stopCharge(g.no, stopByTimeout, "达到配置的充电时长")
			continue
		}
		if s.Soc >= 100 {
			p.stopCharge(g.no, stopBySoc, "SOC 达到 100%")
		}
	}
	p.refreshStatus()
}

func (p *Pile) reportSession(s *session) {
	p.sendBmsStatus(s)
	p.sendBmsQa(s)
	p.sendRealTime(s)
}

func (p *Pile) sendBmsStatus(s *session) {
	body, desc := ykc.EncodeBmsStatus(ykc.BmsStatusInfo{
		TradeNo:      s.TradeNo,
		DeviceNo:     p.dev.DeviceNo,
		Gun:          s.GunNo,
		MaxCellNo:    2,
		BattMaxTempC: s.BattMaxTempC,
		MaxTempProbe: 1,
		BattMinTempC: s.BattMaxTempC - 5,
		MinTempProbe: 1,
		Status1:      0x00,
		Status2:      0x10, // bit4 = 充电允许
	})
	p.send(ykc.TypeChargingBmsStatus, body, desc, s.GunNo, nil)
}

func (p *Pile) sendBmsQa(s *session) {
	body, desc := ykc.EncodeBmsQa(ykc.BmsQaInfo{
		TradeNo:         s.TradeNo,
		DeviceNo:        p.dev.DeviceNo,
		Gun:             s.GunNo,
		NeedVoltageV:    s.VoltageV,
		NeedCurrentA:    s.CurrentA,
		ChargeMode:      0x02,
		MeasureVoltageV: s.VoltageV,
		MeasureCurrentA: s.CurrentA,
		MaxCellVoltageV: 3.70,
		MaxCellGroupNo:  1,
		Soc:             s.Soc,
		RemainMinutes:   p.remainMinutes(s),
		OutVoltageV:     s.VoltageV,
		OutCurrentA:     s.CurrentA,
		ChargedMinutes:  s.ElapsedSec / 60,
	})
	p.send(ykc.TypeChargingBmsQa, body, desc, s.GunNo, nil)
}

func (p *Pile) sendRealTime(s *session) {
	body, desc := ykc.EncodeRealTime(ykc.RealTimeInfo{
		TradeNo:        s.TradeNo,
		DeviceNo:       p.dev.DeviceNo,
		Gun:            s.GunNo,
		Status:         gunWorkCharging,
		GunHome:        0x00,
		Plugged:        0x01,
		VoltageV:       s.VoltageV,
		CurrentA:       s.CurrentA,
		GunTempC:       s.GunTempC,
		Soc:            s.Soc,
		BattMaxTempC:   s.BattMaxTempC,
		ChargedMinutes: s.ElapsedSec / 60,
		RemainMinutes:  p.remainMinutes(s),
		EnergyKwh:      s.totalEnergyKwh(),
		LossEnergyKwh:  s.totalEnergyKwh(),
		AmountYuan:     s.totalAmountYuan(),
	})
	p.send(ykc.TypeRealTimeUp, body, desc, s.GunNo, nil)
}

func (p *Pile) sendRealTimeSnapshot(gunNo byte) {
	g := p.gun(gunNo)
	s := &session{GunNo: gunNo, VoltageV: 0, CurrentA: 0}
	status := gunWorkIdle
	if g != nil && g.session != nil {
		s = g.session
		status = gunWorkCharging
	}
	body, desc := ykc.EncodeRealTime(ykc.RealTimeInfo{
		TradeNo:        s.TradeNo,
		DeviceNo:       p.dev.DeviceNo,
		Gun:            gunNo,
		Status:         status,
		GunHome:        0x01,
		Plugged:        boolToByte(g != nil && g.session != nil),
		VoltageV:       s.VoltageV,
		CurrentA:       s.CurrentA,
		GunTempC:       s.GunTempC,
		Soc:            s.Soc,
		BattMaxTempC:   s.BattMaxTempC,
		ChargedMinutes: s.ElapsedSec / 60,
		RemainMinutes:  p.remainMinutes(s),
		EnergyKwh:      s.totalEnergyKwh(),
		LossEnergyKwh:  s.totalEnergyKwh(),
		AmountYuan:     s.totalAmountYuan(),
	})
	p.send(ykc.TypeRealTimeUp, body, desc, gunNo, nil)
}

func (p *Pile) onRemoteStop(gunNo byte) {
	body, desc := ykc.EncodeRemoteStopAck(p.dev.DeviceNo, gunNo, resultSuccess, 0x00)
	p.send(ykc.TypeRemoteStopAck, body, desc, gunNo, nil)
	p.stopCharge(gunNo, stopByRemote, "平台远程停机")
}

func (p *Pile) onRemoteRestart(ctrl byte) {
	body, desc := ykc.EncodeSimpleAck(p.dev.DeviceNo, resultSuccess)
	p.send(ykc.TypeRemoteRestartAck, body, desc, 0, nil)
	if ctrl == 0x01 {
		p.logger.Eventf(p.dev.DeviceNo, "收到远程重启指令，模拟重启（断开重连）")
		p.conn.Close()
	}
}

// stopCharge 结束充电并上送交易记录。
func (p *Pile) stopCharge(gunNo byte, reason byte, by string) {
	g := p.gun(gunNo)
	if g == nil || g.session == nil || g.session.StopReason != 0 {
		return
	}
	s := g.session
	s.StopReason = reason
	s.StoppedBy = by
	s.EndTime = time.Now()
	g.session = nil

	p.logger.Event(p.dev.DeviceNo, gunNo,
		"充电结束(%s)，累计 %ds 电量 %.4fkWh 金额 %.4f元",
		by, s.ElapsedSec, s.totalEnergyKwh(), s.totalAmountYuan())

	p.sendChargeFinish(s)
	p.settling = append(p.settling, s)
	s.NextTradeAt = time.Now()
	p.sendTradeRecord(s)
	p.updateStage()
	p.refreshStatus()
}

func (p *Pile) sendChargeFinish(s *session) {
	body, desc := ykc.EncodeChargeFinish(ykc.ChargeFinishInfo{
		TradeNo:         s.TradeNo,
		DeviceNo:        p.dev.DeviceNo,
		Gun:             s.GunNo,
		Soc:             s.Soc,
		CellMinVoltageV: 3.50,
		CellMaxVoltageV: 3.70,
		BattMinTempC:    s.BattMaxTempC - 5,
		BattMaxTempC:    s.BattMaxTempC,
		Minutes:         s.ElapsedSec / 60,
		EnergyKwh:       s.totalEnergyKwh(),
		MotorNo:         0xFFFFFFFF,
	})
	p.send(ykc.TypeChargeFinish, body, desc, s.GunNo, nil)
}

func (p *Pile) sendTradeRecord(s *session) {
	s.TradeSent++
	info := ykc.TradeRecordInfo{
		WithMeter:       p.tradeFrame == ykc.TypeTradeUp2,
		TradeNo:         s.TradeNo,
		DeviceNo:        p.dev.DeviceNo,
		Gun:             s.GunNo,
		StartTime:       s.StartTime,
		EndTime:         s.EndTime,
		MeterNo:         p.meterNo,
		MeterProtoVer:   0,
		EncryptMode:     0,
		MeterStartKwh:   s.MeterStartKwh,
		MeterEndKwh:     s.meterEndKwh(),
		TotalEnergy:     s.Accum.TotalEnergy(),
		TotalLossEnergy: s.Accum.TotalEnergy(),
		TotalAmount:     s.Accum.TotalAmount(),
		Vin:             s.Vin,
		TradeFlag:       0x01,
		TradeTime:       time.Now(),
		StopReason:      s.StopReason,
	}
	for i := 0; i < 4; i++ {
		info.UnitPrice[i] = p.model.UnitPrice(byte(i))
		info.Energy[i] = s.Accum.Energy[i]
		info.LossEnergy[i] = s.Accum.Energy[i]
		info.Amount[i] = s.Accum.Amount[i]
	}
	body, desc := ykc.EncodeTradeRecord(info)
	p.send(p.tradeFrame, body, desc, s.GunNo, nil)
	next := time.Duration(p.cfg.Timing.TradeRetrySec) * time.Second
	s.NextTradeAt = time.Now().Add(next)
}

func (p *Pile) onTradeAck(tradeNo string, result byte) {
	for i, s := range p.settling {
		if s.TradeNo != tradeNo {
			continue
		}
		if result == 0x00 {
			s.TradeConfirmed = true
			p.logger.Event(p.dev.DeviceNo, s.GunNo, "交易记录已被平台确认（第 %d 次上送）", s.TradeSent)
		} else {
			p.logger.Event(p.dev.DeviceNo, s.GunNo, "平台判定为非法账单，停止重传")
			s.TradeConfirmed = true
		}
		p.settling = append(p.settling[:i], p.settling[i+1:]...)
		p.updateStage()
		return
	}
}

func (p *Pile) retrySettling() {
	if len(p.settling) == 0 {
		return
	}
	maxRetry := p.cfg.Timing.TradeRetry
	if maxRetry < 1 {
		maxRetry = 1
	}
	kept := p.settling[:0]
	for _, s := range p.settling {
		if s.TradeConfirmed {
			continue
		}
		if time.Now().Before(s.NextTradeAt) {
			kept = append(kept, s)
			continue
		}
		if s.TradeSent >= maxRetry {
			p.logger.Event(p.dev.DeviceNo, s.GunNo,
				"交易记录已重传 %d 次仍未收到确认，停止重传（请检查服务端是否实现 0x%02X）", s.TradeSent, p.tradeFrame)
			s.Settled = true
			continue
		}
		p.sendTradeRecord(s)
		kept = append(kept, s)
	}
	p.settling = kept
	p.updateStage()
}

// ---------------- 控制指令 ----------------

func (p *Pile) handleCommand(cmd Command) error {
	switch cmd.Kind {
	case CmdDeviceStart:
		p.running = true
		p.conn.SetEnabled(true)
		p.updateStage()
		p.refreshStatus()
		return nil
	case CmdDeviceStop:
		for _, g := range p.guns {
			if g.session != nil && g.session.StopReason == 0 {
				p.stopCharge(g.no, stopByManual, "HTTP 停止桩")
			}
		}
		p.running = false
		p.conn.SetEnabled(false)
		p.stage = StageStopped
		p.refreshStatus()
		return nil
	case CmdGunStart:
		if !p.running {
			return ErrNotRunning
		}
		if !p.connected {
			return fmt.Errorf("桩 %s 未连接 server", p.dev.DeviceNo)
		}
		g := p.gun(cmd.Gun)
		if g == nil {
			return fmt.Errorf("枪号 %d 不存在", cmd.Gun)
		}
		if g.session != nil {
			return fmt.Errorf("%d 号枪正在充电", cmd.Gun)
		}
		if p.model == nil || !p.billingReady {
			return fmt.Errorf("尚未获取到计费模型，按协议禁止充电")
		}
		p.tradeSeq++
		tradeNo := ykc.TradeNo(p.dev.DeviceNo, cmd.Gun, time.Now(), p.tradeSeq)
		// HTTP 主动触发的启机没有对应的 0x34 下行帧，直接走启机后的流程，不回 0x33
		p.logger.Event(p.dev.DeviceNo, cmd.Gun, "HTTP 触发启机，交易流水号=%s", tradeNo)
		p.beginCharge(cmd.Gun, tradeNo)
		return nil
	case CmdGunStop:
		if !p.running {
			return ErrNotRunning
		}
		g := p.gun(cmd.Gun)
		if g == nil {
			return fmt.Errorf("枪号 %d 不存在", cmd.Gun)
		}
		if g.session == nil || g.session.StopReason != 0 {
			return fmt.Errorf("%d 号枪未在充电", cmd.Gun)
		}
		p.stopCharge(cmd.Gun, stopByManual, cmd.Reason)
		return nil
	}
	return fmt.Errorf("未知指令: %d", cmd.Kind)
}

// ---------------- 状态与工具 ----------------

func (p *Pile) gun(no byte) *gun {
	for _, g := range p.guns {
		if g.no == no {
			return g
		}
	}
	return nil
}

func (p *Pile) updateStage() {
	switch {
	case !p.running:
		p.stage = StageStopped
	case !p.connected:
		p.stage = StageDisconnected
	case !p.billingReady:
		p.stage = StageHandshaking
	case p.anyCharging():
		p.stage = StageCharging
	case len(p.settling) > 0:
		p.stage = StageSettling
	default:
		p.stage = StageIdle
	}
	p.refreshStatus()
}

func (p *Pile) anyCharging() bool {
	for _, g := range p.guns {
		if g.session != nil && g.session.StopReason == 0 {
			return true
		}
	}
	return false
}

func (p *Pile) refreshStatus() {
	p.mu.Lock()
	st := DeviceStatus{
		DeviceNo:  p.dev.DeviceNo,
		GunCount:  p.dev.GunCount,
		Running:   p.running,
		Connected: p.connected,
		Stage:     string(p.stage),
		Server:    p.cfg.Addr(),
		Protocol:  fmt.Sprintf("%s %s", strings.ToUpper(p.cfg.Protocol.Type), p.codec.Version()),
	}
	if p.model != nil {
		st.BillingModel = p.model.String()
	}
	for _, g := range p.guns {
		gs := GunStatus{GunNo: int(g.no)}
		if s := g.session; s != nil {
			gs.Charging = true
			gs.TradeNo = s.TradeNo
			gs.StartAt = s.StartTime.Format("2006-01-02 15:04:05")
			gs.ElapsedSec = s.ElapsedSec
			gs.Soc = s.Soc
			gs.VoltageV = round2(s.VoltageV)
			gs.CurrentA = round2(s.CurrentA)
			gs.EnergyKwh = round4(s.totalEnergyKwh())
			gs.AmountYuan = round4(s.totalAmountYuan())
		}
		st.Guns = append(st.Guns, gs)
	}
	p.status = st
	p.mu.Unlock()
}

// send 组帧、记日志并入写队列。
func (p *Pile) send(typ ykc.FrameType, body []byte, desc string, gunNo byte, seqRaw *[2]byte) {
	var raw []byte
	if seqRaw != nil {
		raw = p.codec.EncodeEcho(*seqRaw, 0x00, typ, body)
	} else {
		seq := p.seq
		p.seq++
		raw = p.codec.Encode(seq, 0x00, typ, body)
	}
	p.logger.Frame(ykc.DirUp, p.codec.Version(), p.dev.DeviceNo, gunNo, typ, raw, desc)
	p.conn.Send(raw)
}

// sendEcho 用下行帧的原始序列号字节组帧，用于需要对齐序列号的应答。
func (p *Pile) sendEcho(typ ykc.FrameType, body []byte, desc string, gunNo byte, seqRaw [2]byte) {
	p.send(typ, body, desc, gunNo, &seqRaw)
}

// remainMinutes 估算剩余充电时间（分钟）。
func (p *Pile) remainMinutes(s *session) int {
	left := p.cfg.Timing.ChargeDurationSec - s.ElapsedSec
	if left <= 0 {
		return 0
	}
	return (left + 59) / 60
}

func boolToByte(v bool) byte {
	if v {
		return 0x01
	}
	return 0x00
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func round4(v float64) float64 { return float64(int64(v*10000+0.5)) / 10000 }
