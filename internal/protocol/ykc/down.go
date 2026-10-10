package ykc

import (
	"fmt"
	"strings"
	"time"
)

// 本文件实现下行帧（⬇️ server -> 模拟器）的消息体解析。

// BillingPayload 计费模型（0x0A / 0x58）解析结果，费率保留协议 raw 值（×0.00001）。
type BillingPayload struct {
	DeviceNo  string
	ModelNo   string // 4 位数字串
	Ep        [4]int64
	Sp        [4]int64
	LossRatio byte
	Slots     [48]byte
}

// SectionNames 费率段名称，下标即段索引：0 尖 / 1 峰 / 2 平 / 3 谷。
var SectionNames = [4]string{"尖", "峰", "平", "谷"}

// DecodeLoginAck 解析登录认证应答（0x02，8 字节）。
func DecodeLoginAck(body []byte) (deviceNo string, result byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.BCD(7)
	result = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%s 登录结果=%d[%s]", deviceNo, result, loginResultDesc(result))
	return
}

// DecodeHeartbeatAck 解析心跳包应答（0x04，9 字节）。
func DecodeHeartbeatAck(body []byte) (deviceNo string, gun byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.BCD(7)
	gun = r.U8()
	ack := r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%s 枪号=%02d 心跳应答=%d", deviceNo, gun, ack)
	return
}

// DecodeBillingVerifyAck 解析计费模型验证请求应答（0x06，10 字节）。
func DecodeBillingVerifyAck(body []byte) (deviceNo, modelNo string, result byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.BCD(7)
	modelNo = r.BCD(2)
	result = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%s 计费模型编号=%s 验证结果=%d[%s]", deviceNo, modelNo, result, verifyResultDesc(result))
	return
}

// DecodeBillingModelAck 解析计费模型请求应答（0x0A / 0x58，90 字节）。
func DecodeBillingModelAck(body []byte) (*BillingPayload, string, error) {
	r := NewR(body)
	p := &BillingPayload{}
	p.DeviceNo = r.BCD(7)
	p.ModelNo = r.BCD(2)
	for i := 0; i < 4; i++ {
		p.Ep[i] = int64(r.U32LE())
		p.Sp[i] = int64(r.U32LE())
	}
	p.LossRatio = r.U8()
	copy(p.Slots[:], r.Raw(48))
	if err := r.Err(); err != nil {
		return nil, "", err
	}

	var sb strings.Builder
	for i := 0; i < 4; i++ {
		sb.WriteString(fmt.Sprintf("%s电费=%.5f元 服务费=%.5f元 ", SectionNames[i],
			float64(p.Ep[i])/Scale5, float64(p.Sp[i])/Scale5))
	}
	sb.WriteString(fmt.Sprintf("计损比例=%d%% 时段费率号=", p.LossRatio))
	for i, s := range p.Slots {
		if i > 0 {
			sb.WriteString(",")
		}
		if int(s) < len(SectionNames) {
			sb.WriteString(SectionNames[s])
		} else {
			sb.WriteString("?")
		}
	}
	return p, fmt.Sprintf("桩号=%s 计费模型编号=%s %s", p.DeviceNo, p.ModelNo, sb.String()), nil
}

// DecodeTimeSync 解析对时设置（0x56，14 字节）。
func DecodeTimeSync(body []byte) (deviceNo string, t time.Time, desc string, err error) {
	r := NewR(body)
	deviceNo = r.BCD(7)
	t = r.Time7()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%s 当前时间=%s", deviceNo, t.Format("2006-01-02 15:04:05.000"))
	return
}

// RemoteStartInfo 远程控制启机（0x34）解析结果。
type RemoteStartInfo struct {
	TradeNo      string
	DeviceNo     string
	Gun          byte
	LogicCard    string
	PhysicalCard uint64
	BalanceYuan  float64
}

// DecodeRemoteStart 解析运营平台远程控制启机（0x34，44 字节）。
func DecodeRemoteStart(body []byte) (*RemoteStartInfo, string, error) {
	r := NewR(body)
	v := &RemoteStartInfo{}
	v.TradeNo = r.BCD(16)
	v.DeviceNo = r.BCD(7)
	v.Gun = r.U8()
	v.LogicCard = r.BCD(8)
	v.PhysicalCard = r.U64LE()
	// 账户余额 4B 保留 2 位小数，按小端解读；个别版本/样例为大端，结果异常时回退。
	bb := r.Raw(4)
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	le := uint32(bb[0]) | uint32(bb[1])<<8 | uint32(bb[2])<<16 | uint32(bb[3])<<24
	be := uint32(bb[0])<<24 | uint32(bb[1])<<16 | uint32(bb[2])<<8 | uint32(bb[3])
	v.BalanceYuan = float64(le) / 100
	if v.BalanceYuan > 1e7 {
		v.BalanceYuan = float64(be) / 100
	}
	desc := fmt.Sprintf("交易流水号=%s 桩号=%s 枪号=%02d 逻辑卡号=%s 物理卡号=%016X 账户余额=%.2f元",
		v.TradeNo, v.DeviceNo, v.Gun, v.LogicCard, v.PhysicalCard, v.BalanceYuan)
	return v, desc, nil
}

// DecodeGunCommand 解析「桩号 + 枪号」结构的下行帧，适用于 0x12 读取实时监测数据、0x36 远程停机。
func DecodeGunCommand(body []byte) (deviceNo string, gun byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.BCD(7)
	gun = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%s 枪号=%02d", deviceNo, gun)
	return
}

// DecodeTradeAck 解析交易记录确认（0x40，17 字节）。
func DecodeTradeAck(body []byte) (tradeNo string, result byte, desc string, err error) {
	r := NewR(body)
	tradeNo = r.BCD(16)
	result = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("交易流水号=%s 确认结果=%d[%s]", tradeNo, result, tradeAckDesc(result))
	return
}

// DecodeRemoteRestart 解析远程重启（0x92，8 字节）。
func DecodeRemoteRestart(body []byte) (deviceNo string, ctrl byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.BCD(7)
	ctrl = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%s 执行控制=%d[%s]", deviceNo, ctrl, restartCtrlDesc(ctrl))
	return
}

// ---------------- 枚举描述 ----------------

func boolDesc(v byte) string {
	if v == 0x01 {
		return "成功"
	}
	return "失败"
}

func loginResultDesc(v byte) string {
	if v == 0x00 {
		return "登录成功"
	}
	return "登录失败"
}

func verifyResultDesc(v byte) string {
	if v == 0x00 {
		return "一致"
	}
	return "不一致"
}

func tradeAckDesc(v byte) string {
	if v == 0x00 {
		return "上传成功"
	}
	return "非法账单"
}

func restartCtrlDesc(v byte) string {
	switch v {
	case 0x01:
		return "立即执行"
	case 0x02:
		return "空闲执行"
	}
	return "-"
}

func gunStatusDesc(v byte) string {
	switch v {
	case 0x00:
		return "正常"
	case 0x01:
		return "故障"
	}
	return "-"
}

func gunWorkStatusDesc(v byte) string {
	switch v {
	case 0x00:
		return "离线"
	case 0x01:
		return "故障"
	case 0x02:
		return "空闲"
	case 0x03:
		return "充电"
	}
	return "-"
}

func pileTypeDesc(v byte) string {
	switch v {
	case 0x00:
		return "直流桩"
	case 0x01:
		return "交流桩"
	}
	return "-"
}

func netTypeDesc(v byte) string {
	switch v {
	case 0x00:
		return "SIM卡"
	case 0x01:
		return "LAN"
	case 0x02:
		return "WAN"
	case 0x03:
		return "其他"
	}
	return "-"
}

func supplierDesc(v byte) string {
	switch v {
	case 0x00:
		return "移动"
	case 0x02:
		return "电信"
	case 0x03:
		return "联通"
	case 0x04:
		return "其他"
	}
	return "-"
}

func startErrDesc(v byte) string {
	switch v {
	case 0x00:
		return "无"
	case 0x01:
		return "设备编号不匹配"
	case 0x02:
		return "枪已在充电"
	case 0x03:
		return "设备故障"
	case 0x04:
		return "设备离线"
	case 0x05:
		return "未插枪"
	}
	return "-"
}

func stopErrDesc(v byte) string {
	switch v {
	case 0x00:
		return "无"
	case 0x01:
		return "设备编号不匹配"
	case 0x02:
		return "枪未处于充电状态"
	case 0x03:
		return "其他"
	}
	return "-"
}

func batteryTypeDesc(v byte) string {
	switch v {
	case 0x01:
		return "铅酸"
	case 0x02:
		return "氢"
	case 0x03:
		return "磷酸铁锂"
	case 0x04:
		return "锰酸锂"
	case 0x05:
		return "钴酸锂"
	case 0x06:
		return "三元材料"
	case 0x07:
		return "聚合物锂离子"
	case 0x08:
		return "钛酸锂"
	case 0xFF:
		return "其他"
	}
	return "-"
}

func ownershipDesc(v byte) string {
	if v == 0 {
		return "租赁"
	}
	return "车自有"
}

func chargeModeDesc(v byte) string {
	switch v {
	case 0x01:
		return "恒压充电"
	case 0x02:
		return "恒流充电"
	}
	return "-"
}

func tradeFlagDesc(v byte) string {
	switch v {
	case 0x01:
		return "app启动"
	case 0x02:
		return "卡启动"
	case 0x04:
		return "离线卡启动"
	case 0x05:
		return "vin码启动"
	}
	return "-"
}

func stopReasonDesc(v byte) string {
	switch v {
	case 0x40:
		return "APP远程停止"
	case 0x41:
		return "SOC达到100%"
	case 0x42:
		return "充电电量满足设定条件"
	case 0x43:
		return "充电金额满足设定条件"
	case 0x44:
		return "充电时间满足设定条件"
	case 0x45:
		return "手动停止充电"
	case 0x90:
		return "未知原因停止"
	}
	if v >= 0x4A && v <= 0x69 {
		return "充电启动失败"
	}
	if v >= 0x6A && v <= 0x8F {
		return "充电异常中止"
	}
	return "-"
}

// hwFaultDesc 解析 0x13 硬件故障位域。
func hwFaultDesc(v uint16) string {
	names := []string{
		"急停按钮动作故障", "无可用整流模块", "出风口温度过高", "交流防雷故障",
		"交直流模块DC20通信中断", "绝缘检测模块FC08通信中断", "电度表通信中断", "读卡器通信中断",
		"RC10通信中断", "风扇调速板故障", "直流熔断器故障", "高压接触器故障", "门打开",
	}
	if v == 0 {
		return "无"
	}
	var sb strings.Builder
	for i := 0; i < len(names); i++ {
		if v&(1<<uint(i)) != 0 {
			if sb.Len() > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(names[i])
		}
	}
	if sb.Len() == 0 {
		return "未知故障位"
	}
	return sb.String()
}

// bmsStatus1Desc 解析 0x25 状态字节 1。
func bmsStatus1Desc(v byte) string {
	pairs := []string{
		"单体电压:正常", "单体电压:过高", "单体电压:过低",
		"SOC:正常", "SOC:过高", "SOC:过低",
		"充电过流:正常", "充电过流:过流", "充电过流:不可信",
		"温度:正常", "温度:过高", "温度:不可信",
	}
	return bits2Desc(v, pairs)
}

// bmsStatus2Desc 解析 0x25 状态字节 2。
func bmsStatus2Desc(v byte) string {
	pairs := []string{
		"绝缘:正常", "绝缘:故障", "绝缘:不可信",
		"连接器:正常", "连接器:故障", "连接器:不可信",
		"充电:禁止", "充电:允许",
	}
	return bits2Desc(v, pairs)
}

func bits2Desc(v byte, groups []string) string {
	var sb strings.Builder
	for i := 0; i < 4 && i*2 < len(groups); i++ {
		val := (v >> (2 * uint(i))) & 0x03
		idx := i*3 + int(val)
		if idx < len(groups) {
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(groups[idx])
		}
	}
	if sb.Len() == 0 {
		return "-"
	}
	return sb.String()
}
