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

// 云快充 V1.5 / V1.6 / V1.7 定义的全部帧类型。
// 帧类型码奇偶约定：桩侧发起为奇数（上行 ⬆️），平台侧发起为偶数（下行 ⬇️）。
const (
	TypeLogin             FrameType = 0x01 // ⬆️ 充电桩登录认证
	TypeLoginAck          FrameType = 0x02 // ⬇️ 登录认证应答
	TypeHeartbeat         FrameType = 0x03 // ⬆️ 充电桩心跳包
	TypeHeartbeatAck      FrameType = 0x04 // ⬇️ 心跳包应答
	TypeBillingVerify     FrameType = 0x05 // ⬆️ 计费模型验证请求
	TypeBillingVerifyAck  FrameType = 0x06 // ⬇️ 计费模型验证请求应答
	TypeBillingModel      FrameType = 0x09 // ⬆️ 充电桩计费模型请求
	TypeBillingModelAck   FrameType = 0x0A // ⬇️ 计费模型请求应答（90B 尖峰平谷 / 98B 尖峰平谷深谷）
	TypeReadRealTime      FrameType = 0x12 // ⬇️ 读取实时监测数据
	TypeRealTimeUp        FrameType = 0x13 // ⬆️ 上传实时监测数据
	TypeHandshake         FrameType = 0x15 // ⬆️ 充电握手
	TypeParamConfig       FrameType = 0x17 // ⬆️ 参数配置
	TypeChargeFinish      FrameType = 0x19 // ⬆️ 充电结束
	TypeError             FrameType = 0x1B // ⬆️ 错误报文（BMS/充电机充电错误）
	TypeChargingBmsStop   FrameType = 0x1D // ⬆️ 充电阶段BMS中止
	TypeChargingMotorStop FrameType = 0x21 // ⬆️ 充电阶段充电机中止
	TypeChargingBmsQa     FrameType = 0x23 // ⬆️ 充电过程BMS需求与充电机输出
	TypeChargingBmsStatus FrameType = 0x25 // ⬆️ 充电过程BMS信息
	TypeChargeApply       FrameType = 0x31 // ⬆️ 充电桩主动申请启动充电
	TypeChargeApplyAck    FrameType = 0x32 // ⬇️ 运营平台确认启动充电
	TypeRemoteStartAck    FrameType = 0x33 // ⬆️ 远程启机命令回复
	TypeRemoteStart       FrameType = 0x34 // ⬇️ 运营平台远程控制启机
	TypeRemoteStopAck     FrameType = 0x35 // ⬆️ 远程停机命令回复
	TypeRemoteStop        FrameType = 0x36 // ⬇️ 运营平台远程停机
	TypeTradeUp           FrameType = 0x3B // ⬆️ 交易记录(V1.5/V1.6)
	TypeTradeUp2          FrameType = 0x3D // ⬆️ 交易记录(V1.7+，含电表块)
	TypeTradeAck          FrameType = 0x40 // ⬇️ 交易记录确认
	TypeBalanceAck        FrameType = 0x41 // ⬆️ 余额更新应答
	TypeBalanceUpdate     FrameType = 0x42 // ⬇️ 远程账户余额更新
	TypeCardSyncAck       FrameType = 0x43 // ⬆️ 卡数据同步应答
	TypeCardSync          FrameType = 0x44 // ⬇️ 离线卡数据同步
	TypeCardClearAck      FrameType = 0x45 // ⬆️ 离线卡数据清除应答
	TypeCardClear         FrameType = 0x46 // ⬇️ 离线卡数据清除
	TypeCardQueryAck      FrameType = 0x47 // ⬆️ 离线卡数据查询应答
	TypeCardQuery         FrameType = 0x48 // ⬇️ 离线卡数据查询
	TypeWorkParamAck      FrameType = 0x51 // ⬆️ 充电桩工作参数设置应答
	TypeWorkParam         FrameType = 0x52 // ⬇️ 充电桩工作参数设置
	TypeTimeSyncAck       FrameType = 0x55 // ⬆️ 对时设置应答
	TypeTimeSync          FrameType = 0x56 // ⬇️ 对时设置
	TypeBillingSettingAck FrameType = 0x57 // ⬆️ 计费模型设置应答
	TypeBillingSetting    FrameType = 0x58 // ⬇️ 计费模型设置
	TypeLockUp            FrameType = 0x61 // ⬆️ 地锁数据上送
	TypeLockCmd           FrameType = 0x62 // ⬇️ 遥控地锁升锁与降锁
	TypeLockAck           FrameType = 0x63 // ⬆️ 充电桩返回数据（地锁）
	TypeRemoteRestartAck  FrameType = 0x91 // ⬆️ 远程重启应答
	TypeRemoteRestart     FrameType = 0x92 // ⬇️ 远程重启
	TypeRemoteUpdateAck   FrameType = 0x93 // ⬆️ 远程更新应答
	TypeRemoteUpdate      FrameType = 0x94 // ⬇️ 远程更新
	TypeQrUrlAck          FrameType = 0x9B // ⬆️ 二维码下发应答（V1.6补充）
	TypeQrUrl             FrameType = 0x9C // ⬇️ 平台下发二维码（无桩号，V1.6补充）
	TypeQrUrlNo           FrameType = 0x9E // ⬇️ 平台下发二维码（含桩号，V1.6补充）
	TypeParallelApply     FrameType = 0xA1 // ⬆️ 充电桩主动申请并充充电（V1.6+）
	TypeParallelApplyAck  FrameType = 0xA2 // ⬇️ 运营平台确认并充启动充电（V1.6+）
	TypeParallelStartAck  FrameType = 0xA3 // ⬆️ 远程并充启机命令回复（V1.6+）
	TypeParallelStart     FrameType = 0xA4 // ⬇️ 运营平台远程控制并充启机（V1.6+）
	TypeQrPrefix          FrameType = 0xF0 // ⬇️ 二维码前缀下发（V1.5补充）
	TypeQrPrefixAck       FrameType = 0xF1 // ⬆️ 二维码前缀下发应答（V1.5补充）

	// 以下帧在真实设备运行日志（doc/device-*.log）中观察到，属于桩企/运营商私有扩展，
	// 云快充 V1.5/V1.6/V1.7 文档均无定义，仅登记名称用于日志可读，不做字段解析。
	TypeSfPriceModel    FrameType = 0x0B // ⬆️ 计费模型请求（自由定价/瞬峰）
	TypeSfPriceModelAck FrameType = 0x0C // ⬇️ 计费模型应答（自由定价/瞬峰）
	TypeSfTrade         FrameType = 0x81 // ⬆️ 交易记录（自由定价，含 48 时段电量明细）
	TypeRemoteStart2Ack FrameType = 0xA7 // ⬆️ 远程启机命令回复(V1.8+)
	TypeRemoteStart2    FrameType = 0xA8 // ⬇️ 运营平台远程控制启机(V1.8+)
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
	TypeChargeApply:       "充电桩主动申请启动充电",
	TypeChargeApplyAck:    "运营平台确认启动充电",
	TypeRemoteStartAck:    "远程启机命令回复",
	TypeRemoteStart:       "运营平台远程控制启机",
	TypeRemoteStopAck:     "远程停机命令回复",
	TypeRemoteStop:        "运营平台远程停机",
	TypeTradeUp:           "交易记录",
	TypeTradeUp2:          "交易记录",
	TypeTradeAck:          "交易记录确认",
	TypeBalanceAck:        "余额更新应答",
	TypeBalanceUpdate:     "远程账户余额更新",
	TypeCardSyncAck:       "卡数据同步应答",
	TypeCardSync:          "离线卡数据同步",
	TypeCardClearAck:      "离线卡数据清除应答",
	TypeCardClear:         "离线卡数据清除",
	TypeCardQueryAck:      "离线卡数据查询应答",
	TypeCardQuery:         "离线卡数据查询",
	TypeWorkParamAck:      "充电桩工作参数设置应答",
	TypeWorkParam:         "充电桩工作参数设置",
	TypeTimeSyncAck:       "对时设置应答",
	TypeTimeSync:          "对时设置",
	TypeBillingSettingAck: "计费模型设置应答",
	TypeBillingSetting:    "计费模型设置",
	TypeLockUp:            "地锁数据上送",
	TypeLockCmd:           "遥控地锁升锁与降锁",
	TypeLockAck:           "充电桩返回数据",
	TypeRemoteRestartAck:  "远程重启应答",
	TypeRemoteRestart:     "远程重启",
	TypeRemoteUpdateAck:   "远程更新应答",
	TypeRemoteUpdate:      "远程更新",
	TypeQrUrlAck:          "二维码下发应答",
	TypeQrUrl:             "平台下发二维码",
	TypeQrUrlNo:           "平台下发二维码",
	TypeParallelApply:     "充电桩主动申请并充充电",
	TypeParallelApplyAck:  "运营平台确认并充启动充电",
	TypeParallelStartAck:  "远程并充启机命令回复",
	TypeParallelStart:     "运营平台远程控制并充启机",
	TypeQrPrefix:          "二维码前缀下发",
	TypeQrPrefixAck:       "二维码前缀下发应答",
	TypeSfPriceModel:      "计费模型请求[私有:自由定价]",
	TypeSfPriceModelAck:   "计费模型应答[私有:自由定价]",
	TypeSfTrade:           "交易记录[私有:自由定价]",
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

// 版本标签常量。
const (
	V15 = "V15"
	V16 = "V16"
	V17 = "V17"
	V18 = "V18"
	V21 = "V21"
)

// ProtoVerByte 返回登录帧的通信协议版本字节（版本号 ×10）：
// V1.5 -> 0x0F(15)，V1.6 -> 0x10(16)，V1.7 -> 0x11(17)，V2.1 -> 0x15(21)。
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
// V1.5/V1.6 为 0x3B（无电表块，158 字节起），V1.7 起为 0x3D（含电表表号与密文）。
func TradeFrameType(version string) FrameType {
	switch strings.ToUpper(strings.TrimSpace(version)) {
	case V15, V16:
		return TypeTradeUp
	}
	return TypeTradeUp2
}

// TradeWithMeter 返回该版本的交易记录是否携带电表块：仅 V1.7 起携带。
func TradeWithMeter(version string) bool {
	return TradeFrameType(version) == TypeTradeUp2
}

// SupportsParallel 返回该版本是否支持双枪并充（0xA1~0xA4，V1.6 起引入）。
func SupportsParallel(version string) bool {
	switch strings.ToUpper(strings.TrimSpace(version)) {
	case V15:
		return false
	}
	return true
}
