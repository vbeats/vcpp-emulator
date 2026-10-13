package ykc

import (
	"fmt"
	"strings"
	"time"
)

// 本文件实现下行帧（⬇️ server -> 模拟器）的消息体解析。

// MaxSections 计费模型最多支持的费率段数：尖/峰/平/谷/深谷。
const MaxSections = 5

// SectionNames 费率段名称，下标即段索引：0 尖 / 1 峰 / 2 平 / 3 谷 / 4 深谷。
var SectionNames = [MaxSections]string{"尖", "峰", "平", "谷", "深谷"}

// 计费模型帧的消息体长度：4 段（尖峰平谷）90 字节，5 段（尖峰平谷深谷）98 字节。
const (
	BillingBodyLen4 = 90
	BillingBodyLen5 = 98
)

// BillingPayload 计费模型（0x0A / 0x58）解析结果，费率保留协议 raw 值（×0.00001）。
// Sections 为 4 时是尖峰平谷模式，5 时是尖峰平谷深谷模式（深谷排在谷之后）。
type BillingPayload struct {
	DeviceNo  uint64
	ModelNo   string // 4 位数字串
	Sections  int
	Ep        [MaxSections]int64
	Sp        [MaxSections]int64
	LossRatio byte
	Slots     [48]byte
}

// DecodeLoginAck 解析登录认证应答（0x02，8 字节）。
func DecodeLoginAck(body []byte) (deviceNo uint64, result byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	result = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 登录结果=%d[%s]", deviceNo, result, loginResultDesc(result))
	return
}

// DecodeHeartbeatAck 解析心跳包应答（0x04，9 字节）。
func DecodeHeartbeatAck(body []byte) (deviceNo uint64, gun byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	gun = r.U8()
	ack := r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 枪号=%02d 心跳应答=%d", deviceNo, gun, ack)
	return
}

// DecodeBillingVerifyAck 解析计费模型验证请求应答（0x06，10 字节）。
func DecodeBillingVerifyAck(body []byte) (deviceNo uint64, modelNo string, result byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	modelNo = r.BCD(2)
	result = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 计费模型编号=%s 验证结果=%d[%s]", deviceNo, modelNo, result, verifyResultDesc(result))
	return
}

// DecodeBillingModelAck 解析计费模型请求应答 / 计费模型设置（0x0A / 0x58）。
// 消息体 90 字节为尖峰平谷（4 段），98 字节为尖峰平谷深谷（5 段，深谷排在谷之后）。
// 协议没有「模式/段数」字段，模式只能按消息体长度区分。
func DecodeBillingModelAck(body []byte) (*BillingPayload, string, error) {
	r := NewR(body)
	p := &BillingPayload{}
	p.DeviceNo = r.DeviceNo()
	p.ModelNo = r.BCD(2)
	p.Sections = 4
	if len(body) >= BillingBodyLen5 {
		p.Sections = 5
	}
	for i := 0; i < p.Sections; i++ {
		p.Ep[i] = int64(r.U32LE())
		p.Sp[i] = int64(r.U32LE())
	}
	p.LossRatio = r.U8()
	copy(p.Slots[:], r.Raw(48))
	if err := r.Err(); err != nil {
		return nil, "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("模式=%s ", sectionModeDesc(p.Sections)))
	for i := 0; i < p.Sections; i++ {
		sb.WriteString(fmt.Sprintf("%s电费=%.5f元 服务费=%.5f元 ", SectionNames[i],
			float64(p.Ep[i])/Scale5, float64(p.Sp[i])/Scale5))
	}
	sb.WriteString(fmt.Sprintf("计损比例=%d%% 时段费率号=", p.LossRatio))
	for i, s := range p.Slots {
		if i > 0 {
			sb.WriteString(",")
		}
		if int(s) < p.Sections {
			sb.WriteString(SectionNames[s])
		} else {
			sb.WriteString("?")
		}
	}
	return p, fmt.Sprintf("桩号=%014d 计费模型编号=%s %s", p.DeviceNo, p.ModelNo, sb.String()), nil
}

// DecodeTimeSync 解析对时设置（0x56，14 字节）。
func DecodeTimeSync(body []byte) (deviceNo uint64, t time.Time, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	t = r.Time7()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 当前时间=%s", deviceNo, t.Format("2006-01-02 15:04:05.000"))
	return
}

// RemoteStartInfo 远程控制启机（0x34）解析结果。
type RemoteStartInfo struct {
	TradeNo      string
	DeviceNo     uint64
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
	v.DeviceNo = r.DeviceNo()
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
	desc := fmt.Sprintf("交易流水号=%s 桩号=%014d 枪号=%02d 逻辑卡号=%s 物理卡号=%016X 账户余额=%.2f元",
		v.TradeNo, v.DeviceNo, v.Gun, v.LogicCard, v.PhysicalCard, v.BalanceYuan)
	return v, desc, nil
}

// ParallelStartInfo 运营平台远程控制并充启机（0xA4）解析结果。
type ParallelStartInfo struct {
	RemoteStartInfo
	ParallelNo string // 6B BCD：年月日时分秒
}

// DecodeParallelStart 解析运营平台远程控制并充启机（0xA4，50 字节）。
func DecodeParallelStart(body []byte) (*ParallelStartInfo, string, error) {
	v, desc, err := DecodeRemoteStart(body[:44])
	if err != nil {
		return nil, "", err
	}
	r := NewR(body[44:])
	p := &ParallelStartInfo{RemoteStartInfo: *v}
	p.ParallelNo = r.BCD(6)
	if err = r.Err(); err != nil {
		return nil, "", err
	}
	return p, desc + fmt.Sprintf(" 并充序号=%s", p.ParallelNo), nil
}

// DecodeGunCommand 解析「桩号 + 枪号」结构的下行帧，适用于 0x12 读取实时监测数据、0x36 远程停机。
func DecodeGunCommand(body []byte) (deviceNo uint64, gun byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	gun = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 枪号=%02d", deviceNo, gun)
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
func DecodeRemoteRestart(body []byte) (deviceNo uint64, ctrl byte, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	ctrl = r.U8()
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 执行控制=%d[%s]", deviceNo, ctrl, restartCtrlDesc(ctrl))
	return
}

// ---------------- 0x32 运营平台确认启动充电 ----------------

// ChargeApplyAckInfo 运营平台确认启动充电（0x32）解析结果。
type ChargeApplyAckInfo struct {
	TradeNo     string
	DeviceNo    uint64
	Gun         byte
	LogicCard   string
	BalanceYuan float64
	AuthOK      byte
	FailReason  byte
}

// DecodeChargeApplyAck 解析运营平台确认启动充电（0x32，38 字节）。
// 该帧是刷卡/VIN 启动路径下交易流水号的来源，后续实时数据与交易记录必须沿用同一流水号。
func DecodeChargeApplyAck(body []byte) (*ChargeApplyAckInfo, string, error) {
	r := NewR(body)
	v := &ChargeApplyAckInfo{}
	v.TradeNo = r.BCD(16)
	v.DeviceNo = r.DeviceNo()
	v.Gun = r.U8()
	v.LogicCard = r.BCD(8)
	v.BalanceYuan = float64(r.U32LE()) / 100
	v.AuthOK = r.U8()
	v.FailReason = r.U8()
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("交易流水号=%s 桩号=%014d 枪号=%02d 逻辑卡号=%s 账户余额=%.2f元 鉴权=%d[%s] 失败原因=%02X[%s]",
		v.TradeNo, v.DeviceNo, v.Gun, v.LogicCard, v.BalanceYuan, v.AuthOK, boolDesc(v.AuthOK),
		v.FailReason, authErrDesc(v.FailReason))
	return v, desc, nil
}

// ---------------- 0x42 远程账户余额更新 ----------------

// BalanceUpdateInfo 远程账户余额更新（0x42）解析结果。
type BalanceUpdateInfo struct {
	DeviceNo     uint64
	Gun          byte
	PhysicalCard uint64
	BalanceYuan  float64
}

// DecodeBalanceUpdate 解析远程账户余额更新（0x42，20 字节）。
func DecodeBalanceUpdate(body []byte) (*BalanceUpdateInfo, string, error) {
	r := NewR(body)
	v := &BalanceUpdateInfo{}
	v.DeviceNo = r.DeviceNo()
	v.Gun = r.U8()
	v.PhysicalCard = r.U64LE()
	v.BalanceYuan = float64(r.U32LE()) / 100
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("桩号=%014d 枪号=%02d 物理卡号=%016X 修改后账户金额=%.2f元",
		v.DeviceNo, v.Gun, v.PhysicalCard, v.BalanceYuan)
	return v, desc, nil
}

// ---------------- 0x44 / 0x46 / 0x48 离线卡 ----------------

// OfflineCard 一张离线卡：逻辑卡号 8B BCD + 物理卡号 8B。
type OfflineCard struct {
	LogicCard    string
	PhysicalCard uint64
}

// DecodeCardSync 解析离线卡数据同步（0x44，8 + 16N 字节，最多 15 张）。
func DecodeCardSync(body []byte) (deviceNo uint64, cards []OfflineCard, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	n := int(r.U8())
	for i := 0; i < n; i++ {
		c := OfflineCard{LogicCard: r.BCD(8), PhysicalCard: r.U64LE()}
		cards = append(cards, c)
	}
	if err = r.Err(); err != nil {
		return
	}
	desc = fmt.Sprintf("桩号=%014d 下发卡个数=%d%s", deviceNo, n, cardsDesc(cards, false))
	return
}

// DecodeCardClear 解析离线卡数据清除（0x46，8 + 8N 字节，最多 24 张）。
func DecodeCardClear(body []byte) (deviceNo uint64, physicals []uint64, desc string, err error) {
	r := NewR(body)
	deviceNo = r.DeviceNo()
	n := int(r.U8())
	for i := 0; i < n; i++ {
		physicals = append(physicals, r.U64LE())
	}
	if err = r.Err(); err != nil {
		return
	}
	var sb strings.Builder
	for i, p := range physicals {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%016X", p))
	}
	desc = fmt.Sprintf("桩号=%014d 清除卡个数=%d 物理卡号=[%s]", deviceNo, n, sb.String())
	return
}

// DecodeCardQuery 解析离线卡数据查询（0x48，8 + 8N 字节，最多 26 张）。
func DecodeCardQuery(body []byte) (deviceNo uint64, physicals []uint64, desc string, err error) {
	return DecodeCardClear(body)
}

// ---------------- 0x52 充电桩工作参数设置 ----------------

// WorkParamInfo 充电桩工作参数设置（0x52）解析结果。
type WorkParamInfo struct {
	DeviceNo    uint64
	Enable      byte // 0x00 允许正常 / 0x01 停止使用并锁定
	MaxPowerPct byte // 1BIN = 1%，范围 30~100
}

// DecodeWorkParam 解析充电桩工作参数设置（0x52，9 字节）。
func DecodeWorkParam(body []byte) (*WorkParamInfo, string, error) {
	r := NewR(body)
	v := &WorkParamInfo{}
	v.DeviceNo = r.DeviceNo()
	v.Enable = r.U8()
	v.MaxPowerPct = r.U8()
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("桩号=%014d 是否允许工作=%d[%s] 最大允许输出功率=%d%%",
		v.DeviceNo, v.Enable, workEnableDesc(v.Enable), v.MaxPowerPct)
	return v, desc, nil
}

// ---------------- 0x62 遥控地锁 ----------------

// LockCmdInfo 遥控地锁（0x62）解析结果。
type LockCmdInfo struct {
	DeviceNo uint64
	Gun      byte
	Action   byte // 0x55 升锁 / 0xFF 降锁
}

// DecodeLockCmd 解析遥控地锁升锁与降锁（0x62，13 字节）。
func DecodeLockCmd(body []byte) (*LockCmdInfo, string, error) {
	r := NewR(body)
	v := &LockCmdInfo{}
	v.DeviceNo = r.DeviceNo()
	v.Gun = r.U8()
	v.Action = r.U8()
	r.Raw(4)
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("桩号=%014d 枪号=%02d 升/降地锁=%02X[%s]", v.DeviceNo, v.Gun, v.Action, lockActionDesc(v.Action))
	return v, desc, nil
}

// ---------------- 0x94 远程更新 ----------------

// RemoteUpdateInfo 远程更新（0x94）解析结果。
type RemoteUpdateInfo struct {
	DeviceNo   uint64
	PileModel  byte // 0x01 直流 / 0x02 交流
	PowerKw    uint16
	ServerAddr string
	ServerPort uint16
	User       string
	Password   string
	FilePath   string
	Ctrl       byte // 0x01 立即执行 / 0x02 空闲执行
	TimeoutMin byte
}

// DecodeRemoteUpdate 解析远程更新（0x94，94 字节）。
func DecodeRemoteUpdate(body []byte) (*RemoteUpdateInfo, string, error) {
	r := NewR(body)
	v := &RemoteUpdateInfo{}
	v.DeviceNo = r.DeviceNo()
	v.PileModel = r.U8()
	v.PowerKw = r.U16LE()
	v.ServerAddr = r.ASCII(16)
	v.ServerPort = r.U16BE()
	v.User = r.ASCII(16)
	v.Password = r.ASCII(16)
	v.FilePath = r.ASCII(32)
	v.Ctrl = r.U8()
	v.TimeoutMin = r.U8()
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("桩号=%014d 桩型号=%d[%s] 桩功率=%dkW 升级服务器=%s:%d 用户名=%s 文件路径=%s 执行控制=%d[%s] 下载超时=%dmin",
		v.DeviceNo, v.PileModel, pileTypeDesc(v.PileModel-1), v.PowerKw, v.ServerAddr, v.ServerPort,
		v.User, v.FilePath, v.Ctrl, restartCtrlDesc(v.Ctrl), v.TimeoutMin)
	return v, desc, nil
}

// ---------------- 二维码下发 ----------------

// QrPrefixInfo 二维码前缀下发（0xF0，V1.5 补充）解析结果。
type QrPrefixInfo struct {
	DeviceNo uint64
	Format   byte // 0x00 前缀+14位桩编号 / 0x01 前缀+桩编号+2位枪号
	Prefix   string
}

// DecodeQrPrefix 解析二维码前缀下发（0xF0，9 + N 字节）。
func DecodeQrPrefix(body []byte) (*QrPrefixInfo, string, error) {
	r := NewR(body)
	v := &QrPrefixInfo{}
	v.DeviceNo = r.DeviceNo()
	v.Format = r.U8()
	n := int(r.U8())
	v.Prefix = r.ASCII(n)
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("桩号=%014d 二维码格式=%d[%s] 前缀长度=%d 前缀=%s",
		v.DeviceNo, v.Format, qrFormatDesc(v.Format), n, v.Prefix)
	return v, desc, nil
}

// QrUrlInfo 平台下发二维码（0x9C / 0x9E，V1.6 补充）解析结果。
type QrUrlInfo struct {
	DeviceNo uint64 // 0x9C 无桩号，置 0
	HasNo    bool
	Gun      byte
	URL      string
}

// DecodeQrUrl 解析平台下发二维码：0x9C（无桩号）/ 0x9E（含桩号）。
func DecodeQrUrl(body []byte, hasDeviceNo bool) (*QrUrlInfo, string, error) {
	r := NewR(body)
	v := &QrUrlInfo{HasNo: hasDeviceNo}
	if hasDeviceNo {
		v.DeviceNo = r.DeviceNo()
	}
	v.Gun = r.U8()
	n := int(r.U16BE())
	v.URL = r.ASCII(n)
	if err := r.Err(); err != nil {
		return nil, "", err
	}
	desc := fmt.Sprintf("桩号=%014d 枪号=%02d 二维码长度=%d 二维码=%s", v.DeviceNo, v.Gun, n, v.URL)
	return v, desc, nil
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

// startTypeDesc 解析 0x31 启动方式。
func startTypeDesc(v byte) string {
	switch v {
	case 0x01:
		return "刷卡启动"
	case 0x02:
		return "账号启动"
	case 0x03:
		return "VIN码启动"
	}
	return "-"
}

// roleDesc 解析并充主辅枪标记。
func roleDesc(v byte) string {
	if v == 0x01 {
		return "辅枪"
	}
	return "主枪"
}

// updateStatusDesc 解析 0x93 升级状态。
func updateStatusDesc(v byte) string {
	switch v {
	case 0x00:
		return "成功"
	case 0x01:
		return "编号错误"
	case 0x02:
		return "程序与桩型号不符"
	case 0x03:
		return "下载更新文件超时"
	}
	return "-"
}

// balanceResultDesc 解析 0x41 修改结果。
func balanceResultDesc(v byte) string {
	switch v {
	case 0x00:
		return "成功"
	case 0x01:
		return "设备编号错误"
	case 0x02:
		return "卡号错误"
	}
	return "-"
}

// cardErrDesc 解析 0x43 失败原因。
func cardErrDesc(v byte) string {
	switch v {
	case 0x01:
		return "卡号格式错误"
	case 0x02:
		return "存储空间不足"
	}
	return "-"
}

// existDesc 解析 0x47 查询结果。
func existDesc(v byte) string {
	if v == 0x01 {
		return "存在"
	}
	return "不存在"
}

// lockStateDesc 解析地锁状态。
func lockStateDesc(v byte) string {
	switch v {
	case 0x00:
		return "未到位"
	case 0x55:
		return "升锁到位"
	case 0xFF:
		return "降锁到位"
	}
	return "-"
}

// parkStateDesc 解析车位状态。
func parkStateDesc(v byte) string {
	switch v {
	case 0x00:
		return "无车辆"
	case 0xFF:
		return "停放车辆"
	}
	return "-"
}

// lockAlarmDesc 解析地锁报警状态。
func lockAlarmDesc(v byte) string {
	switch v {
	case 0x00:
		return "正常"
	case 0x55:
		return "摇臂升降异常"
	case 0xFF:
		return "待机摇臂破坏"
	}
	return "-"
}

// lockActionDesc 解析 0x62 升/降地锁。
func lockActionDesc(v byte) string {
	switch v {
	case 0x55:
		return "升锁"
	case 0xFF:
		return "降锁"
	}
	return "-"
}

// workEnableDesc 解析 0x52 是否允许工作。
func workEnableDesc(v byte) string {
	if v == 0x01 {
		return "停止使用并锁定"
	}
	return "允许正常"
}

// authErrDesc 解析 0x32 鉴权失败原因。
func authErrDesc(v byte) string {
	switch v {
	case 0x01:
		return "账户不存在"
	case 0x02:
		return "账户冻结"
	case 0x03:
		return "账户余额不足"
	case 0x04:
		return "该卡有未结账记录"
	case 0x05:
		return "桩停用"
	case 0x06:
		return "该账户不能在此桩充电"
	case 0x07:
		return "密码错误"
	case 0x08:
		return "电站电容不足"
	case 0x09:
		return "VIN不存在"
	case 0x0A:
		return "该桩有未结账记录"
	case 0x0B:
		return "该桩不支持刷卡"
	}
	return "无"
}

// qrFormatDesc 解析 0xF0 二维码格式。
func qrFormatDesc(v byte) string {
	if v == 0x01 {
		return "前缀+桩编号+枪编号"
	}
	return "前缀+桩编号"
}

// sectionModeDesc 返回费率模式名称。
func sectionModeDesc(n int) string {
	if n == 5 {
		return "尖峰平谷深谷(5段)"
	}
	return "尖峰平谷(4段)"
}

// cardsDesc 拼接离线卡列表描述。
func cardsDesc(cards []OfflineCard, withResult bool) string {
	var sb strings.Builder
	for i, c := range cards {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("[逻辑卡号=%s 物理卡号=%016X", c.LogicCard, c.PhysicalCard))
		if withResult {
			sb.WriteString("]")
			continue
		}
		sb.WriteString("]")
	}
	if sb.Len() == 0 {
		return ""
	}
	return " 卡列表=" + sb.String()
}

// errorFlagsDesc 解析 0x1B 错误报文的 8 字节位域。
func errorFlagsDesc(flags [8]byte) string {
	names := []string{
		"辨识报文(SPN2560=0x00)", "辨识报文(SPN2560=0xAA)", "预留",
		"充电机时间同步与最大输出能力", "充电机完成充电准备", "预留",
		"充电机充电状态", "充电机中止充电", "预留",
		"充电机充电统计", "BMS其他",
	}
	var sb strings.Builder
	idx := 0
	for b := 0; b < 4; b++ {
		for g := 0; g < 4 && idx < len(names); g++ {
			val := (flags[b] >> (2 * uint(g))) & 0x03
			if val == 0 || strings.HasPrefix(names[idx], "预留") {
				idx++
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf("%s=%s", names[idx], bits2Desc(val)))
			idx++
		}
	}
	idx = 0
	for b := 4; b < 8; b++ {
		for g := 0; g < 4; g++ {
			val := (flags[b] >> (2 * uint(g))) & 0x03
			if val == 0 {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf("BMS组[%d:%d]=%s", b, g, bits2Desc(val)))
			_ = idx
		}
	}
	if sb.Len() == 0 {
		return "全部正常"
	}
	return sb.String()
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

func bits2Desc(v byte, groups ...[]string) string {
	if len(groups) > 0 {
		g := groups[0]
		var sb strings.Builder
		for i := 0; i < 4 && i*2 < len(g); i++ {
			val := (v >> (2 * uint(i))) & 0x03
			if i > 0 {
				sb.WriteString(" ")
			}
			if int(val) < 3 {
				sb.WriteString(g[i*2+int(val)])
			} else {
				sb.WriteString("-")
			}
		}
		return sb.String()
	}
	switch v {
	case 0x01:
		return "超时"
	case 0x02:
		return "不可信状态"
	case 0x00:
		return "正常"
	}
	return "-"
}
