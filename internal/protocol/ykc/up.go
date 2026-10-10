package ykc

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// 本文件实现上行帧（⬆️ 模拟器 -> server）的消息体编码。
// 每个 Encode 方法返回消息体与人类可读的解析描述，日志直接复用解析描述，保证日志与编码同源。

func u16f1(v float64) uint16 { return uint16(math.Round(v * Scale1)) }

func u16f2(v float64) uint16 { return uint16(math.Round(v * Scale2)) }

func u32f4(v float64) uint32 { return uint32(math.Round(v * Scale4)) }

func u32f5(v float64) uint32 { return uint32(math.Round(v * Scale5)) }

// curRaw 电流类字段编码：协议为 raw*0.1-400，故 raw = (A+400)*10。
func curRaw(a float64) uint16 { return uint16(math.Round((a + 400) * Scale1)) }

// ---------------- 0x01 登录认证 ----------------

// LoginInfo 登录认证（0x01）内容。
type LoginInfo struct {
	DeviceNo   string
	PileType   byte
	GunCount   byte
	ProtoVer   byte
	McuVersion string
	NetType    byte
	Sim        string
	Supplier   byte
}

// EncodeLogin 编码登录认证消息体（30 字节）。
func EncodeLogin(v LoginInfo) ([]byte, string) {
	w := NewW(30)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.PileType)
	w.U8(v.GunCount)
	w.U8(v.ProtoVer)
	w.ASCII(v.McuVersion, 8)
	w.U8(v.NetType)
	w.BCD(v.Sim, 10)
	w.U8(v.Supplier)
	desc := fmt.Sprintf(
		"桩号=%s 桩类型=%d[%s] 枪数量=%d 通信协议版本=%d mcu程序版本=%s 网络连接类型=%d[%s] sim卡号=%s 运营商=%d[%s]",
		v.DeviceNo, v.PileType, pileTypeDesc(v.PileType), v.GunCount, v.ProtoVer,
		strings.TrimRight(v.McuVersion, "\x00"), v.NetType, netTypeDesc(v.NetType), v.Sim, v.Supplier, supplierDesc(v.Supplier))
	return w.Bytes(), desc
}

// ---------------- 0x03 心跳 ----------------

// EncodeHeartbeat 编码心跳包消息体（9 字节）。
func EncodeHeartbeat(deviceNo string, gun byte, gunStatus byte) ([]byte, string) {
	w := NewW(9)
	w.BCD(deviceNo, 7)
	w.U8(gun)
	w.U8(gunStatus)
	desc := fmt.Sprintf("桩号=%s 枪号=%02d 枪状态=%d[%s]", deviceNo, gun, gunStatus, gunStatusDesc(gunStatus))
	return w.Bytes(), desc
}

// ---------------- 0x05 计费模型验证 ----------------

// EncodeBillingVerify 编码计费模型验证请求消息体（9 字节）。modelNo 为 4 位数字串，首次连接传 "0000"。
func EncodeBillingVerify(deviceNo, modelNo string) ([]byte, string) {
	w := NewW(9)
	w.BCD(deviceNo, 7)
	w.BCD(modelNo, 2)
	desc := fmt.Sprintf("桩号=%s 计费模型编号=%s", deviceNo, modelNo)
	return w.Bytes(), desc
}

// ---------------- 0x09 计费模型请求 ----------------

// EncodeBillingModelRequest 编码计费模型请求消息体（7 字节）。
func EncodeBillingModelRequest(deviceNo string) ([]byte, string) {
	w := NewW(7)
	w.BCD(deviceNo, 7)
	return w.Bytes(), fmt.Sprintf("桩号=%s", deviceNo)
}

// ---------------- 0x55 对时应答 ----------------

// EncodeTimeSyncAck 编码对时设置应答消息体（14 字节）。
func EncodeTimeSyncAck(deviceNo string, t time.Time) ([]byte, string) {
	w := NewW(14)
	w.BCD(deviceNo, 7)
	w.Time7(t)
	desc := fmt.Sprintf("桩号=%s 当前时间=%s", deviceNo, t.Format("2006-01-02 15:04:05.000"))
	return w.Bytes(), desc
}

// ---------------- 0x33 远程启机回复 / 0x35 远程停机回复 ----------------

// EncodeRemoteStartAck 编码远程启机命令回复消息体（26 字节）。
func EncodeRemoteStartAck(tradeNo, deviceNo string, gun byte, result byte, errCode byte) ([]byte, string) {
	w := NewW(26)
	w.BCD(tradeNo, 16)
	w.BCD(deviceNo, 7)
	w.U8(gun)
	w.U8(result)
	w.U8(errCode)
	desc := fmt.Sprintf("交易流水号=%s 桩号=%s 枪号=%02d 启动结果=%d[%s] 失败原因=%d[%s]",
		tradeNo, deviceNo, gun, result, boolDesc(result), errCode, startErrDesc(errCode))
	return w.Bytes(), desc
}

// EncodeRemoteStopAck 编码远程停机命令回复消息体（10 字节）。
func EncodeRemoteStopAck(deviceNo string, gun byte, result byte, errCode byte) ([]byte, string) {
	w := NewW(10)
	w.BCD(deviceNo, 7)
	w.U8(gun)
	w.U8(result)
	w.U8(errCode)
	desc := fmt.Sprintf("桩号=%s 枪号=%02d 停止结果=%d[%s] 失败原因=%d[%s]",
		deviceNo, gun, result, boolDesc(result), errCode, stopErrDesc(errCode))
	return w.Bytes(), desc
}

// ---------------- 0x15 充电握手 ----------------

// HandshakeInfo 充电握手（0x15）内容。
type HandshakeInfo struct {
	TradeNo         string
	DeviceNo        string
	Gun             byte
	BatteryType     byte
	RatedCapacityAh float64
	RatedVoltageV   float64
	Vendor          string
	PackSerial      uint32
	ProduceDate     time.Time
	ChargeTimes     uint32
	Ownership       byte
	Vin             string
	BmsSwSerial     byte
	BmsSwDate       time.Time
}

// EncodeHandshake 编码充电握手消息体（73 字节）。
func EncodeHandshake(v HandshakeInfo) ([]byte, string) {
	w := NewW(73)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	// BMS 通信协议版本号 V1.1 -> 01 01 00
	w.U8(0x01)
	w.U8(0x01)
	w.U8(0x00)
	w.U8(v.BatteryType)
	w.U16LE(u16f1(v.RatedCapacityAh))
	w.U16LE(u16f1(v.RatedVoltageV))
	w.ASCII(v.Vendor, 4)
	w.U32LE(v.PackSerial)
	year := v.ProduceDate.Year() - 1985
	if year < 0 {
		year = 0
	}
	if year > 255 {
		year = 255
	}
	w.U8(byte(year))
	w.U8(byte(v.ProduceDate.Month()))
	w.U8(byte(v.ProduceDate.Day()))
	w.U24LE(v.ChargeTimes)
	w.U8(v.Ownership)
	w.U8(0x00)
	w.ASCII(v.Vin, 17)
	w.U8(v.BmsSwSerial)
	w.U8(byte(v.BmsSwDate.Day()))
	w.U8(byte(v.BmsSwDate.Month()))
	w.U16LE(uint16(v.BmsSwDate.Year()))
	w.U8(0xFF)
	w.U8(0xFF)
	w.U8(0xFF)

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d BMS协议版本=1.1 电池类型=%d[%s] 额定容量=%.1fAh 额定总电压=%.1fV 厂商=%s 电池组序号=%d 生产日期=%s 充电次数=%d 产权=%d[%s] VIN=%s BMS软件版本=%s",
		v.TradeNo, v.DeviceNo, v.Gun, v.BatteryType, batteryTypeDesc(v.BatteryType),
		v.RatedCapacityAh, v.RatedVoltageV, strings.TrimSpace(v.Vendor), v.PackSerial,
		v.ProduceDate.Format("2006-01-02"), v.ChargeTimes, v.Ownership, ownershipDesc(v.Ownership),
		strings.TrimRight(v.Vin, "\x00"), v.BmsSwDate.Format("2006-01-02"))
	return w.Bytes(), desc
}

// ---------------- 0x17 参数配置 ----------------

// ParamConfigInfo 参数配置（0x17）内容。
type ParamConfigInfo struct {
	TradeNo           string
	DeviceNo          string
	Gun               byte
	CellMaxVoltageV   float64
	MaxChargeCurrentA float64
	RatedEnergyKwh    float64
	MaxTotalVoltageV  float64
	MaxTempC          int
	Soc               float64
	BatteryVoltageV   float64
	PileMaxVoltageV   float64
	PileMinVoltageV   float64
	PileMaxCurrentA   float64
	PileMinCurrentA   float64
}

// EncodeParamConfig 编码参数配置消息体（45 字节）。
func EncodeParamConfig(v ParamConfigInfo) ([]byte, string) {
	w := NewW(45)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.U16LE(u16f2(v.CellMaxVoltageV))
	w.U16LE(curRaw(v.MaxChargeCurrentA))
	w.U16LE(u16f1(v.RatedEnergyKwh))
	w.U16LE(u16f1(v.MaxTotalVoltageV))
	w.Temp(v.MaxTempC)
	w.U16LE(u16f1(v.Soc))
	w.U16LE(u16f1(v.BatteryVoltageV))
	w.U16LE(u16f1(v.PileMaxVoltageV))
	w.U16LE(u16f1(v.PileMinVoltageV))
	w.U16LE(curRaw(v.PileMaxCurrentA))
	w.U16LE(curRaw(v.PileMinCurrentA))

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d 单体最高允许充电电压=%.2fV 最高允许充电电流=%.1fA 标称总能量=%.1fkWh 最高允许充电总电压=%.1fV 最高允许温度=%d℃ 整车SOC=%.1f%% 当前电池电压=%.1fV 电桩输出电压范围=%.1f~%.1fV 电桩输出电流范围=%.1f~%.1fA",
		v.TradeNo, v.DeviceNo, v.Gun, v.CellMaxVoltageV, v.MaxChargeCurrentA, v.RatedEnergyKwh,
		v.MaxTotalVoltageV, v.MaxTempC, v.Soc, v.BatteryVoltageV, v.PileMinVoltageV, v.PileMaxVoltageV,
		v.PileMinCurrentA, v.PileMaxCurrentA)
	return w.Bytes(), desc
}

// ---------------- 0x25 充电过程 BMS 信息 ----------------

// BmsStatusInfo 充电过程 BMS 信息（0x25）内容。
type BmsStatusInfo struct {
	TradeNo      string
	DeviceNo     string
	Gun          byte
	MaxCellNo    int
	BattMaxTempC int
	MaxTempProbe int
	BattMinTempC int
	MinTempProbe int
	Status1      byte
	Status2      byte
}

// EncodeBmsStatus 编码充电过程 BMS 信息消息体（31 字节）。
func EncodeBmsStatus(v BmsStatusInfo) ([]byte, string) {
	w := NewW(31)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.U8(byte(clamp(v.MaxCellNo-1, 0, 255)))
	w.Temp(v.BattMaxTempC)
	w.U8(byte(clamp(v.MaxTempProbe-1, 0, 255)))
	w.Temp(v.BattMinTempC)
	w.U8(byte(clamp(v.MinTempProbe-1, 0, 255)))
	w.U8(v.Status1)
	w.U8(v.Status2)

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d 最高单体电压所在编号=%d 电池最高温度=%d℃(检测点%d) 电池最低温度=%d℃(检测点%d) 状态1=%s 状态2=%s",
		v.TradeNo, v.DeviceNo, v.Gun, v.MaxCellNo, v.BattMaxTempC, v.MaxTempProbe,
		v.BattMinTempC, v.MinTempProbe, bmsStatus1Desc(v.Status1), bmsStatus2Desc(v.Status2))
	return w.Bytes(), desc
}

// ---------------- 0x23 充电过程 BMS 需求与充电机输出 ----------------

// BmsQaInfo 充电过程 BMS 需求与充电机输出（0x23）内容。
type BmsQaInfo struct {
	TradeNo         string
	DeviceNo        string
	Gun             byte
	NeedVoltageV    float64
	NeedCurrentA    float64
	ChargeMode      byte
	MeasureVoltageV float64
	MeasureCurrentA float64
	MaxCellVoltageV float64
	MaxCellGroupNo  int
	Soc             int
	RemainMinutes   int
	OutVoltageV     float64
	OutCurrentA     float64
	ChargedMinutes  int
}

// EncodeBmsQa 编码充电过程 BMS 需求与充电机输出消息体（44 字节）。
func EncodeBmsQa(v BmsQaInfo) ([]byte, string) {
	w := NewW(44)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.U16LE(u16f1(v.NeedVoltageV))
	w.U16LE(curRaw(v.NeedCurrentA))
	w.U8(v.ChargeMode)
	w.U16LE(u16f1(v.MeasureVoltageV))
	w.U16LE(curRaw(v.MeasureCurrentA))
	cell := uint16(v.MaxCellGroupNo&0x0F)<<12 | (u16f2(v.MaxCellVoltageV) & 0x0FFF)
	w.U16LE(cell)
	w.U8(byte(clamp(v.Soc, 0, 100)))
	w.U16LE(uint16(clamp(v.RemainMinutes, 0, 65535)))
	w.U16LE(u16f1(v.OutVoltageV))
	w.U16LE(curRaw(v.OutCurrentA))
	w.U16LE(uint16(clamp(v.ChargedMinutes, 0, 65535)))

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d BMS电压需求=%.1fV BMS电流需求=%.1fA 充电模式=%d[%s] 电压测量=%.1fV 电流测量=%.1fA 最高单体电压=%.2fV(组号%d) SOC=%d%% 估算剩余时间=%dmin 电桩输出电压=%.1fV 电桩输出电流=%.1fA 累计充电时间=%dmin",
		v.TradeNo, v.DeviceNo, v.Gun, v.NeedVoltageV, v.NeedCurrentA, v.ChargeMode, chargeModeDesc(v.ChargeMode),
		v.MeasureVoltageV, v.MeasureCurrentA, v.MaxCellVoltageV, v.MaxCellGroupNo, v.Soc, v.RemainMinutes,
		v.OutVoltageV, v.OutCurrentA, v.ChargedMinutes)
	return w.Bytes(), desc
}

// ---------------- 0x13 上传实时监测数据 ----------------

// RealTimeInfo 上传实时监测数据（0x13）内容。
type RealTimeInfo struct {
	TradeNo        string
	DeviceNo       string
	Gun            byte
	Status         byte
	GunHome        byte
	Plugged        byte
	VoltageV       float64
	CurrentA       float64
	GunTempC       int
	GunLineCode    uint64
	Soc            int
	BattMaxTempC   int
	ChargedMinutes int
	RemainMinutes  int
	EnergyKwh      float64
	LossEnergyKwh  float64
	AmountYuan     float64
	HwFault        uint16
}

// EncodeRealTime 编码上传实时监测数据消息体（60 字节）。
func EncodeRealTime(v RealTimeInfo) ([]byte, string) {
	w := NewW(60)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.U8(v.Status)
	w.U8(v.GunHome)
	w.U8(v.Plugged)
	w.U16LE(u16f1(v.VoltageV))
	w.U16LE(u16f1(v.CurrentA))
	w.Temp(v.GunTempC)
	w.U64LE(v.GunLineCode)
	w.U8(byte(clamp(v.Soc, 0, 100)))
	w.Temp(v.BattMaxTempC)
	w.U16LE(uint16(clamp(v.ChargedMinutes, 0, 65535)))
	w.U16LE(uint16(clamp(v.RemainMinutes, 0, 65535)))
	w.U32LE(u32f4(v.EnergyKwh))
	w.U32LE(u32f4(v.LossEnergyKwh))
	w.U32LE(u32f4(v.AmountYuan))
	w.U16LE(v.HwFault)

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d 状态=%d[%s] 枪归位=%d 插枪=%d 输出电压=%.1fV 输出电流=%.1fA 枪线温度=%d℃ SOC=%d%% 电池最高温度=%d℃ 累计充电时间=%dmin 剩余时间=%dmin 充电度数=%.4fkWh 计损充电度数=%.4fkWh 已充金额=%.4f元 硬件故障=%s",
		v.TradeNo, v.DeviceNo, v.Gun, v.Status, gunWorkStatusDesc(v.Status), v.GunHome, v.Plugged,
		v.VoltageV, v.CurrentA, v.GunTempC, v.Soc, v.BattMaxTempC, v.ChargedMinutes, v.RemainMinutes,
		v.EnergyKwh, v.LossEnergyKwh, v.AmountYuan, hwFaultDesc(v.HwFault))
	return w.Bytes(), desc
}

// ---------------- 0x19 充电结束 / 0x1D BMS 中止 / 0x21 充电机中止 ----------------

// ChargeFinishInfo 充电结束（0x19）内容。
type ChargeFinishInfo struct {
	TradeNo         string
	DeviceNo        string
	Gun             byte
	Soc             int
	CellMinVoltageV float64
	CellMaxVoltageV float64
	BattMinTempC    int
	BattMaxTempC    int
	Minutes         int
	EnergyKwh       float64
	MotorNo         uint32
}

// EncodeChargeFinish 编码充电结束消息体（39 字节）。
func EncodeChargeFinish(v ChargeFinishInfo) ([]byte, string) {
	w := NewW(39)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.U8(byte(clamp(v.Soc, 0, 100)))
	w.U16LE(u16f2(v.CellMinVoltageV))
	w.U16LE(u16f2(v.CellMaxVoltageV))
	w.Temp(v.BattMinTempC)
	w.Temp(v.BattMaxTempC)
	w.U16LE(uint16(clamp(v.Minutes, 0, 65535)))
	w.U16LE(u16f1(v.EnergyKwh))
	w.U32LE(v.MotorNo)

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d 中止SOC=%d%% 单体电压范围=%.2f~%.2fV 电池温度范围=%d~%d℃ 累计充电时间=%dmin 输出能量=%.1fkWh 充电机编号=%08X",
		v.TradeNo, v.DeviceNo, v.Gun, v.Soc, v.CellMinVoltageV, v.CellMaxVoltageV,
		v.BattMinTempC, v.BattMaxTempC, v.Minutes, v.EnergyKwh, v.MotorNo)
	return w.Bytes(), desc
}

// StopReasonInfo 中止帧（0x1D / 0x21）内容。
type StopReasonInfo struct {
	TradeNo     string
	DeviceNo    string
	Gun         byte
	Reason      byte
	FaultReason uint16
	ErrReason   byte
}

// EncodeStopReason 编码中止帧消息体（28 字节），0x1D 与 0x21 结构相同。
func EncodeStopReason(v StopReasonInfo) ([]byte, string) {
	w := NewW(28)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.U8(v.Reason)
	w.U16LE(v.FaultReason)
	w.U8(v.ErrReason)
	desc := fmt.Sprintf("交易流水号=%s 桩号=%s 枪号=%02d 中止原因=0x%02X 故障原因=0x%04X 错误原因=0x%02X",
		v.TradeNo, v.DeviceNo, v.Gun, v.Reason, v.FaultReason, v.ErrReason)
	return w.Bytes(), desc
}

// ---------------- 0x3D / 0x3B 交易记录 ----------------

// TradeRecordInfo 交易记录内容。WithMeter=true 时为 V1.7 的 0x3D（201 字节），
// 否则为 V1.5/V1.6 的 0x3B（158 字节，无电表块）。
type TradeRecordInfo struct {
	WithMeter       bool
	TradeNo         string
	DeviceNo        string
	Gun             byte
	StartTime       time.Time
	EndTime         time.Time
	MeterNo         string
	MeterCipher     []byte
	MeterProtoVer   uint16
	EncryptMode     byte
	UnitPrice       [4]int64 // 单价，raw 值 ×0.00001 元
	Energy          [4]int64 // 分段电量，raw 值 ×0.0001 kWh
	LossEnergy      [4]int64 // 分段计损电量
	Amount          [4]int64 // 分段金额，raw 值 ×0.0001 元
	MeterStartKwh   float64
	MeterEndKwh     float64
	TotalEnergy     int64
	TotalLossEnergy int64
	TotalAmount     int64
	Vin             string
	TradeFlag       byte
	TradeTime       time.Time
	StopReason      byte
	PhysicalCard    uint64
}

// EncodeTradeRecord 编码交易记录消息体。
func EncodeTradeRecord(v TradeRecordInfo) ([]byte, string) {
	w := NewW(201)
	w.BCD(v.TradeNo, 16)
	w.BCD(v.DeviceNo, 7)
	w.U8(v.Gun)
	w.Time7(v.StartTime)
	w.Time7(v.EndTime)
	if v.WithMeter {
		// 电表表号 6B BCD 低位在前
		w.BCD(reverseDigits(v.MeterNo, 12), 6)
		cipher := make([]byte, 34)
		copy(cipher, v.MeterCipher)
		w.Raw(cipher)
		w.U16LE(v.MeterProtoVer)
		w.U8(v.EncryptMode)
	}
	for i := 0; i < 4; i++ {
		w.U32LE(uint32(v.UnitPrice[i]))
		w.U32LE(uint32(v.Energy[i]))
		w.U32LE(uint32(v.LossEnergy[i]))
		w.U32LE(uint32(v.Amount[i]))
	}
	w.U40LE(uint64(math.Round(v.MeterStartKwh * Scale4)))
	w.U40LE(uint64(math.Round(v.MeterEndKwh * Scale4)))
	w.U32LE(uint32(v.TotalEnergy))
	w.U32LE(uint32(v.TotalLossEnergy))
	w.U32LE(uint32(v.TotalAmount))
	w.ASCII(v.Vin, 17)
	w.U8(v.TradeFlag)
	w.Time7(v.TradeTime)
	w.U8(v.StopReason)
	w.U64LE(v.PhysicalCard)

	desc := fmt.Sprintf(
		"交易流水号=%s 桩号=%s 枪号=%02d 起止时间=%s~%s 尖=%.4fkWh/%.4f元 峰=%.4fkWh/%.4f元 平=%.4fkWh/%.4f元 谷=%.4fkWh/%.4f元 电表起止值=%.4f~%.4fkWh 总电量=%.4fkWh 消费金额=%.4f元 VIN=%s 交易标识=%d[%s] 交易时间=%s 停止原因=0x%02X[%s]",
		v.TradeNo, v.DeviceNo, v.Gun,
		v.StartTime.Format("2006-01-02 15:04:05"), v.EndTime.Format("2006-01-02 15:04:05"),
		float64(v.Energy[0])/Scale4, float64(v.Amount[0])/Scale4,
		float64(v.Energy[1])/Scale4, float64(v.Amount[1])/Scale4,
		float64(v.Energy[2])/Scale4, float64(v.Amount[2])/Scale4,
		float64(v.Energy[3])/Scale4, float64(v.Amount[3])/Scale4,
		v.MeterStartKwh, v.MeterEndKwh,
		float64(v.TotalEnergy)/Scale4, float64(v.TotalAmount)/Scale4,
		strings.TrimRight(v.Vin, "\x00"), v.TradeFlag, tradeFlagDesc(v.TradeFlag),
		v.TradeTime.Format("2006-01-02 15:04:05"), v.StopReason, stopReasonDesc(v.StopReason))
	return w.Bytes(), desc
}

// ---------------- 0x57 / 0x91 通用应答 ----------------

// EncodeSimpleAck 编码「桩号 + 结果」结构的应答帧，适用于 0x57 计费模型设置应答、0x91 远程重启应答。
func EncodeSimpleAck(deviceNo string, result byte) ([]byte, string) {
	w := NewW(8)
	w.BCD(deviceNo, 7)
	w.U8(result)
	return w.Bytes(), fmt.Sprintf("桩号=%s 设置结果=%d[%s]", deviceNo, result, boolDesc(result))
}

// TradeNo 生成交易流水号：桩号(7B) + 枪号(1B) + 年月日时分秒(6B) + 自增序号(2B)，共 32 位 BCD。
func TradeNo(deviceNo string, gun byte, t time.Time, seq uint16) string {
	sn := fmt.Sprintf("%04d", seq%10000)
	return deviceNo + fmt.Sprintf("%02d", gun) +
		fmt.Sprintf("%02d%02d%02d%02d%02d%02d", t.Year()%100, int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second()) + sn
}

func reverseDigits(s string, width int) string {
	s = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	for len(s) < width {
		s = "0" + s
	}
	if len(s) > width {
		s = s[len(s)-width:]
	}
	var sb strings.Builder
	for i := len(s) - 2; i >= 0; i -= 2 {
		sb.WriteString(s[i : i+2])
	}
	return sb.String()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
