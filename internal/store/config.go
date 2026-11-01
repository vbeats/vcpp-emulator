// Package store 负责模拟器配置的加载与持久化。
//
// 配置解析方式对齐服务端 Spring Boot 工程（vcpp）：
//   - 单一 YAML 配置文件（可写注释），而非不带注释的 JSON；
//   - 默认值集中在 Default()，缺字段或越界取值在「取值点」回退默认值而不拒绝启动；
//   - 首次运行按默认模板生成配置文件，便于直接手工编辑；旧版 JSON 配置自动迁移。
package store

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigFile 配置文件名
	ConfigFile = "config.yml"
	// LegacyConfigFile 旧版 JSON 配置文件名，读到后自动迁移为 YAML
	LegacyConfigFile = "config.json"
	// ConfigPathEnv 显式指定配置文件路径的环境变量
	ConfigPathEnv = "VCPP_EMU_CONFIG"
)

// 取值合理区间：超出区间即回退默认值并记入修正说明。
// 与服务端一致：关键项自行降级到可用值，而不是启动前强校验拒绝。
const (
	minHeartbeatSec      = 5
	maxHeartbeatSec      = 600
	minRealTimeIdleSec   = 10
	maxRealTimeIdleSec   = 3600
	minRealTimeChargeSec = 5
	maxRealTimeChargeSec = 600
	minReconnectSec      = 1
	maxReconnectSec      = 600
	minServerPort        = 1
	maxServerPort        = 65535

	defaultGunCount   = 1
	defaultRatedPower = 120
	defaultProgramVer = "220422"
)

// 加油站模块取值区间
const (
	minDispenserCount  = 1
	maxDispenserCount  = 64
	minNozzleCount     = 1
	maxNozzleCount     = 8
	minFuelPrice       = 0.01
	maxFuelPrice       = 99.99
	minPushIntervalSec = 1
	maxPushIntervalSec = 600
	minHTTPTimeoutSec  = 1
	maxHTTPTimeoutSec  = 60
	minMqttPort        = 1
	maxMqttPort        = 65535
	minMqttQos         = 0
	maxMqttQos         = 2
	minTankVolumeL     = 1
	maxTankVolumeL     = 500

	defaultDispenserCount  = 1
	defaultNozzleCount     = 2
	defaultPushIntervalSec = 5
	defaultHTTPTimeoutSec  = 10
	defaultMqttPort        = 1883
	defaultMqttQos         = 1
	defaultTankVolumeL     = 60
	defaultMqttBroker      = "10.88.0.81"
	defaultMqttUsername    = "admin"
	defaultMqttPassword    = "admin123456"
	defaultMqttClientID    = "vcpp-emulator-gas"
	defaultHTTPBaseURL     = "http://10.88.0.81:18083"
)

// 数据推送形式
const (
	PushModeNone = "none" // 不推送
	PushModeMqtt = "mqtt" // MQTT 服务器
	PushModeHTTP = "http" // HTTP POST JSON
)

// 加油目标模式
const (
	FuelTargetFree   = "free"   // 加满为止（手动停止 / 跳枪自动停止）
	FuelTargetAmount = "amount" // 定额加油：达到指定金额自动停止
	FuelTargetVolume = "volume" // 定量加油：达到指定升数自动停止
)

// ServerConfig 服务端连接配置
type ServerConfig struct {
	Host    string `json:"host" yaml:"host"`       // 服务端 IP / 域名
	Port    int    `json:"port" yaml:"port"`       // 端口，9847~9851 对应不同协议版本
	Version string `json:"version" yaml:"version"` // 协议版本：V15 / V16 / V17 / V18 / V21
}

// PileConfig 单个充电桩配置
type PileConfig struct {
	ID         string  `json:"id" yaml:"id"`                  // 内部唯一标识
	PileNo     string  `json:"pileNo" yaml:"pile-no"`         // 桩号（14 位数字）
	GunCount   int     `json:"gunCount" yaml:"gun-count"`     // 枪数量
	PileType   int     `json:"pileType" yaml:"pile-type"`     // 0 直流 1 交流
	RatedPower float64 `json:"ratedPower" yaml:"rated-power"` // 额定功率 kW
	ProgramVer string  `json:"programVer" yaml:"program-ver"` // 程序版本
	Enabled    bool    `json:"enabled" yaml:"enabled"`        // 是否启用（自动连接）
}

// ChargeTarget 默认充电目标（界面发起充电时作为初始值）
type ChargeTarget struct {
	Mode   string  `json:"mode" yaml:"mode"`     // amount / time / energy / soc
	Amount float64 `json:"amount" yaml:"amount"` // 目标金额（元）
	Minute float64 `json:"minute" yaml:"minute"` // 目标时长（分钟）
	Energy float64 `json:"energy" yaml:"energy"` // 目标电量（kWh）
	Soc    int     `json:"soc" yaml:"soc"`       // 目标 SOC（%）
}

// StartChargeForm 最近一次「启动充电」表单填写内容。
// 只保留一份（最近填写），供界面下次打开表单时回填，避免用户重复录入。
type StartChargeForm struct {
	StartType  int          `json:"startType" yaml:"start-type"`   // 启动方式：1 刷卡 / 3 VIN
	CardNo     string       `json:"cardNo" yaml:"card-no"`         // 卡号
	Vin        string       `json:"vin" yaml:"vin"`                // VIN（启动方式为 VIN 时使用）
	BatteryKwh float64      `json:"batteryKwh" yaml:"battery-kwh"` // 电池容量 kWh
	StartSoc   int          `json:"startSoc" yaml:"start-soc"`     // 起始 SOC %
	MaxVoltage float64      `json:"maxVoltage" yaml:"max-voltage"` // 电池最高允许电压 V
	MaxCurrent float64      `json:"maxCurrent" yaml:"max-current"` // 电池最高允许电流 A
	Target     ChargeTarget `json:"target" yaml:"target"`          // 充电目标
}

// FuelOption 油品选项：统一油号目录，同时给出界面区分色。
// 加油站各油枪只能从该目录中选择油号，油价亦按目录逐项统一配置。
type FuelOption struct {
	Code     string `json:"code" yaml:"code"`         // 油号：-10# / 0# / 92# / 95# / 98# / 100#
	Name     string `json:"name" yaml:"name"`        // 展示名
	Category string `json:"category" yaml:"category"` // 柴油 / 汽油 / 甲醇
	Color    string `json:"color" yaml:"color"`       // 界面区分色（十六进制）
}

// fuelCatalog 油号目录，顺序即界面下拉顺序
var fuelCatalog = []FuelOption{
	{Code: "-10#", Name: "-10# 柴油", Category: "柴油", Color: "#0f766e"},
	{Code: "0#", Name: "0# 柴油", Category: "柴油", Color: "#15803d"},
	{Code: "92#", Name: "92# 汽油", Category: "汽油", Color: "#b45309"},
	{Code: "95#", Name: "95# 汽油", Category: "汽油", Color: "#c2410c"},
	{Code: "98#", Name: "98# 汽油", Category: "汽油", Color: "#9f1239"},
	{Code: "100#", Name: "100# 甲醇", Category: "甲醇", Color: "#4338ca"},
}

// FuelOptions 油号目录副本
func FuelOptions() []FuelOption {
	out := make([]FuelOption, len(fuelCatalog))
	copy(out, fuelCatalog)
	return out
}

// FuelDefaultCode 缺省油号
const FuelDefaultCode = "92#"

// FuelByCode 按油号查目录项
func FuelByCode(code string) (FuelOption, bool) {
	for _, f := range fuelCatalog {
		if f.Code == code {
			return f, true
		}
	}
	return FuelOption{}, false
}

// FuelPrice 油品单价（元 / 升，两位小数）
type FuelPrice struct {
	Code  string  `json:"code" yaml:"code"`
	Price float64 `json:"price" yaml:"price"`
}

// DefaultFuelPrices 默认油价：与油号目录一一对应
func DefaultFuelPrices() []FuelPrice {
	return []FuelPrice{
		{Code: "-10#", Price: 7.25},
		{Code: "0#", Price: 6.88},
		{Code: "92#", Price: 7.68},
		{Code: "95#", Price: 8.19},
		{Code: "98#", Price: 8.96},
		{Code: "100#", Price: 6.50},
	}
}

// FuelPriceOf 取指定油号单价，缺失时回退目录默认值
func FuelPriceOf(prices []FuelPrice, code string) float64 {
	for _, p := range prices {
		if p.Code == code {
			return p.Price
		}
	}
	for _, p := range DefaultFuelPrices() {
		if p.Code == code {
			return p.Price
		}
	}
	for _, p := range DefaultFuelPrices() {
		if p.Code == FuelDefaultCode {
			return p.Price
		}
	}
	return 0
}

// MqttConfig MQTT 推送配置。
// 18083 为 EMQX 控制台端口，实际 MQTT 连接使用 1883，二者分别配置互不冲突。
type MqttConfig struct {
	Broker        string `json:"broker" yaml:"broker"`                 // 服务器地址
	Port          int    `json:"port" yaml:"port"`                     // MQTT 端口
	ClientID      string `json:"clientId" yaml:"client-id"`            // 客户端标识
	Username      string `json:"username" yaml:"username"`             // 用户名
	Password      string `json:"password" yaml:"password"`             // 口令
	TopicProgress string `json:"topicProgress" yaml:"topic-progress"`  // 加油进度推送 topic
	TopicOrder    string `json:"topicOrder" yaml:"topic-order"`        // 加油订单推送 topic
	Qos           int    `json:"qos" yaml:"qos"`                      // 0 / 1 / 2
}

// HTTPPushConfig HTTP POST JSON 推送配置
type HTTPPushConfig struct {
	BaseURL      string `json:"baseUrl" yaml:"base-url"`           // 接口基址
	PathProgress string `json:"pathProgress" yaml:"path-progress"` // 加油进度推送路径
	PathOrder    string `json:"pathOrder" yaml:"path-order"`       // 加油订单推送路径
	Token        string `json:"token" yaml:"token"`                // 可选鉴权令牌
	TimeoutSec   int    `json:"timeoutSec" yaml:"timeout-sec"`     // 超时（秒）
}

// NozzleConfig 单把油枪配置，枪号为字符串，按 1 ~ x 顺序排列
type NozzleConfig struct {
	No       string `json:"no" yaml:"no"`             // 枪号：1 / 2 / ...
	FuelCode string `json:"fuelCode" yaml:"fuel-code"` // 油号，取自油号目录
}

// DispenserConfig 单台加油机配置。
// 油机没有启用 / 停用状态：配置中存在即投入模拟运行。
type DispenserConfig struct {
	ID          string         `json:"id" yaml:"id"`                     // 内部唯一标识
	DispenserNo string         `json:"dispenserNo" yaml:"dispenser-no"` // 油机号
	NozzleCount int            `json:"nozzleCount" yaml:"nozzle-count"` // 枪数量
	Nozzles     []NozzleConfig `json:"nozzles" yaml:"nozzles"`           // 各枪油号配置
}

// FuelTarget 加油目标：定额（金额）/ 定量（升数）/ 加满
type FuelTarget struct {
	Mode   string  `json:"mode" yaml:"mode"`     // free / amount / volume
	Amount float64 `json:"amount" yaml:"amount"` // 定额金额（元）
	Volume float64 `json:"volume" yaml:"volume"` // 定量升数（L）
}

// StartFuelForm 最近一次「开始加油」表单填写内容
type StartFuelForm struct {
	Target  FuelTarget `json:"target" yaml:"target"`
	CardNo  string     `json:"cardNo" yaml:"card-no"`
	PlateNo string     `json:"plateNo" yaml:"plate-no"`
}

// DefaultStartFuel 开始加油表单默认值
func DefaultStartFuel() StartFuelForm {
	return StartFuelForm{
		Target:  FuelTarget{Mode: FuelTargetAmount, Amount: 200, Volume: 30},
		CardNo:  "88888888",
		PlateNo: "京A12345",
	}
}

// GasConfig 加油站模块公共配置：数据推送形式、统一油价与油机列表。
// 该配置属于整个油站，不随单台油机下放，所有油机共用同一套推送通道与油价。
type GasConfig struct {
	PushMode        string            `json:"pushMode" yaml:"push-mode"`               // none / mqtt / http
	Mqtt            MqttConfig        `json:"mqtt" yaml:"mqtt"`                        // MQTT 推送配置
	HTTP            HTTPPushConfig    `json:"http" yaml:"http"`                        // HTTP 推送配置
	Prices          []FuelPrice       `json:"prices" yaml:"prices"`                    // 统一油价（按油号）
	Dispensers      []DispenserConfig `json:"dispensers" yaml:"dispensers"`             // 油机列表
	PushIntervalSec int               `json:"pushIntervalSec" yaml:"push-interval-sec"` // 加油过程推送周期（秒）
	TankVolumeL     float64           `json:"tankVolumeL" yaml:"tank-volume-l"`         // 油箱容积 L，加满自动跳枪
	LastStartFuel   StartFuelForm     `json:"lastStartFuel" yaml:"last-start-fuel"`     // 最近一次开始加油表单填写内容
}

// DefaultGas 加油站默认配置：1 台油机 2 把油枪
func DefaultGas() GasConfig {
	mqtt := DefaultMqtt()
	http := DefaultHTTPPush()
	return GasConfig{
		PushMode:        PushModeMqtt,
		Mqtt:            mqtt,
		HTTP:            http,
		Prices:          DefaultFuelPrices(),
		Dispensers:      GenerateDispensers(defaultDispenserCount, defaultNozzleCount),
		PushIntervalSec: defaultPushIntervalSec,
		TankVolumeL:     defaultTankVolumeL,
		LastStartFuel:   DefaultStartFuel(),
	}
}

// DefaultMqtt MQTT 推送默认配置
func DefaultMqtt() MqttConfig {
	return MqttConfig{
		Broker:        defaultMqttBroker,
		Port:          defaultMqttPort,
		ClientID:      defaultMqttClientID,
		Username:      defaultMqttUsername,
		Password:      defaultMqttPassword,
		TopicProgress: "vcpp/gas/{dispenser}/nozzle/{nozzle}/progress",
		TopicOrder:    "vcpp/gas/{dispenser}/nozzle/{nozzle}/order",
		Qos:           defaultMqttQos,
	}
}

// DefaultHTTPPush HTTP 推送默认配置
func DefaultHTTPPush() HTTPPushConfig {
	return HTTPPushConfig{
		BaseURL:      defaultHTTPBaseURL,
		PathProgress: "/api/gas/fuel/progress",
		PathOrder:    "/api/gas/fuel/order",
		Token:        "",
		TimeoutSec:   defaultHTTPTimeoutSec,
	}
}

// GenerateDispensers 按「油机台数 × 每台枪数」生成油机配置。
// 油机号按 01 / 02 ... 排列，每台油机的枪号固定从 1 开始递增；
// 油号按油号目录轮转分配，用户可在界面逐枪调整。
func GenerateDispensers(count, nozzleCount int) []DispenserConfig {
	if count < minDispenserCount {
		count = minDispenserCount
	}
	if count > maxDispenserCount {
		count = maxDispenserCount
	}
	if nozzleCount < minNozzleCount {
		nozzleCount = minNozzleCount
	}
	if nozzleCount > maxNozzleCount {
		nozzleCount = maxNozzleCount
	}
	cat := fuelCatalog
	out := make([]DispenserConfig, 0, count)
	for i := 0; i < count; i++ {
		nozzles := make([]NozzleConfig, 0, nozzleCount)
		for j := 0; j < nozzleCount; j++ {
			code := cat[(i*nozzleCount+j)%len(cat)].Code
			nozzles = append(nozzles, NozzleConfig{No: strconv.Itoa(j + 1), FuelCode: code})
		}
		out = append(out, DispenserConfig{
			ID:          fmt.Sprintf("dispenser-%d", i+1),
			DispenserNo: fmt.Sprintf("%02d", i+1),
			NozzleCount: nozzleCount,
			Nozzles:     nozzles,
		})
	}
	return out
}

// AppConfig 应用配置，对应单个 YAML 配置文件
type AppConfig struct {
	Server            ServerConfig    `json:"server" yaml:"server"`
	Piles             []PileConfig    `json:"piles" yaml:"piles"`
	Gas               GasConfig       `json:"gas" yaml:"gas"`
	HeartbeatSec      int             `json:"heartbeatSec" yaml:"heartbeat-sec"`             // 心跳周期（秒）
	RealTimeIdleSec   int             `json:"realTimeIdleSec" yaml:"real-time-idle-sec"`     // 空闲实时数据周期（秒）
	RealTimeChargeSec int             `json:"realTimeChargeSec" yaml:"real-time-charge-sec"` // 充电中实时数据周期（秒）
	AutoConnect       bool            `json:"autoConnect" yaml:"auto-connect"`               // 启动时自动连接已启用的桩（默认关闭，保持未连接）
	AutoReconnect     bool            `json:"autoReconnect" yaml:"auto-reconnect"`           // 通信中断后自动重连
	ReconnectSec      int             `json:"reconnectSec" yaml:"reconnect-sec"`             // 重连间隔（秒）
	DefaultTarget     ChargeTarget    `json:"defaultTarget" yaml:"default-target"`           // 默认充电目标
	LastStartCharge   StartChargeForm `json:"lastStartCharge" yaml:"last-start-charge"`      // 最近一次启动充电表单填写内容
}

// defaultTargetMode 缺省充电目标模式（按金额）
const defaultTargetMode = "amount"

// DefaultStartCharge 启动充电表单默认值
func DefaultStartCharge() StartChargeForm {
	return StartChargeForm{
		StartType:  1,
		CardNo:     "88888888",
		BatteryKwh: 60,
		StartSoc:   20,
		MaxVoltage: 750,
		MaxCurrent: 250,
		Target: ChargeTarget{
			Mode:   defaultTargetMode,
			Amount: 30,
			Minute: 30,
			Energy: 20,
			Soc:    90,
		},
	}
}

// Default 默认配置：测试服务 10.88.0.81:9847（云快充 1.5）
func Default() AppConfig {
	return AppConfig{
		Server: ServerConfig{
			Host:    "10.88.0.81",
			Port:    9847,
			Version: "V15",
		},
		Piles: []PileConfig{
			{
				ID:         "pile-1",
				PileNo:     "20260501000001",
				GunCount:   2,
				PileType:   0,
				RatedPower: 120,
				ProgramVer: "220422",
				Enabled:    true,
			},
		},
		HeartbeatSec:      10,
		RealTimeIdleSec:   300,
		RealTimeChargeSec: 15,
		AutoConnect:       false,
		AutoReconnect:     true,
		ReconnectSec:      30,
		DefaultTarget:     DefaultStartCharge().Target,
		LastStartCharge:   DefaultStartCharge(),
		Gas:               DefaultGas(),
	}
}

// configTemplate 落盘时写在文件头的字段说明，替代 JSON 时代的「无法注释」
const configTemplate = `# vcpp-emulator 运行配置
#
# - 全部字段均可省略：缺省或超出合理区间的取值会在读取时回退默认值，并在启动日志中记录，不会因此拒绝启动。
# - server.host / server.port / server.version：对接的平台服务端地址与协议版本（V15/V16/V17/V18/V21）。
# - heartbeat-sec 心跳周期[5,600]；real-time-idle-sec 空闲实时数据周期[10,3600]；
#   real-time-charge-sec 充电中实时数据周期[5,600]；reconnect-sec 重连间隔[1,600]。
# - auto-connect 默认 false：应用启动后保持未连接，需在界面手动连接；手动连接的桩在通信中断后
#   按 auto-reconnect（默认开启）以 reconnect-sec 为周期自动重连。
# - piles 为桩列表（pile-no 为 14 位桩号）；last-start-charge 记录最近一次「启动充电」界面的填写内容。
#
# ---------------- 加油站模块（gas，全站公共配置） ----------------
# - gas 为全站公共配置：所有油机共用同一套数据推送通道、油价与油箱容积，不按油机分别下放。
# - gas.push-mode 数据推送形式：none 不推送 / mqtt 推送到 MQTT 服务器 / http 以 JSON 形式 POST 到接口。
# - gas.mqtt：broker + port（MQTT 端口，默认 1883；18083 为 EMQX 控制台端口）、client-id、
#   username / password、topic-progress 进度推送主题、topic-order 订单推送主题、qos[0,2]。
#   topic 支持 {dispenser} 与 {nozzle} 占位符，运行时替换为油机号与枪号。
# - gas.http：base-url 接口基址、path-progress 进度推送路径、path-order 订单推送路径、
#   token 可选鉴权令牌、timeout-sec 超时[1,60]。
# - gas.prices 统一油价：按油号配置单价（元/升，两位小数），油号取自
#   -10# / 0# / 92# / 95# / 98# / 100#。
# - gas.dispensers 油机列表：dispenser-no 油机号、nozzle-count 枪数、nozzles 逐枪油号。
#   油机 / 油枪也可在界面「批量生成」一键生成（台数 1~64、每台枪数 1~8，生成结果落在此列表）。
# - gas.push-interval-sec[1,600] 加油过程推送周期；gas.tank-volume-l[1,500] 油箱容积，加满自动跳枪。
# - 油机没有启用 / 停用状态：gas.dispensers 中出现的油机随配置载入即投入运行。
# - gas.last-start-fuel 记录最近一次「开始加油」界面的填写内容。
#
`

// Store 配置存储
type Store struct {
	mu    sync.Mutex
	path  string
	cfg   AppConfig
	notes []string // 加载期间的配置修正说明，供启动日志输出
}

// New 创建配置存储，dir 为配置文件目录。
// 配置发现顺序：环境变量 VCPP_EMU_CONFIG → <dir>/config.yml → <dir>/config.json（自动迁移）。
// 文件缺失时按默认模板生成；只有结构性损坏才整体回退默认值。
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	yml := filepath.Join(dir, ConfigFile)
	path, legacy := resolvePath(dir, yml)
	s := &Store{path: path, cfg: Default()}

	switch {
	case legacy:
		// 旧版 JSON：读成功后重写为 YAML，原文件保留为 .bak
		if err := s.load(); err != nil {
			s.cfg = Default()
			s.note("旧版配置解析失败(%v)，已回退默认值", err)
			break
		}
		if err := s.write(yml); err != nil {
			s.note("旧版配置迁移失败: %v", err)
			break
		}
		_ = os.Rename(path, path+".bak")
		s.path = yml
		s.note("旧版 %s 已迁移为 %s（原文件保留为 %s.bak）", LegacyConfigFile, ConfigFile, LegacyConfigFile)
	default:
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := s.write(path); err != nil {
				s.note("默认配置文件写入失败: %v", err)
			}
			break
		}
		if err := s.load(); err != nil {
			s.cfg = Default()
			s.note("配置文件解析失败(%v)，已回退默认值", err)
		}
	}
	return s, nil
}

// resolvePath 配置路径发现：环境变量显式路径 → config.yml → 旧版 config.json
func resolvePath(dir, yml string) (path string, legacy bool) {
	if p := strings.TrimSpace(os.Getenv(ConfigPathEnv)); p != "" {
		return p, false
	}
	js := filepath.Join(dir, LegacyConfigFile)
	if _, err := os.Stat(yml); err == nil {
		return yml, false
	}
	if _, err := os.Stat(js); err == nil {
		return js, true
	}
	return yml, false
}

// Path 配置文件路径
func (s *Store) Path() string { return s.path }

// Notes 加载期间的配置修正说明
func (s *Store) Notes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.notes))
	copy(out, s.notes)
	return out
}

// Get 获取当前配置（返回副本）
func (s *Store) Get() AppConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Save 保存配置（先写 .tmp 再 rename 原子替换）
func (s *Store) Save(cfg AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	return s.write(s.path)
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg AppConfig
	if strings.EqualFold(filepath.Ext(s.path), ".json") {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return err
		}
	} else if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	s.notes = append(s.notes, normalize(&cfg)...)
	s.cfg = cfg
	return nil
}

// write 将当前配置写入指定路径，落盘前同样做取值兜底
func (s *Store) write(path string) error {
	cfg := s.cfg
	normalize(&cfg)
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	out := append([]byte(configTemplate), data...)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) note(format string, a ...any) {
	s.notes = append(s.notes, fmt.Sprintf(format, a...))
}

// normalize 取值兜底：缺值回退默认、越界回退并记录修正说明。
// 与服务端一致——不做一次性强校验拒绝启动，而是在取值点降级到可用值。
func normalize(cfg *AppConfig) []string {
	def := Default()
	var notes []string
	add := func(format string, a ...any) {
		notes = append(notes, fmt.Sprintf(format, a...))
	}

	cfg.Server.Host = strings.TrimSpace(cfg.Server.Host)
	if cfg.Server.Host == "" {
		cfg.Server.Host = def.Server.Host
		add("server.host 为空，已回退 %s", def.Server.Host)
	}
	if cfg.Server.Port < minServerPort || cfg.Server.Port > maxServerPort {
		cfg.Server.Port = def.Server.Port
		add("server.port 超出 [%d,%d]，已回退 %d", minServerPort, maxServerPort, def.Server.Port)
	}
	cfg.Server.Version = strings.TrimSpace(cfg.Server.Version)
	if cfg.Server.Version == "" {
		cfg.Server.Version = def.Server.Version
		add("server.version 为空，已回退 %s", def.Server.Version)
	}

	reset(add, "heartbeat-sec", &cfg.HeartbeatSec, minHeartbeatSec, maxHeartbeatSec, def.HeartbeatSec)
	reset(add, "real-time-idle-sec", &cfg.RealTimeIdleSec, minRealTimeIdleSec, maxRealTimeIdleSec, def.RealTimeIdleSec)
	reset(add, "real-time-charge-sec", &cfg.RealTimeChargeSec, minRealTimeChargeSec, maxRealTimeChargeSec, def.RealTimeChargeSec)
	reset(add, "reconnect-sec", &cfg.ReconnectSec, minReconnectSec, maxReconnectSec, def.ReconnectSec)

	if cfg.DefaultTarget.Mode == "" {
		cfg.DefaultTarget = def.DefaultTarget
		add("default-target 为空，已回退默认值")
	}

	piles := make([]PileConfig, 0, len(cfg.Piles))
	for i, p := range cfg.Piles {
		p.PileNo = strings.TrimSpace(p.PileNo)
		if p.PileNo == "" {
			add("piles[%d] 缺少桩号，已忽略该桩", i)
			continue
		}
		if len(p.PileNo) != 14 {
			add("piles[%d] 桩号 %s 不是 14 位，请核对", i, p.PileNo)
		}
		if p.ID == "" {
			p.ID = fmt.Sprintf("pile-%d", i+1)
		}
		if p.GunCount <= 0 {
			p.GunCount = defaultGunCount
		}
		if p.PileType != 0 && p.PileType != 1 {
			p.PileType = 0
		}
		if p.RatedPower <= 0 {
			p.RatedPower = defaultRatedPower
		}
		if p.ProgramVer == "" {
			p.ProgramVer = defaultProgramVer
		}
		piles = append(piles, p)
	}
	cfg.Piles = piles

	// 先判空再钳制：整份缺失（旧配置）直接套用默认值，
	// 部分填写时只对异常项做修补，保留用户刻意填入的取值。
	if cfg.LastStartCharge == (StartChargeForm{}) {
		cfg.LastStartCharge = DefaultStartCharge()
	} else {
		normalizeStartCharge(&cfg.LastStartCharge)
	}

	notes = append(notes, normalizeGas(&cfg.Gas)...)

	return notes
}

// normalizeGas 加油站模块取值兜底：推送通道、统一油价与油机列表。
// 与充电桩一致的口径——单项异常只修补该项，不因局部问题丢弃整份油站配置。
func normalizeGas(g *GasConfig) []string {
	def := DefaultGas()
	var notes []string
	add := func(format string, a ...any) {
		notes = append(notes, fmt.Sprintf(format, a...))
	}

	g.PushMode = strings.TrimSpace(g.PushMode)
	switch g.PushMode {
	case PushModeNone, PushModeMqtt, PushModeHTTP:
	default:
		if g.PushMode != "" {
			add("gas.push-mode=%s 非法，已回退 %s", g.PushMode, def.PushMode)
		}
		g.PushMode = def.PushMode
	}

	g.Mqtt.Broker = strings.TrimSpace(g.Mqtt.Broker)
	if g.Mqtt.Broker == "" {
		g.Mqtt.Broker = def.Mqtt.Broker
	}
	if g.Mqtt.Port < minMqttPort || g.Mqtt.Port > maxMqttPort {
		if g.Mqtt.Port != 0 {
			add("gas.mqtt.port=%d 超出 [%d,%d]，已回退 %d", g.Mqtt.Port, minMqttPort, maxMqttPort, def.Mqtt.Port)
		}
		g.Mqtt.Port = def.Mqtt.Port
	}
	g.Mqtt.ClientID = strings.TrimSpace(g.Mqtt.ClientID)
	if g.Mqtt.ClientID == "" {
		g.Mqtt.ClientID = def.Mqtt.ClientID
	}
	// 用户名与口令允许为空（匿名接入），仅做去空格处理
	g.Mqtt.Username = strings.TrimSpace(g.Mqtt.Username)
	g.Mqtt.Password = strings.TrimSpace(g.Mqtt.Password)
	g.Mqtt.TopicProgress = strings.TrimSpace(g.Mqtt.TopicProgress)
	if g.Mqtt.TopicProgress == "" {
		g.Mqtt.TopicProgress = def.Mqtt.TopicProgress
	}
	g.Mqtt.TopicOrder = strings.TrimSpace(g.Mqtt.TopicOrder)
	if g.Mqtt.TopicOrder == "" {
		g.Mqtt.TopicOrder = def.Mqtt.TopicOrder
	}
	if g.Mqtt.Qos < minMqttQos || g.Mqtt.Qos > maxMqttQos {
		add("gas.mqtt.qos=%d 超出 [%d,%d]，已回退 %d", g.Mqtt.Qos, minMqttQos, maxMqttQos, def.Mqtt.Qos)
		g.Mqtt.Qos = def.Mqtt.Qos
	}

	g.HTTP.BaseURL = strings.TrimRight(strings.TrimSpace(g.HTTP.BaseURL), "/")
	g.HTTP.PathProgress = strings.TrimSpace(g.HTTP.PathProgress)
	if g.HTTP.PathProgress == "" {
		g.HTTP.PathProgress = def.HTTP.PathProgress
	}
	g.HTTP.PathOrder = strings.TrimSpace(g.HTTP.PathOrder)
	if g.HTTP.PathOrder == "" {
		g.HTTP.PathOrder = def.HTTP.PathOrder
	}
	g.HTTP.Token = strings.TrimSpace(g.HTTP.Token)
	reset(add, "gas.http.timeout-sec", &g.HTTP.TimeoutSec, minHTTPTimeoutSec, maxHTTPTimeoutSec, def.HTTP.TimeoutSec)

	if g.TankVolumeL < minTankVolumeL || g.TankVolumeL > maxTankVolumeL {
		if g.TankVolumeL != 0 {
			add("gas.tank-volume-l=%.2f 超出 [%.0f,%.0f]，已回退 %.0f", g.TankVolumeL,
				float64(minTankVolumeL), float64(maxTankVolumeL), def.TankVolumeL)
		}
		g.TankVolumeL = def.TankVolumeL
	}

	// 油价：按油号目录补齐缺失项，剔除未知油号，并把单价规范到两位小数
	known := make(map[string]float64, len(def.Prices))
	for _, p := range def.Prices {
		known[p.Code] = p.Price
	}
	prices := make([]FuelPrice, 0, len(def.Prices))
	seen := make(map[string]bool, len(def.Prices))
	for _, p := range g.Prices {
		p.Code = strings.TrimSpace(p.Code)
		if _, ok := known[p.Code]; !ok {
			add("gas.prices 中油号 %s 不在目录中，已忽略", p.Code)
			continue
		}
		if seen[p.Code] {
			continue
		}
		if p.Price < minFuelPrice || p.Price > maxFuelPrice {
			add("gas.prices[%s]=%.2f 超出 [%.2f,%.2f]，已回退 %.2f", p.Code, p.Price,
				minFuelPrice, maxFuelPrice, known[p.Code])
			p.Price = known[p.Code]
		}
		p.Price = round2(p.Price)
		seen[p.Code] = true
		prices = append(prices, p)
	}
	for _, p := range def.Prices {
		if !seen[p.Code] {
			prices = append(prices, p)
			seen[p.Code] = true
		}
	}
	g.Prices = prices

	reset(add, "gas.push-interval-sec", &g.PushIntervalSec, minPushIntervalSec, maxPushIntervalSec, def.PushIntervalSec)

	units := make([]DispenserConfig, 0, len(g.Dispensers))
	for i, d := range g.Dispensers {
		d.DispenserNo = strings.TrimSpace(d.DispenserNo)
		if d.DispenserNo == "" {
			add("gas.dispensers[%d] 缺少油机号，已忽略该油机", i)
			continue
		}
		if d.ID == "" {
			d.ID = fmt.Sprintf("dispenser-%d", i+1)
		}
		if d.NozzleCount < minNozzleCount || d.NozzleCount > maxNozzleCount {
			add("gas.dispensers[%d] 枪数 %d 超出 [%d,%d]，已回退 %d", i, d.NozzleCount,
				minNozzleCount, maxNozzleCount, defaultNozzleCount)
			d.NozzleCount = defaultNozzleCount
		}
		d.Nozzles = normalizeNozzles(d.NozzleCount, d.Nozzles, add)
		units = append(units, d)
	}
	// 旧配置完全没有油机时按默认台数 / 枪数生成，保证界面可直接操作
	if len(units) == 0 {
		units = GenerateDispensers(defaultDispenserCount, defaultNozzleCount)
		add("gas.dispensers 为空，已按 %d 台 × %d 枪自动生成", defaultDispenserCount, defaultNozzleCount)
	}
	g.Dispensers = units

	if g.LastStartFuel == (StartFuelForm{}) {
		g.LastStartFuel = DefaultStartFuel()
	} else {
		normalizeStartFuel(&g.LastStartFuel)
	}

	return notes
}

// NormalizeNozzles 保证每台油机的枪号连续（1 ~ x）且油号合法，供上层写入前规整
func NormalizeNozzles(count int, list []NozzleConfig) []NozzleConfig {
	return normalizeNozzles(count, list, func(string, ...any) {})
}

// normalizeNozzles 保证每台油机的枪号连续（1 ~ x）且油号合法
func normalizeNozzles(count int, list []NozzleConfig, add func(string, ...any)) []NozzleConfig {
	out := make([]NozzleConfig, 0, count)
	for i := 0; i < count; i++ {
		n := NozzleConfig{No: strconv.Itoa(i + 1), FuelCode: FuelDefaultCode}
		if i < len(list) {
			code := strings.TrimSpace(list[i].FuelCode)
			if code != "" {
				if _, ok := FuelByCode(code); ok {
					n.FuelCode = code
				} else {
					add("油号 %s 不在目录中，已回退 %s", code, FuelDefaultCode)
				}
			}
		}
		out = append(out, n)
	}
	return out
}

// normalizeStartFuel 修补加油表单取值，字符串（卡号 / 车牌）允许为空
func normalizeStartFuel(f *StartFuelForm) {
	def := DefaultStartFuel()
	switch f.Target.Mode {
	case FuelTargetFree, FuelTargetAmount, FuelTargetVolume:
	default:
		f.Target.Mode = def.Target.Mode
	}
	if f.Target.Amount < 0 {
		f.Target.Amount = def.Target.Amount
	}
	if f.Target.Volume < 0 {
		f.Target.Volume = def.Target.Volume
	}
}

// reset 取值越界时回退默认值，并记录修正说明
func reset(add func(string, ...any), key string, v *int, min, max, def int) {
	if *v < min || *v > max {
		add("%s=%d 超出合理区间 [%d,%d]，已回退 %d", key, *v, min, max, def)
		*v = def
	}
}

// round2 保留两位小数（油价 / 金额）
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// normalizeStartCharge 修补旧配置缺失或异常的表单取值，保证其始终可安全参与充电计算。
// 字符串字段（卡号 / VIN）允许用户清空并保持为空，不做强制回填。
func normalizeStartCharge(f *StartChargeForm) {
	def := DefaultStartCharge()
	if f.StartType != 1 && f.StartType != 3 {
		f.StartType = def.StartType
	}
	if f.BatteryKwh <= 0 {
		f.BatteryKwh = def.BatteryKwh
	}
	if f.StartSoc < 0 || f.StartSoc > 100 {
		f.StartSoc = def.StartSoc
	}
	if f.MaxVoltage <= 0 {
		f.MaxVoltage = def.MaxVoltage
	}
	if f.MaxCurrent <= 0 {
		f.MaxCurrent = def.MaxCurrent
	}
	if f.Target.Mode == "" {
		f.Target.Mode = defaultTargetMode
	}
}
