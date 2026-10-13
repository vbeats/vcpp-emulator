package config

import (
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/rest"

	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
)

// Config 是模拟器的统一配置，对应 etc/emulator.yaml。
// 字段标签沿用 go-zero 约定（json tag + default），yaml 会被转换为 json 后解析。
type Config struct {
	rest.RestConf

	Server   ServerConfig
	Protocol ProtocolConfig
	Timing   TimingConfig
	Billing  BillingConfig
	// FrameLog 通信帧日志开关。字段名避免与 go-zero ServiceConf 的 Log 冲突。
	FrameLog FrameLogConfig `json:"frameLog,optional"`
	Devices  []DeviceConfig `json:",optional"`
}

// ServerConfig 是服务端（vcpp）地址。同一时刻只允许连接一个 server 端。
type ServerConfig struct {
	Ip   string `json:",default=127.0.0.1"`
	Port int    `json:",default=9849"`
}

// ProtocolConfig 协议选择。帧格式、帧类型码、字节序等由协议规定，不在配置中暴露。
type ProtocolConfig struct {
	Type    string `json:",default=ykc"` // ykc | ocpp（ocpp 尚未实现）
	Version string `json:",default=V17"` // 云快充版本：V15 | V16 | V17
}

// TimingConfig 各类周期，单位秒。
type TimingConfig struct {
	ReconnectSec        int `json:",default=10"`
	DialTimeoutSec      int `json:",default=5"`
	HeartbeatSec        int `json:",default=10"`
	HeartbeatTimeoutSec int `json:",default=30"`
	ChargeReportSec     int `json:",default=15"`
	ChargeDurationSec   int `json:",default=600"`
	TradeRetry          int `json:",default=3"`
	TradeRetrySec       int `json:",default=30"`
}

// BillingConfig 模拟充电过程的电气参数，用于生成电量与账单。
type BillingConfig struct {
	ChargePowerKw   float64 `json:",default=60"`
	PackCapacityKwh float64 `json:",default=60"`
	MeterStartKwh   float64 `json:",default=1000"`
	InitialSoc      int     `json:",default=20"`
}

// FrameLogConfig 通信日志开关。
type FrameLogConfig struct {
	Hex    bool `json:",default=true"`
	Parsed bool `json:",default=true"`
}

// DeviceConfig 单个模拟桩。
// DeviceNo 云快充桩号：协议为 7 字节 BCD，即 14 位十进制数字，用整型表示。
type DeviceConfig struct {
	DeviceNo   int64
	GunCount   int     `json:",default=1"`
	AutoStart  bool    `json:",default=true"`
	PileType   int     `json:",default=0"`
	NetType    int     `json:",default=1"`
	McuVersion string  `json:",default=v1.0.0"`
	Sim        string  `json:",optional"`
	Supplier   int     `json:",default=0"`
	VoltageV   float64 `json:",default=400"`
}

// ProtocolVersions 已实现的云快充版本标签。V18/V21 尚未实现，配置会拒绝启动。
var ProtocolVersions = []string{"V15", "V16", "V17"}

// Validate 校验配置并返回首个人类可读的错误。
func (c Config) Validate() error {
	if c.Server.Ip == "" {
		return fmt.Errorf("server.ip 不能为空")
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	if !strings.EqualFold(c.Protocol.Type, "ykc") {
		return fmt.Errorf("protocol.type 当前仅支持 ykc，收到: %s", c.Protocol.Type)
	}
	if !contains(ProtocolVersions, strings.ToUpper(c.Protocol.Version)) {
		return fmt.Errorf("protocol.version 非法: %s，可选 %v", c.Protocol.Version, ProtocolVersions)
	}
	if c.Timing.ReconnectSec <= 0 || c.Timing.HeartbeatSec <= 0 ||
		c.Timing.HeartbeatTimeoutSec <= 0 || c.Timing.ChargeReportSec <= 0 ||
		c.Timing.ChargeDurationSec <= 0 || c.Timing.DialTimeoutSec <= 0 {
		return fmt.Errorf("timing 下的周期配置必须为正整数")
	}
	if len(c.Devices) == 0 {
		return fmt.Errorf("devices 至少配置一个模拟桩")
	}
	seen := make(map[int64]struct{}, len(c.Devices))
	for i, d := range c.Devices {
		if d.DeviceNo <= 0 || d.DeviceNo > ykc.MaxDeviceNo {
			return fmt.Errorf("devices[%d].deviceNo 必须为 1~14 位正整数，收到: %d", i, d.DeviceNo)
		}
		if _, ok := seen[d.DeviceNo]; ok {
			return fmt.Errorf("devices[%d].deviceNo 重复: %014d", i, d.DeviceNo)
		}
		seen[d.DeviceNo] = struct{}{}
		if d.GunCount <= 0 || d.GunCount > 255 {
			return fmt.Errorf("devices[%d].gunCount 必须在 1~255，收到: %d", i, d.GunCount)
		}
		if d.VoltageV <= 0 {
			return fmt.Errorf("devices[%d].voltageV 必须大于 0", i)
		}
	}
	if c.Billing.ChargePowerKw <= 0 || c.Billing.PackCapacityKwh <= 0 {
		return fmt.Errorf("billing.chargePowerKw / packCapacityKwh 必须大于 0")
	}
	return nil
}

// Addr 返回服务端地址。
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Ip, c.Server.Port)
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

// DeviceNoText 返回桩号的 14 位字符串形式，用于日志与 HTTP 输出。
func DeviceNoText(v int64) string { return fmt.Sprintf("%014d", v) }
