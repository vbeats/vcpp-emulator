package ykc

import (
	"strconv"
	"strings"
	"time"
)

// ---------------- 业务枚举 ----------------

// 枪状态（0x13 实时监测）
const (
	GunStatusOffline  byte = 0x00 // 离线
	GunStatusFault    byte = 0x01 // 故障
	GunStatusIdle     byte = 0x02 // 空闲
	GunStatusCharging byte = 0x03 // 充电
)

// 枪归位状态
const (
	GunHomeNo    byte = 0x00
	GunHomeYes   byte = 0x01
	GunHomeUnkown byte = 0x02
)

// 启动方式
const (
	StartTypeCard byte = 0x01 // 刷卡
	StartTypeAccount byte = 0x02 // 账号
	StartTypeVin  byte = 0x03 // VIN 码
)

// 交易标识
const (
	TradeTypeApp    byte = 0x01 // app 启动
	TradeTypeCard   byte = 0x02 // 卡启动
	TradeTypeOffline byte = 0x04 // 离线卡启动
	TradeTypeVin    byte = 0x05 // VIN 启动
)

// 停止原因
const (
	StopReasonRemote    byte = 0x40 // APP 远程停止
	StopReasonSocFull   byte = 0x41 // SOC 100%
	StopReasonEnergy    byte = 0x42 // 电量满足
	StopReasonAmount    byte = 0x43 // 金额满足
	StopReasonTime      byte = 0x44 // 时间满足
	StopReasonManual    byte = 0x45 // 手动停止
	StopReasonCardStop  byte = 0x46 // 刷卡停止
	StopReasonFault     byte = 0x6A // 系统闭锁（异常中止）
	StopReasonUnknown   byte = 0x90 // 未知原因
)

// StopReasonDesc 停止原因描述
func StopReasonDesc(r byte) string {
	switch r {
	case StopReasonRemote:
		return "APP 远程停止"
	case StopReasonSocFull:
		return "SOC 达到 100%"
	case StopReasonEnergy:
		return "充电电量满足设定条件"
	case StopReasonAmount:
		return "充电金额满足设定条件"
	case StopReasonTime:
		return "充电时间满足设定条件"
	case StopReasonManual:
		return "手动停止充电"
	case StopReasonCardStop:
		return "刷卡停止充电"
	case StopReasonFault:
		return "系统闭锁"
	default:
		return "未知原因停止"
	}
}

// 费率段名称
var sectionNames = []string{"尖", "峰", "平", "谷", "深谷"}

// SectionName 费率段名称
func SectionName(i int) string {
	if i >= 0 && i < len(sectionNames) {
		return sectionNames[i]
	}
	return "段"
}

// ---------------- 计费模型 ----------------

// Section 费率段（电费率 + 服务费率，单位 元/度）
type Section struct {
	Ep float64 `json:"ep"` // 电费率
	Sp float64 `json:"sp"` // 服务费率
}

// Price 合计单价
func (s Section) Price() float64 { return s.Ep + s.Sp }

// BillingModel 计费模型（0x0A / 0x58）
type BillingModel struct {
	ModelNo    uint16    `json:"modelNo"`    // 计费模型编号
	Sections   []Section `json:"sections"`   // 费率段
	LossRatio  byte      `json:"lossRatio"`  // 计损比例
	RateIndex  []byte    `json:"rateIndex"`  // 48 个半小时时段各自指向的费率段
}

// PriceAt 取指定时刻的单价（元/度）
func (m *BillingModel) PriceAt(t time.Time) float64 {
	if m == nil || len(m.Sections) == 0 {
		return 0
	}
	idx := t.Hour()*2 + t.Minute()/30
	if idx < 0 || idx > 47 {
		idx = 0
	}
	seg := 0
	if len(m.RateIndex) == 48 {
		seg = int(m.RateIndex[idx])
	}
	if seg < 0 || seg >= len(m.Sections) {
		seg = 0
	}
	return m.Sections[seg].Price()
}

// SegmentAt 取指定时刻的费率段索引
func (m *BillingModel) SegmentAt(t time.Time) int {
	if m == nil || len(m.Sections) == 0 {
		return 0
	}
	idx := t.Hour()*2 + t.Minute()/30
	if idx < 0 || idx > 47 {
		idx = 0
	}
	if len(m.RateIndex) == 48 {
		return int(m.RateIndex[idx])
	}
	return 0
}

// Describe 计费模型描述
func (m *BillingModel) Describe() string {
	if m == nil || len(m.Sections) == 0 {
		return "-"
	}
	var sb strings.Builder
	for i, s := range m.Sections {
		sb.WriteString(SectionName(i))
		sb.WriteString(":")
		sb.WriteString(formatFloat(s.Price(), 5))
		sb.WriteString(" ")
	}
	return strings.TrimSpace(sb.String())
}

func formatFloat(f float64, prec int) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', prec, 64), "0"), ".")
}

// ---------------- 上行报文构造 ----------------

// LoginInfo 登录参数
type LoginInfo struct {
	PileNo     string // 桩号（14 位数字）
	PileType   byte   // 0 直流 1 交流
	GunCount   byte   // 枪数量
	ProgramVer string // 程序版本（≤8 字符）
	NetType    byte   // 0 SIM 1 LAN 2 WAN 3 其他
	Sim        string // SIM 卡号（≤20 位数字）
	Supplier   byte   // 0 移动 2 电信 3 联通 4 其他
	Token      string // Token（V1.8+，≤14 位数字）
	Phone      string // 手机号码（V2.1）
	Secret     string // 随机密钥密文（V1.8+，88 字节）
}

// BuildLogin 构造 0x01 登录认证消息体
func (v *Spec) BuildLogin(info LoginInfo) []byte {
	ver := v.ProtocolVersionBytes()
	body := make([]byte, 0, 148)
	if v.LoginKind >= 1 {
		secret := make([]byte, 88)
		copy(secret, info.Secret)
		body = append(body, secret...)
	}
	body = append(body, BcdEncode(info.PileNo, 7)...)
	body = append(body, info.PileType, info.GunCount)
	body = append(body, ver...)
	prog := make([]byte, 8)
	copy(prog, info.ProgramVer)
	body = append(body, prog...)
	body = append(body, info.NetType)
	body = append(body, BcdEncode(info.Sim, 10)...)
	body = append(body, info.Supplier)
	if v.LoginKind >= 1 {
		body = append(body, BcdEncode(info.Token, 7)...)
	}
	if v.LoginKind >= 2 {
		phone := make([]byte, 11)
		copy(phone, info.Phone)
		body = append(body, phone...)
		body = append(body, 0x03, 0x03) // 支持网络制式 / 当前网络制式
		body = append(body, lonBytes(116.397, 39.909)...)
	}
	return body
}

// lonBytes 经纬度（4B，偏移 +180 / +90，6 位小数）
func lonBytes(lon, lat float64) []byte {
	lonWire := uint32((lon + 180) * 1_000_000)
	latWire := uint32((lat + 90) * 1_000_000)
	b := make([]byte, 8)
	b[0] = byte(lonWire >> 24)
	b[1] = byte(lonWire >> 16)
	b[2] = byte(lonWire >> 8)
	b[3] = byte(lonWire)
	b[4] = byte(latWire >> 24)
	b[5] = byte(latWire >> 16)
	b[6] = byte(latWire >> 8)
	b[7] = byte(latWire)
	return b
}

// BuildHeartbeat 构造 0x03 心跳消息体：桩编号 7B + 枪号 1B + 枪状态 1B
func BuildHeartbeat(pileNo string, gunNo byte, status byte) []byte {
	body := make([]byte, 0, 9)
	body = append(body, BcdEncode(pileNo, 7)...)
	body = append(body, gunNo, status)
	return body
}

// BuildBillingVerify 构造 0x05 计费模型验证请求：桩编号 7B + 模型编号 2B
func BuildBillingVerify(pileNo string, modelNo uint16) []byte {
	body := make([]byte, 0, 9)
	body = append(body, BcdEncode(pileNo, 7)...)
	body = append(body, byte(modelNo>>8), byte(modelNo))
	return body
}

// BuildBillingRequest 构造 0x09 计费模型请求（V2.1 携带计费模型编号）
func (v *Spec) BuildBillingRequest(pileNo string, modelNo uint16) []byte {
	body := make([]byte, 0, 9)
	body = append(body, BcdEncode(pileNo, 7)...)
	if v.VariableRate {
		body = append(body, byte(modelNo>>8), byte(modelNo))
	}
	return body
}

// RealTimeData 0x13 上传实时监测数据
type RealTimeData struct {
	TradeNo        string
	PileNo         string
	GunNo          byte
	Status         byte
	GunHome        byte
	Plugged        byte
	Voltage        float64 // V
	Current        float64 // A
	GunTemp        int     // ℃
	GunLineCode    uint64
	Soc            int // %
	BatteryMaxTemp int // ℃
	ChargeMinutes  int
	RemainMinutes  int
	Energy         float64 // kWh
	LossEnergy     float64 // kWh（计损）
	Amount         float64 // 元
	Fault          uint16
	// V2.1 追加
	PileTemp   int
	Smoke      byte
	MeterValue float64
}

// BuildRealTimeData 构造 0x13 消息体（V1.x 60B / V2.1 67B）
func (v *Spec) BuildRealTimeData(d RealTimeData) []byte {
	body := make([]byte, 0, 67)
	body = append(body, BcdEncode(d.TradeNo, 16)...)
	body = append(body, BcdEncode(d.PileNo, 7)...)
	body = append(body, d.GunNo, d.Status, d.GunHome, d.Plugged)
	body = append(body, u16(ToWireVoltage(d.Voltage))...)
	body = append(body, u16(ToWireVoltage(d.Current))...)
	body = append(body, ToWireTemp(d.GunTemp))
	body = append(body, u64(d.GunLineCode)...)
	body = append(body, byte(d.Soc), ToWireTemp(d.BatteryMaxTemp))
	body = append(body, u16(uint16(d.ChargeMinutes))...)
	body = append(body, u16(uint16(d.RemainMinutes))...)
	body = append(body, u32(ToWireQuantity(d.Energy))...)
	body = append(body, u32(ToWireQuantity(d.LossEnergy))...)
	body = append(body, u32(ToWireQuantity(d.Amount))...)
	body = append(body, byte(d.Fault), byte(d.Fault>>8))
	if v.RealTimeExtra {
		body = append(body, ToWireTemp(d.PileTemp), d.Smoke)
		body = append(body, Uint64ToBin5(uint64(d.MeterValue*10000))...)
	}
	return body
}

// ApplyCharge 0x31 / 0xA5 申请启动充电
type ApplyCharge struct {
	TradeNo   string
	PileNo    string
	GunNo     byte
	StartType byte   // 1 刷卡 2 账号 3 VIN
	NeedPwd   byte   // 0 不需要 1 需要
	CardNo    string // 账号或物理卡号（8B BIN，不足补 0）
	Password  string // MD5 16 位小写
	Vin       string // VIN 码（反序上送）
}

// BuildApplyCharge 构造申请启动充电消息体。
//
// 字段顺序说明：桩号前必须携带 16 字节交易流水号，
// 服务端按「交易流水号 + 桩编号 + 枪号」解析固定头，其后为业务字段。
func BuildApplyCharge(a ApplyCharge) []byte {
	body := make([]byte, 0, 67)
	body = append(body, BcdEncode(a.TradeNo, 16)...)
	body = append(body, BcdEncode(a.PileNo, 7)...)
	body = append(body, a.GunNo, a.StartType, a.NeedPwd)
	card := make([]byte, 8)
	copy(card, a.CardNo)
	body = append(body, card...)
	pwd := make([]byte, 16)
	copy(pwd, a.Password)
	body = append(body, pwd...)
	vin := make([]byte, 17)
	if a.StartType == StartTypeVin && a.Vin != "" {
		// VIN 码在鉴权帧中反序上送
		r := []byte(a.Vin)
		for i := 0; i < len(r)/2; i++ {
			r[i], r[len(r)-1-i] = r[len(r)-1-i], r[i]
		}
		copy(vin, r)
	}
	body = append(body, vin...)
	return body
}

// BuildRemoteStartAck 构造 0x33 / 0xA7 远程启机命令回复
// 交易流水号 16B + 桩编号 7B + 枪号 1B + 启动结果 1B + 失败原因 1B
func BuildRemoteStartAck(tradeNo, pileNo string, gunNo, result, reason byte) []byte {
	body := make([]byte, 0, 26)
	body = append(body, BcdEncode(tradeNo, 16)...)
	body = append(body, BcdEncode(pileNo, 7)...)
	body = append(body, gunNo, result, reason)
	return body
}

// BuildRemoteStopAck 构造 0x35 远程停机命令回复
// 桩编号 7B + 枪号 1B + 停止结果 1B + 失败原因 1B
func BuildRemoteStopAck(pileNo string, gunNo, result, reason byte) []byte {
	body := make([]byte, 0, 10)
	body = append(body, BcdEncode(pileNo, 7)...)
	body = append(body, gunNo, result, reason)
	return body
}

// BuildTimeSyncAck 构造 0x55 对时设置应答：桩编号 7B + 时间 7B
func BuildTimeSyncAck(pileNo string, t time.Time) []byte {
	body := make([]byte, 0, 14)
	body = append(body, BcdEncode(pileNo, 7)...)
	body = append(body, EncodeCP56(t)...)
	return body
}

// RateAmount 单个费率段的电量与金额
type RateAmount struct {
	Price    float64 // 单价（电价 + 服务费）
	Energy   float64 // 电量
	LossEnergy float64 // 计损电量
	Amount   float64 // 金额
}

// TradeRecord 交易记录（0x3B / 0x3D）
type TradeRecord struct {
	TradeNo     string
	PileNo      string
	GunNo       byte
	StartTime   time.Time
	EndTime     time.Time
	MeterNo     string // 电表表号 6B BCD（V1.7+）
	Rates       []RateAmount
	MeterStart  float64
	MeterEnd    float64
	TotalEnergy float64
	LossEnergy  float64
	Amount      float64
	Vin         string
	TradeType   byte
	TradeTime   time.Time
	StopReason  byte
	CardNo      uint64
	// V2.1：48 个半小时时段电量
	HalfHourKwh []float64
}

// BuildTrade 构造交易记录消息体（按版本选择 0x3B / 0x3D 布局）
func (v *Spec) BuildTrade(r TradeRecord) []byte {
	if v.Code == V21 {
		return buildTradeV21(r)
	}
	if v.Code == V17 || v.Code == V18 {
		return buildTradeV17(r)
	}
	return buildTradeV15(r)
}

// buildTradeV15 0x3B（V1.5 / V1.6）：固定尖峰平谷 4 段，含电表起止值
func buildTradeV15(r TradeRecord) []byte {
	body := make([]byte, 0, 158)
	body = append(body, BcdEncode(r.TradeNo, 16)...)
	body = append(body, BcdEncode(r.PileNo, 7)...)
	body = append(body, r.GunNo)
	body = append(body, EncodeCP56(r.StartTime)...)
	body = append(body, EncodeCP56(r.EndTime)...)
	for i := 0; i < 4; i++ {
		body = append(body, rateBlock(r.Rates, i)...)
	}
	body = append(body, Uint64ToBin5(uint64(r.MeterStart*10000))...)
	body = append(body, Uint64ToBin5(uint64(r.MeterEnd*10000))...)
	body = append(body, u32(ToWireQuantity(r.TotalEnergy))...)
	body = append(body, u32(ToWireQuantity(r.LossEnergy))...)
	body = append(body, u32(ToWireQuantity(r.Amount))...)
	body = append(body, vinBytes(r.Vin, false)...)
	body = append(body, r.TradeType)
	body = append(body, EncodeCP56(r.TradeTime)...)
	body = append(body, r.StopReason)
	body = append(body, u64(r.CardNo)...)
	return body
}

// buildTradeV17 0x3D（V1.7 / V1.8）：新增电表表号/密文/协议版本号/加密方式，深谷按长度识别
func buildTradeV17(r TradeRecord) []byte {
	body := make([]byte, 0, 217)
	body = append(body, BcdEncode(r.TradeNo, 16)...)
	body = append(body, BcdEncode(r.PileNo, 7)...)
	body = append(body, r.GunNo)
	body = append(body, EncodeCP56(r.StartTime)...)
	body = append(body, EncodeCP56(r.EndTime)...)
	body = append(body, BcdEncode(r.MeterNo, 6)...)
	body = append(body, make([]byte, 34)...) // 电表密文
	body = append(body, 0x01, 0x00)          // 电表协议版本号（小端）
	body = append(body, 0x00)                // 加密方式
	for i := 0; i < 4; i++ {
		body = append(body, rateBlock(r.Rates, i)...)
	}
	if len(r.Rates) > 4 {
		body = append(body, rateBlock(r.Rates, 4)...)
	}
	body = append(body, Uint64ToBin5(uint64(r.MeterStart*10000))...)
	body = append(body, Uint64ToBin5(uint64(r.MeterEnd*10000))...)
	body = append(body, u32(ToWireQuantity(r.TotalEnergy))...)
	body = append(body, u32(ToWireQuantity(r.LossEnergy))...)
	body = append(body, u32(ToWireQuantity(r.Amount))...)
	body = append(body, vinBytes(r.Vin, false)...)
	body = append(body, r.TradeType)
	body = append(body, EncodeCP56(r.TradeTime)...)
	body = append(body, r.StopReason)
	body = append(body, u64(r.CardNo)...)
	return body
}

// buildTradeV21 0x3D（V2.1）：费率时段数量 + N 段明细 + 48 个半小时时段电量
func buildTradeV21(r TradeRecord) []byte {
	body := make([]byte, 0, 394)
	body = append(body, BcdEncode(r.TradeNo, 16)...)
	body = append(body, BcdEncode(r.PileNo, 7)...)
	body = append(body, r.GunNo)
	body = append(body, EncodeCP56(r.StartTime)...)
	body = append(body, EncodeCP56(r.EndTime)...)
	body = append(body, BcdEncode(r.MeterNo, 6)...)
	body = append(body, make([]byte, 34)...)
	body = append(body, 0x01, 0x00)
	body = append(body, 0x00)
	body = append(body, Uint64ToBin5(uint64(r.MeterStart*10000))...)
	body = append(body, Uint64ToBin5(uint64(r.MeterEnd*10000))...)
	body = append(body, u32(ToWireQuantity(r.TotalEnergy))...)
	body = append(body, u32(ToWireQuantity(r.LossEnergy))...)
	body = append(body, u32(ToWireQuantity(r.Amount))...)
	body = append(body, vinBytes(r.Vin, false)...)
	body = append(body, r.TradeType)
	body = append(body, EncodeCP56(r.TradeTime)...)
	body = append(body, r.StopReason)
	body = append(body, u64(r.CardNo)...)
	body = append(body, byte(len(r.Rates)))
	for _, rate := range r.Rates {
		body = append(body, u32(ToWirePrice(rate.Price))...)
		body = append(body, u32(ToWireQuantity(rate.Energy))...)
		body = append(body, u32(ToWireQuantity(rate.LossEnergy))...)
		body = append(body, u32(ToWireQuantity(rate.Amount))...)
	}
	for i := 0; i < 48; i++ {
		var kwh float64
		if i < len(r.HalfHourKwh) {
			kwh = r.HalfHourKwh[i]
		}
		body = append(body, u32(ToWireQuantity(kwh))...)
	}
	return body
}

// rateBlock 单价 4B + 电量 4B + 计损电量 4B + 金额 4B
func rateBlock(rates []RateAmount, i int) []byte {
	out := make([]byte, 16)
	if i < len(rates) {
		copy(out[0:], u32(ToWirePrice(rates[i].Price)))
		copy(out[4:], u32(ToWireQuantity(rates[i].Energy)))
		copy(out[8:], u32(ToWireQuantity(rates[i].LossEnergy)))
		copy(out[12:], u32(ToWireQuantity(rates[i].Amount)))
	}
	return out
}

// vinBytes VIN 码 17B，无 VIN 时全 0xFF
func vinBytes(vin string, reverse bool) []byte {
	out := make([]byte, 17)
	if vin == "" {
		for i := range out {
			out[i] = 0xFF
		}
		return out
	}
	b := []byte(vin)
	if reverse {
		for i := 0; i < len(b)/2; i++ {
			b[i], b[len(b)-1-i] = b[len(b)-1-i], b[i]
		}
	}
	copy(out, b)
	return out
}

// ---------------- 下行报文解析 ----------------

// LoginAck 0x02 登录应答
type LoginAck struct {
	PileNo string
	Result byte // 0x00 成功 0x01 失败
	Time   time.Time
}

// ParseLoginAck 解析 0x02（桩编号 7B + 结果 1B + 系统时间 7B，尾部时间可能缺失）
func ParseLoginAck(body []byte) LoginAck {
	ack := LoginAck{Result: 0x01}
	if len(body) >= 7 {
		ack.PileNo = BcdDecode(body[:7])
	}
	if len(body) >= 8 {
		ack.Result = body[7]
	}
	if len(body) >= 15 {
		if t, err := DecodeCP56(body[8:15]); err == nil {
			ack.Time = t
		}
	}
	return ack
}

// ParseBillingVerifyAck 解析 0x06（桩编号 7B + 模型编号 2B + 验证结果 1B）
func ParseBillingVerifyAck(body []byte) (modelNo uint16, result byte) {
	if len(body) < 10 {
		return
	}
	modelNo = U16BE(body[7:9])
	result = body[9]
	return
}

// ParseBillingModel 解析 0x0A / 0x58 计费模型
func (v *Spec) ParseBillingModel(body []byte) *BillingModel {
	m := &BillingModel{}
	if len(body) < 9 {
		return m
	}
	m.ModelNo = U16BE(body[7:9])

	offset := 9
	count := v.RateSegment
	if v.VariableRate {
		if len(body) <= offset {
			return m
		}
		count = int(body[offset])
		offset++
	} else {
		count = (len(body) - 58) / 8
	}
	if count < 0 {
		count = 0
	}
	if count > 48 {
		count = 48
	}
	for i := 0; i < count && offset+8 <= len(body); i++ {
		m.Sections = append(m.Sections, Section{
			Ep: WirePrice(U32LE(body[offset:])),
			Sp: WirePrice(U32LE(body[offset+4:])),
		})
		offset += 8
	}
	if offset < len(body) {
		m.LossRatio = body[offset]
		offset++
	}
	if offset+48 <= len(body) {
		m.RateIndex = append(m.RateIndex, body[offset:offset+48]...)
	}
	return m
}

// ApplyChargeAck 0x32 / 0xA6 确认启动充电
type ApplyChargeAck struct {
	TradeNo    string
	PileNo     string
	GunNo      byte
	CardNo     string  // 逻辑卡号 8B BCD
	Balance    float64 // 账户余额
	MaxPower   float64 // 最大功率 kW（V1.8+ 0xA6）
	AuthFlag   byte    // 鉴权成功标志
	FailReason byte
}

// ParseApplyChargeAck 解析 0x32 / 0xA6
func ParseApplyChargeAck(body []byte) ApplyChargeAck {
	ack := ApplyChargeAck{}
	if len(body) < 24 {
		return ack
	}
	ack.TradeNo = BcdDecode(body[:16])
	ack.PileNo = BcdDecode(body[16:23])
	ack.GunNo = body[23]
	if len(body) >= 32 {
		ack.CardNo = BcdDecode(body[24:32])
	}
	if len(body) >= 36 {
		ack.Balance = float64(U32LE(body[32:36])) / 100
	}
	switch {
	case len(body) >= 40:
		// V1.8+ 0xA6：余额之后携带 2B 最大功率，再跟鉴权标志与失败原因
		ack.MaxPower = float64(U16LE(body[36:38]))
		ack.AuthFlag = body[38]
		ack.FailReason = body[39]
	case len(body) >= 26:
		// V1.5~V1.7 0x32：流水号 + 桩号 + 枪号 + 结果(2B)
		ack.AuthFlag = body[24]
		ack.FailReason = body[25]
	}
	return ack
}

// RemoteCharge 0x34 / 0xA8 远程控制启机
type RemoteCharge struct {
	TradeNo  string
	PileNo   string
	GunNo    byte
	CardNo   string  // 逻辑卡号
	PhyCard  uint64  // 物理卡号
	Balance  float64 // 账户余额
}

// ParseRemoteCharge 解析 0x34 / 0xA8
func ParseRemoteCharge(body []byte) RemoteCharge {
	rc := RemoteCharge{}
	if len(body) < 24 {
		return rc
	}
	rc.TradeNo = BcdDecode(body[:16])
	rc.PileNo = BcdDecode(body[16:23])
	rc.GunNo = body[23]
	if len(body) >= 32 {
		rc.CardNo = BcdDecode(body[24:32])
	}
	if len(body) >= 40 {
		rc.PhyCard = U64LE(body[32:40])
	}
	if len(body) >= 44 {
		rc.Balance = float64(U32LE(body[40:44])) / 100
	}
	return rc
}

// ParseRemoteStop 解析 0x36 远程停机（桩编号 7B + 枪号 1B）
func ParseRemoteStop(body []byte) (pileNo string, gunNo byte) {
	if len(body) < 8 {
		return
	}
	return BcdDecode(body[:7]), body[7]
}

// ParseTradeAck 解析 0x40 交易记录确认（流水号 16B + 结果 1B）
func ParseTradeAck(body []byte) (tradeNo string, result byte) {
	if len(body) < 17 {
		return
	}
	return BcdDecode(body[:16]), body[16]
}

// ParseTimeSync 解析 0x56 对时设置（桩编号 7B + 时间 7B）
func ParseTimeSync(body []byte) (pileNo string, t time.Time) {
	if len(body) < 7 {
		return
	}
	pileNo = BcdDecode(body[:7])
	if len(body) >= 14 {
		if v, err := DecodeCP56(body[7:14]); err == nil {
			t = v
		}
	}
	return
}

// ParseReadRealTime 解析 0x12 读取实时监测数据（桩编号 7B + 枪号 1B）
func ParseReadRealTime(body []byte) (pileNo string, gunNo byte) {
	if len(body) < 8 {
		return
	}
	return BcdDecode(body[:7]), body[7]
}

// 小端写入辅助
func u16(v uint16) []byte {
	b := make([]byte, 2)
	PutU16LE(b, v)
	return b
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	PutU32LE(b, v)
	return b
}

func u64(v uint64) []byte {
	b := make([]byte, 8)
	PutU64LE(b, v)
	return b
}
