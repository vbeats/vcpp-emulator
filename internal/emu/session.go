package emu

import (
	"math"
	"math/rand"
	"time"

	"vcpp-emulator/internal/ykc"
)

// 充电阶段
const (
	PhaseWaitAck    = "waitAck"    // 已发起申请，等待平台确认启动
	PhaseCharging   = "charging"   // 充电中
	PhaseStopping   = "stopping"   // 已结束充电，等待交易记录确认
	PhaseSettled    = "settled"    // 平台已确认交易记录
)

// Session 一次充电会话，承载车辆电池模型与实时计量数据
type Session struct {
	TradeNo   string
	StartType byte
	CardNo    string
	Vin       string
	StartTime time.Time
	EndTime   time.Time
	Target    ChargeTarget
	Phase     string

	// 车辆电池模型
	BatteryKwh float64
	StartSoc   float64
	Soc        float64
	MaxVoltage float64
	MinVoltage float64
	MaxCurrent float64

	// 实时计量
	Voltage     float64
	Current     float64
	Power       float64
	Energy      float64
	LossEnergy  float64
	Amount      float64
	GunTemp     int
	BatteryTemp int
	MeterStart  float64
	MeterEnd    float64
	Elapsed     float64 // 已充秒数

	// 分段统计（按费率段 / 半小时时段累计电量）
	SegEnergy []float64
	SegAmount []float64
	HalfHour  []float64

	// 流程控制
	StopReason  byte
	Balance     float64
	WaitUntil   time.Time // 等待平台确认的截止时刻
	TradeRetry  int       // 交易记录重发次数
	TradeSentAt time.Time
}

// NewSession 创建充电会话
func NewSession(tradeNo string, startType byte, cardNo, vin string, target ChargeTarget,
	batteryKwh float64, startSoc int, maxVoltage, maxCurrent float64, meterStart float64) *Session {
	if batteryKwh <= 0 {
		batteryKwh = 60
	}
	if startSoc < 0 || startSoc > 100 {
		startSoc = 20
	}
	if maxVoltage <= 0 {
		maxVoltage = 440
	}
	if maxCurrent <= 0 {
		maxCurrent = 250
	}
	return &Session{
		TradeNo:     tradeNo,
		StartType:   startType,
		CardNo:      cardNo,
		Vin:         vin,
		StartTime:   time.Now(),
		Target:      target,
		Phase:       PhaseWaitAck,
		BatteryKwh:  batteryKwh,
		StartSoc:    float64(startSoc),
		Soc:         float64(startSoc),
		MaxVoltage:  maxVoltage,
		MinVoltage:  maxVoltage * 0.80,
		MaxCurrent:  maxCurrent,
		MeterStart:  meterStart,
		MeterEnd:    meterStart,
		GunTemp:     25,
		BatteryTemp: 26,
		SegEnergy:   make([]float64, 8),
		SegAmount:   make([]float64, 8),
		HalfHour:    make([]float64, 48),
		WaitUntil:   time.Now().Add(30 * time.Second),
	}
}

// outputVoltage 依据 SOC 推算电池端电压：随 SOC 线性抬升并带轻微波动
func (s *Session) outputVoltage() float64 {
	ratio := s.Soc / 100
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	u := s.MinVoltage + (s.MaxVoltage-s.MinVoltage)*ratio
	u += (rand.Float64() - 0.5) * 1.5 // ±0.75V 波动
	return u
}

// demandPower 依据 SOC 与启动爬坡推算需求功率
func (s *Session) demandPower(ratedPower float64) float64 {
	// 启动 30s 内功率爬坡，模拟 BMS 握手后的电流渐升
	ramp := 1.0
	if s.Elapsed < 30 {
		ramp = 0.3 + 0.7*s.Elapsed/30
	}
	// SOC 越高可接受功率越低
	factor := 1.0
	switch {
	case s.Soc >= 95:
		factor = 0.4 - 0.3*(s.Soc-95)/5
	case s.Soc >= 80:
		factor = 1 - 0.6*(s.Soc-80)/15
	}
	if factor < 0.1 {
		factor = 0.1
	}
	p := ratedPower * ramp * factor
	// 电流抖动，避免数据完全线性
	p *= 0.99 + rand.Float64()*0.02
	if p < 0 {
		p = 0
	}
	return p
}

// Advance 推进 dt 秒的充电过程。
// price 为当前时段单价（元/度），seg 为当前费率段索引，ratedPower 为桩额定功率（kW）。
func (s *Session) Advance(dt float64, price float64, seg int, ratedPower float64) {
	if dt <= 0 {
		return
	}
	s.Elapsed += dt

	u := s.outputVoltage()
	p := s.demandPower(ratedPower)
	i := p * 1000 / u
	if i > s.MaxCurrent {
		i = s.MaxCurrent
	}
	// 枪线最小维持电流，避免恒为零
	if i < 0 {
		i = 0
	}
	p = u * i / 1000

	s.Voltage = u
	s.Current = i
	s.Power = p

	dEnergy := p * dt / 3600
	if dEnergy < 0 {
		dEnergy = 0
	}
	dAmount := dEnergy * price

	s.Energy += dEnergy
	s.LossEnergy += dEnergy
	s.Amount += dAmount
	s.MeterEnd = s.MeterStart + s.Energy

	if seg >= 0 && seg < len(s.SegEnergy) {
		s.SegEnergy[seg] += dEnergy
		s.SegAmount[seg] += dAmount
	}
	idx := time.Now().Hour()*2 + time.Now().Minute()/30
	if idx >= 0 && idx < len(s.HalfHour) {
		s.HalfHour[idx] += dEnergy
	}

	// SOC 按充入电量 / 电池容量推进
	if s.BatteryKwh > 0 {
		s.Soc += dEnergy / s.BatteryKwh * 100
	}
	if s.Soc > 100 {
		s.Soc = 100
	}

	// 温度一阶趋近，时间常数约 60s
	load := 0.0
	if s.MaxCurrent > 0 {
		load = i / s.MaxCurrent
	}
	targetGun := 25 + 28*load
	s.GunTemp = approach(s.GunTemp, targetGun, dt/60)
	targetBattery := 26 + 14*load + (s.Soc-20)*0.05
	s.BatteryTemp = approach(s.BatteryTemp, targetBattery, dt/120)
}

// approach 一阶惯性趋近
func approach(cur int, target, ratio float64) int {
	if ratio > 1 {
		ratio = 1
	}
	v := float64(cur) + (target-float64(cur))*ratio
	return int(math.Round(v))
}

// Minutes 已充分钟数
func (s *Session) Minutes() int { return int(s.Elapsed / 60) }

// RemainMinutes 预计剩余分钟数
func (s *Session) RemainMinutes() int {
	rate := 0.0
	switch s.Target.Mode {
	case TargetAmount:
		rate = s.Amount
	case TargetTime:
		rate = s.Elapsed / 60
	case TargetEnergy:
		rate = s.Energy
	case TargetSoc:
		rate = s.Soc
	default:
		return 0
	}
	target := s.targetValue()
	if rate <= 0 || target <= 0 {
		return 0
	}
	remain := (target - rate) / rate * (s.Elapsed / 60)
	if remain < 0 {
		remain = 0
	}
	return int(math.Ceil(remain))
}

// targetValue 目标值（与 Progress 分母一致）
func (s *Session) targetValue() float64 {
	switch s.Target.Mode {
	case TargetAmount:
		return s.Target.Amount
	case TargetTime:
		return s.Target.Minute
	case TargetEnergy:
		return s.Target.Energy
	case TargetSoc:
		return float64(s.Target.Soc)
	}
	return 0
}

// Progress 充电进度 0~1
func (s *Session) Progress() float64 {
	target := s.targetValue()
	if target <= 0 {
		return 0
	}
	var cur float64
	switch s.Target.Mode {
	case TargetAmount:
		cur = s.Amount
	case TargetTime:
		cur = s.Elapsed / 60
	case TargetEnergy:
		cur = s.Energy
	case TargetSoc:
		cur = s.Soc
	}
	v := cur / target
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v
}

// CheckTarget 判断是否达到充电目标，返回停止原因码
func (s *Session) CheckTarget() (bool, byte) {
	if s.Soc >= 100 {
		return true, ykc.StopReasonSocFull
	}
	switch s.Target.Mode {
	case TargetAmount:
		if s.Target.Amount > 0 && s.Amount >= s.Target.Amount {
			return true, ykc.StopReasonAmount
		}
	case TargetTime:
		if s.Target.Minute > 0 && s.Elapsed/60 >= s.Target.Minute {
			return true, ykc.StopReasonTime
		}
	case TargetEnergy:
		if s.Target.Energy > 0 && s.Energy >= s.Target.Energy {
			return true, ykc.StopReasonEnergy
		}
	case TargetSoc:
		if s.Target.Soc > 0 && s.Soc >= float64(s.Target.Soc) {
			return true, ykc.StopReasonSocFull
		}
	}
	return false, 0
}

// BuildRates 构造交易记录中的费率段明细（按费率段数量对齐）
func (s *Session) BuildRates(model *ykc.BillingModel) []ykc.RateAmount {
	count := 4
	if model != nil && len(model.Sections) > 0 {
		count = len(model.Sections)
	}
	if count > 8 {
		count = 8
	}
	rates := make([]ykc.RateAmount, count)
	for i := 0; i < count; i++ {
		price := 0.0
		if model != nil && i < len(model.Sections) {
			price = model.Sections[i].Price()
		}
		rates[i] = ykc.RateAmount{
			Price:      price,
			Energy:     s.SegEnergy[i],
			LossEnergy: s.SegEnergy[i],
			Amount:     s.SegAmount[i],
		}
	}
	return rates
}
