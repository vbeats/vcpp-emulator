package sim

import (
	"math"
	"time"

	"github.com/csf/vcpp-emulator/internal/billing"
)

// session 一次充电会话。所有字段仅在桩 Actor 协程内访问。
type session struct {
	GunNo     byte
	TradeNo   string
	Vin       string
	StartTime time.Time
	EndTime   time.Time

	ElapsedSec      int
	VoltageV        float64
	CurrentA        float64
	Soc             int
	BattMaxTempC    int
	GunTempC        int
	MeterStartKwh   float64
	Energy1e4       int64
	Accum           billing.Accum
	StopReason      byte
	StoppedBy       string
	StartResultSent bool
	TradeSent       int
	TradeConfirmed  bool
	NextTradeAt     time.Time
	Settled         bool
}

// newSession 创建充电会话。
func newSession(gun byte, tradeNo, vin string, meterStart float64, soc int, voltage float64, powerKw float64) *session {
	cur := 0.0
	if voltage > 0 {
		cur = powerKw * 1000 / voltage
	}
	return &session{
		GunNo:         gun,
		TradeNo:       tradeNo,
		Vin:           vin,
		StartTime:     time.Now(),
		VoltageV:      voltage,
		CurrentA:      cur,
		Soc:           soc,
		BattMaxTempC:  30,
		GunTempC:      28,
		MeterStartKwh: meterStart,
	}
}

// advance 推进 seconds 秒的充电过程，按当前时刻所属费率段累计电量与金额。
func (s *session) advance(seconds int, powerKw float64, model *billing.Model,
	initialSoc int, packKwh float64) {
	now := time.Now()
	delta := int64(math.Round(powerKw * float64(seconds) / 3600.0 * billing.Scale4))
	slot := model.SlotAt(now)
	s.Accum.Add(slot, delta, model.UnitPrice(slot))
	s.Energy1e4 += delta
	s.ElapsedSec += seconds

	// 电量增加 -> SOC 上升，温度随电流缓慢抬升
	if packKwh > 0 {
		soc := initialSoc + int(float64(s.Energy1e4)/billing.Scale4/packKwh*100)
		if soc > 100 {
			soc = 100
		}
		s.Soc = soc
	}
	if s.Soc > 80 {
		s.BattMaxTempC = 38
	} else if s.Soc > 50 {
		s.BattMaxTempC = 35
	}
	s.GunTempC = 28 + int(s.CurrentA/40)
}

// totalEnergyKwh 总电量（kWh）。
func (s *session) totalEnergyKwh() float64 { return s.Accum.TotalEnergyKwh() }

// totalAmountYuan 总金额（元）。
func (s *session) totalAmountYuan() float64 { return s.Accum.TotalAmountYuan() }

// meterEndKwh 电表止值。
func (s *session) meterEndKwh() float64 { return s.MeterStartKwh + s.totalEnergyKwh() }
