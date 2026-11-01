package ykc

import "strings"

// VersionCode 协议版本标识，与服务端版本标签一致
type VersionCode string

const (
	V15 VersionCode = "V15"
	V16 VersionCode = "V16"
	V17 VersionCode = "V17"
	V18 VersionCode = "V18"
	V21 VersionCode = "V21"
)

// Spec 协议版本能力模型：一组帧结构参数 + 一组帧类型选择
type Spec struct {
	Code           VersionCode // 版本标识
	Desc           string      // 中文描述
	DefaultPort    int         // 服务端默认端口
	LengthFieldLen int         // 数据长度域字节数：V1.x=1，V2.1=2
	SendTimeField  bool        // 是否携带 7 字节 CP56Time2a 发送时间域
	TradeCmd       byte        // 交易记录帧类型：0x3B / 0x3D
	ApplyCmd       byte        // 申请启动充电：0x31 / 0xA5
	ApplyAckCmd    byte        // 确认启动充电：0x32 / 0xA6
	RemoteStartAck byte        // 远程启机回复：0x33 / 0xA7
	LoginKind      int         // 登录帧形态：0=V1.5~V1.7，1=V1.8，2=V2.1
	VariableRate   bool        // 计费模型费率段数是否由报文动态决定（V2.1）
	RealTimeExtra  bool        // 实时数据是否追加桩体温度/烟感/电表示值（V2.1）
	RateSegment    int         // 计费模型默认费率段数
}

var (
	specV15 = Spec{
		Code: V15, Desc: "云快充1.5", DefaultPort: 9847,
		LengthFieldLen: 1, SendTimeField: false,
		TradeCmd: CmdTradeUp, ApplyCmd: CmdApplyCharge, ApplyAckCmd: CmdApplyChargeAck,
		RemoteStartAck: CmdRemoteChargeAck, LoginKind: 0, VariableRate: false,
		RealTimeExtra: false, RateSegment: 4,
	}
	specV16 = Spec{
		Code: V16, Desc: "云快充1.6", DefaultPort: 9848,
		LengthFieldLen: 1, SendTimeField: false,
		TradeCmd: CmdTradeUp, ApplyCmd: CmdApplyCharge, ApplyAckCmd: CmdApplyChargeAck,
		RemoteStartAck: CmdRemoteChargeAck, LoginKind: 0, VariableRate: false,
		RealTimeExtra: false, RateSegment: 4,
	}
	specV17 = Spec{
		Code: V17, Desc: "云快充1.7", DefaultPort: 9849,
		LengthFieldLen: 1, SendTimeField: false,
		TradeCmd: CmdTradeUp2, ApplyCmd: CmdApplyCharge, ApplyAckCmd: CmdApplyChargeAck,
		RemoteStartAck: CmdRemoteChargeAck, LoginKind: 0, VariableRate: false,
		RealTimeExtra: false, RateSegment: 4,
	}
	specV18 = Spec{
		Code: V18, Desc: "云快充1.8", DefaultPort: 9850,
		LengthFieldLen: 1, SendTimeField: false,
		TradeCmd: CmdTradeUp2, ApplyCmd: CmdApplyCharge2, ApplyAckCmd: CmdApplyCharge2Ack,
		RemoteStartAck: CmdRemoteCharge2Ack, LoginKind: 1, VariableRate: false,
		RealTimeExtra: false, RateSegment: 4,
	}
	specV21 = Spec{
		Code: V21, Desc: "云快充2.1", DefaultPort: 9851,
		LengthFieldLen: 2, SendTimeField: true,
		TradeCmd: CmdTradeUp2, ApplyCmd: CmdApplyCharge2, ApplyAckCmd: CmdApplyCharge2Ack,
		RemoteStartAck: CmdRemoteCharge2Ack, LoginKind: 2, VariableRate: true,
		RealTimeExtra: true, RateSegment: 4,
	}
)

// AllSpecs 全部协议版本
func AllSpecs() []*Spec {
	return []*Spec{&specV15, &specV16, &specV17, &specV18, &specV21}
}

// GetSpec 按版本标识获取协议能力模型，未知版本返回 V1.5
func GetSpec(code VersionCode) *Spec {
	switch VersionCode(strings.ToUpper(string(code))) {
	case V15:
		return &specV15
	case V16:
		return &specV16
	case V17:
		return &specV17
	case V18:
		return &specV18
	case V21:
		return &specV21
	default:
		return &specV15
	}
}

// SpecByPort 按端口推断协议版本（9847~9851）
func SpecByPort(port int) *Spec {
	for _, s := range AllSpecs() {
		if s.DefaultPort == port {
			return s
		}
	}
	return &specV15
}

// ProtocolVersionBytes 登录帧中的通信协议版本字段。
//   - V1.5 ~ V1.7：1 字节，版本号 ×10（V1.5 → 0x0F）
//   - V1.8 / V2.1：3 字节 BCD（V1.8.1 → 01 08 01，V2.1.0 → 02 01 00）
func (v *Spec) ProtocolVersionBytes() []byte {
	switch v.Code {
	case V15:
		return []byte{0x0F}
	case V16:
		return []byte{0x10}
	case V17:
		return []byte{0x11}
	case V18:
		return []byte{0x01, 0x08, 0x01}
	case V21:
		return []byte{0x02, 0x01, 0x00}
	default:
		return []byte{0x0F}
	}
}

// UsesExtLogin 登录帧是否携带 88 字节随机密钥（V1.8+）
func (v *Spec) UsesExtLogin() bool { return v.LoginKind >= 1 }
