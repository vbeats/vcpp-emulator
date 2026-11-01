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
	"os"
	"path/filepath"
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

// AppConfig 应用配置，对应单个 YAML 配置文件
type AppConfig struct {
	Server            ServerConfig    `json:"server" yaml:"server"`
	Piles             []PileConfig    `json:"piles" yaml:"piles"`
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

	return notes
}

// reset 取值越界时回退默认值，并记录修正说明
func reset(add func(string, ...any), key string, v *int, min, max, def int) {
	if *v < min || *v > max {
		add("%s=%d 超出合理区间 [%d,%d]，已回退 %d", key, *v, min, max, def)
		*v = def
	}
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
