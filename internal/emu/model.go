// Package emu 实现云快充充电桩设备模拟器：
// 单桩单 TCP 连接、多枪独立状态机，完整模拟登录/心跳/计费模型/启停/实时数据/交易记录流程。
package emu

// 桩连接状态
const (
	StatusOffline    = "offline"    // 未连接
	StatusConnecting = "connecting" // 连接中
	StatusOnline     = "online"     // 已登录在线
	StatusError      = "error"      // 异常
)

// 枪状态
const (
	GunIdle     = "idle"     // 空闲
	GunPlugged  = "plugged"  // 已插枪
	GunStarting = "starting" // 启动中（等待平台确认）
	GunCharging = "charging" // 充电中
	GunStopping = "stopping" // 停止中（等待交易记录确认）
	GunFault    = "fault"    // 故障
)

// 充电目标模式
const (
	TargetAmount = "amount" // 按金额
	TargetTime   = "time"   // 按时长
	TargetEnergy = "energy" // 按电量
	TargetSoc    = "soc"    // 按 SOC
)

// ChargeTarget 充电目标
type ChargeTarget struct {
	Mode   string  `json:"mode"`
	Amount float64 `json:"amount"` // 元
	Minute float64 `json:"minute"` // 分钟
	Energy float64 `json:"energy"` // kWh
	Soc    int     `json:"soc"`    // %
}

// GunView 前端展示用枪状态
type GunView struct {
	No            int     `json:"no"`
	Status        string  `json:"status"`
	StatusCode    byte    `json:"statusCode"`
	Plugged       bool    `json:"plugged"`
	Voltage       float64 `json:"voltage"`
	Current       float64 `json:"current"`
	Power         float64 `json:"power"`
	Soc           int     `json:"soc"`
	Energy        float64 `json:"energy"`
	Amount        float64 `json:"amount"`
	Minutes       int     `json:"minutes"`
	RemainMinutes int     `json:"remainMinutes"`
	GunTemp       int     `json:"gunTemp"`
	BatteryTemp   int     `json:"batteryTemp"`
	TradeNo       string  `json:"tradeNo"`
	StartType     string  `json:"startType"`
	CardNo        string  `json:"cardNo"`
	Vin           string  `json:"vin"`
	Balance       float64 `json:"balance"`
	Progress      float64 `json:"progress"` // 0~1
	Target        ChargeTarget `json:"target"`
	StopReason    string  `json:"stopReason"`
	WaitSeconds   int     `json:"waitSeconds"` // 等待平台确认剩余秒数
	MaxVolts      float64 `json:"maxVolts"`    // 电池最高允许电压
	MaxAmps       float64 `json:"maxAmps"`     // 电池最高允许电流
}

// PileView 前端展示用桩状态
type PileView struct {
	ID         string    `json:"id"`
	PileNo     string    `json:"pileNo"`
	Version    string    `json:"version"`
	VersionDesc string   `json:"versionDesc"`
	Addr       string    `json:"addr"`
	Status     string    `json:"status"`
	GunCount   int       `json:"gunCount"`
	PileType   string    `json:"pileType"`
	RatedPower float64   `json:"ratedPower"`
	ProgramVer string    `json:"programVer"`
	Enabled    bool      `json:"enabled"`
	LoggedIn   bool      `json:"loggedIn"`
	LastError  string    `json:"lastError"`
	SentFrames int64     `json:"sentFrames"`
	RecvFrames int64     `json:"recvFrames"`
	Billing    string    `json:"billing"`
	ConnectedAt string   `json:"connectedAt"`
	Guns       []GunView `json:"guns"`
}

// StartRequest 启动充电请求
type StartRequest struct {
	PileID   string  `json:"pileId"`
	GunNo    int     `json:"gunNo"`
	StartType byte   `json:"startType"` // 1 刷卡 3 VIN
	CardNo   string  `json:"cardNo"`
	Vin      string  `json:"vin"`
	Target   ChargeTarget `json:"target"`
	BatteryKwh float64 `json:"batteryKwh"`
	StartSoc int      `json:"startSoc"`
	MaxVoltage float64 `json:"maxVoltage"`
	MaxCurrent float64 `json:"maxCurrent"`
}
