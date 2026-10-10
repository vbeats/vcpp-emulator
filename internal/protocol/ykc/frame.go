package ykc

import (
	"fmt"
	"strconv"
	"strings"
)

// Direction 通信方向。
// 下行 ⬇️ = server 端(vcpp) → 模拟器；上行 ⬆️ = 模拟器 → server 端(vcpp)。
type Direction uint8

const (
	DirUp   Direction = iota // ⬆️ 上行：模拟器 -> server
	DirDown                  // ⬇️ 下行：server -> 模拟器
)

func (d Direction) String() string {
	if d == DirUp {
		return "⬆️"
	}
	return "⬇️"
}

// FrameType 帧类型码，与 vcpp 侧 ykc/enums/FrameType.java 一一对应。
type FrameType = byte

const (
	TypeLogin             FrameType = 0x01 // ⬆️ 充电桩登录认证
	TypeLoginAck          FrameType = 0x02 // ⬇️ 登录认证应答
	TypeHeartbeat         FrameType = 0x03 // ⬆️ 充电桩心跳包
	TypeHeartbeatAck      FrameType = 0x04 // ⬇️ 心跳包应答
	TypeBillingVerify     FrameType = 0x05 // ⬆️ 计费模型验证请求
	TypeBillingVerifyAck  FrameType = 0x06 // ⬇️ 计费模型验证请求应答
	TypeBillingModel      FrameType = 0x09 // ⬆️ 充电桩计费模型请求
	TypeBillingModelAck   FrameType = 0x0A // ⬇️ 计费模型请求应答
	TypeReadRealTime      FrameType = 0x12 // ⬇️ 读取实时监测数据
	TypeRealTimeUp        FrameType = 0x13 // ⬆️ 上传实时监测数据
	TypeHandshake         FrameType = 0x15 // ⬆️ 充电握手
	TypeParamConfig       FrameType = 0x17 // ⬆️ 参数配置
	TypeChargeFinish      FrameType = 0x19 // ⬆️ 充电结束
	TypeError             FrameType = 0x1B // ⬆️ 错误报文
	TypeChargingBmsStop   FrameType = 0x1D // ⬆️ 充电阶段BMS中止
	TypeChargingMotorStop FrameType = 0x21 // ⬆️ 充电阶段充电机中止
	TypeChargingBmsQa     FrameType = 0x23 // ⬆️ 充电过程BMS需求与充电机输出
	TypeChargingBmsStatus FrameType = 0x25 // ⬆️ 充电过程BMS信息
	TypeRemoteStartAck    FrameType = 0x33 // ⬆️ 远程启机命令回复
	TypeRemoteStart       FrameType = 0x34 // ⬇️ 运营平台远程控制启机
	TypeRemoteStopAck     FrameType = 0x35 // ⬆️ 远程停机命令回复
	TypeRemoteStop        FrameType = 0x36 // ⬇️ 运营平台远程停机
	TypeTradeUp           FrameType = 0x3B // ⬆️ 交易记录(V1.5/V1.6)
	TypeTradeUp2          FrameType = 0x3D // ⬆️ 交易记录(V1.7+)
	TypeTradeAck          FrameType = 0x40 // ⬇️ 交易记录确认
	TypeTimeSyncAck       FrameType = 0x55 // ⬆️ 对时设置应答
	TypeTimeSync          FrameType = 0x56 // ⬇️ 对时设置
	TypeBillingSettingAck FrameType = 0x57 // ⬆️ 计费模型设置应答
	TypeBillingSetting    FrameType = 0x58 // ⬇️ 计费模型设置
	TypeRemoteRestartAck  FrameType = 0x91 // ⬆️ 远程重启应答
	TypeRemoteRestart     FrameType = 0x92 // ⬇️ 远程重启
	TypeRemoteStart2Ack   FrameType = 0xA7 // ⬆️ 远程启机命令回复(V1.8+)
	TypeRemoteStart2      FrameType = 0xA8 // ⬇️ 运营平台远程控制启机(V1.8+)
)

var frameNames = map[FrameType]string{
	TypeLogin:             "充电桩登录认证",
	TypeLoginAck:          "登录认证应答",
	TypeHeartbeat:         "充电桩心跳包",
	TypeHeartbeatAck:      "心跳包应答",
	TypeBillingVerify:     "计费模型验证请求",
	TypeBillingVerifyAck:  "计费模型验证请求应答",
	TypeBillingModel:      "充电桩计费模型请求",
	TypeBillingModelAck:   "计费模型请求应答",
	TypeReadRealTime:      "读取实时监测数据",
	TypeRealTimeUp:        "上传实时监测数据",
	TypeHandshake:         "充电握手",
	TypeParamConfig:       "参数配置",
	TypeChargeFinish:      "充电结束",
	TypeError:             "错误报文",
	TypeChargingBmsStop:   "充电阶段BMS中止",
	TypeChargingMotorStop: "充电阶段充电机中止",
	TypeChargingBmsQa:     "充电过程BMS需求与充电机输出",
	TypeChargingBmsStatus: "充电过程BMS信息",
	TypeRemoteStartAck:    "远程启机命令回复",
	TypeRemoteStart:       "运营平台远程控制启机",
	TypeRemoteStopAck:     "远程停机命令回复",
	TypeRemoteStop:        "运营平台远程停机",
	TypeTradeUp:           "交易记录",
	TypeTradeUp2:          "交易记录",
	TypeTradeAck:          "交易记录确认",
	TypeTimeSyncAck:       "对时设置应答",
	TypeTimeSync:          "对时设置",
	TypeBillingSettingAck: "计费模型设置应答",
	TypeBillingSetting:    "计费模型设置",
	TypeRemoteRestartAck:  "远程重启应答",
	TypeRemoteRestart:     "远程重启",
	TypeRemoteStart2Ack:   "远程启机命令回复",
	TypeRemoteStart2:      "运营平台远程控制启机",
}

// FrameName 返回帧类型的中文名称。
func FrameName(t FrameType) string {
	if n, ok := frameNames[t]; ok {
		return n
	}
	return "未知帧类型"
}

// FrameDir 返回帧类型的通信方向：桩侧发起为上行，平台侧发起为下行。
func FrameDir(t FrameType) Direction {
	if t%2 == 1 {
		return DirUp
	}
	return DirDown
}

// FrameLabel 返回 "0x01 充电桩登录认证" 形式的标签。
func FrameLabel(t FrameType) string {
	return fmt.Sprintf("0x%02X %s", t, FrameName(t))
}

// ProtoVerByte 返回登录帧的通信协议版本字节：V1.5 -> 0x0F(15)，V1.7 -> 0x11(17)，V2.1 -> 0x15(21)。
func ProtoVerByte(version string) byte {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToUpper(strings.TrimSpace(version)))
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0x11
	}
	return byte(n)
}

// TradeFrameType 返回指定版本的交易记录帧类型，由协议规定：
// V1.5/V1.6 为 0x3B（无电表块），V1.7 起为 0x3D（含电表表号与密钥）。
func TradeFrameType(version string) byte {
	switch strings.ToUpper(strings.TrimSpace(version)) {
	case "V15", "V16":
		return TypeTradeUp
	}
	return TypeTradeUp2
}
