package sim

// Stage 桩的阶段状态。
type Stage string

const (
	StageDisconnected Stage = "Disconnected" // 未连接，等待重连
	StageConnecting   Stage = "Connecting"   // 正在建立 TCP 连接
	StageHandshaking  Stage = "Handshaking"  // 登录 / 对时 / 计费模型获取
	StageIdle         Stage = "Idle"         // 空闲，周期心跳
	StageStarting     Stage = "Starting"     // 启机回复 + 握手 + 参数配置
	StageCharging     Stage = "Charging"     // 充电中，周期上报
	StageStopping     Stage = "Stopping"     // 中止中
	StageSettling     Stage = "Settling"     // 交易记录上送中
	StageStopped      Stage = "Stopped"      // HTTP 已停止
)

// CmdKind 控制指令类型。
type CmdKind int

const (
	CmdDeviceStart CmdKind = iota // 启动桩
	CmdDeviceStop                 // 停止桩
	CmdGunStart                   // 手动触发启机
	CmdGunStop                    // 手动触发终止
)

// Command HTTP 控制面下发给桩 Actor 的指令。
type Command struct {
	Kind   CmdKind
	Gun    byte
	Reason string
	Reply  chan error
}

// GunStatus 单枪状态快照。
type GunStatus struct {
	GunNo      int     `json:"gunNo"`
	Charging   bool    `json:"charging"`
	TradeNo    string  `json:"tradeNo"`
	StartAt    string  `json:"startAt"`
	ElapsedSec int     `json:"elapsedSec"`
	Soc        int     `json:"soc"`
	VoltageV   float64 `json:"voltageV"`
	CurrentA   float64 `json:"currentA"`
	EnergyKwh  float64 `json:"energyKwh"`
	AmountYuan float64 `json:"amountYuan"`
	StoppedBy  string  `json:"stoppedBy"`
}

// DeviceStatus 单桩状态快照。
type DeviceStatus struct {
	DeviceNo     string      `json:"deviceNo"`
	GunCount     int         `json:"gunCount"`
	Running      bool        `json:"running"`
	Connected    bool        `json:"connected"`
	Stage        string      `json:"stage"`
	Server       string      `json:"server"`
	Protocol     string      `json:"protocol"`
	BillingModel string      `json:"billingModel"`
	Guns         []GunStatus `json:"guns"`
}

// ErrNotRunning 桩未启动。
var ErrNotRunning = errorf("桩未启动")

func errorf(msg string) error { return &simError{msg} }

type simError struct{ msg string }

func (e *simError) Error() string { return e.msg }
