// Package gas 实现加油机（Dispenser）设备模拟器：
// 单进程模拟多台加油机、每台多把油枪，各自维护「提枪 → 加油 → 挂枪结算」状态机；
// 加油过程与加油订单经 Pusher 抽象按配置以 MQTT / HTTP POST JSON 形式对外推送。
package gas

// 加油机状态
const (
	StatusOffline = "offline" // 已停止（仅退出或配置移除期间短暂出现）
	StatusIdle    = "idle"    // 待机（可提枪加油）
	StatusFueling = "fueling" // 有枪正在加油
	StatusFault   = "fault"   // 故障
)

// 油枪状态
const (
	NozzleIdle    = "idle"    // 挂枪待机
	NozzleLifted  = "lifted"  // 已提枪，等待加油
	NozzleFueling = "fueling" // 加油中
	NozzleSettled = "settled" // 已结算，等待挂枪
	NozzleFault   = "fault"   // 故障
)

// 加油目标模式
const (
	TargetFree   = "free"   // 加满为止（手动停止或跳枪）
	TargetAmount = "amount" // 定额：达到指定金额自动停止
	TargetVolume = "volume" // 定量：达到指定升数自动停止
)

// 加油停止原因
const (
	StopManual    = "手动停止"
	StopAmount    = "已到达定额金额"
	StopVolume    = "已到达定量升数"
	StopTankFull  = "油箱已加满（跳枪）"
	StopPowerOff  = "油机停用，加油中断"
)

// 加油订单状态
const (
	OrderFueling = "fueling" // 加油中
	OrderSettled = "settled" // 已结算
)

// FuelTarget 加油目标
type FuelTarget struct {
	Mode   string  `json:"mode"`   // free / amount / volume
	Amount float64 `json:"amount"` // 定额金额（元）
	Volume float64 `json:"volume"` // 定量升数（L）
}

// StartFuelRequest 开始加油请求
type StartFuelRequest struct {
	DispenserID string     `json:"dispenserId"`
	NozzleNo    string     `json:"nozzleNo"`
	Target      FuelTarget `json:"target"`
	CardNo      string     `json:"cardNo"`
	PlateNo     string     `json:"plateNo"`
}

// FuelProgress 加油过程实时进度，推送字段与订单保持一致：
// 枪号 / 升数（4 位小数）/ 金额（2 位小数）/ 泵码（4 位小数）/ 单价（2 位小数）。
type FuelProgress struct {
	DispenserNo string  `json:"dispenserNo"`
	NozzleNo    string  `json:"nozzleNo"`
	FuelCode    string  `json:"fuelCode"`
	FuelName    string  `json:"fuelName"`
	FuelColor   string  `json:"fuelColor"`
	Volume      float64 `json:"volume"`   // 升，4 位小数
	Amount      float64 `json:"amount"`   // 元，2 位小数
	PumpStart   float64 `json:"pumpStart"` // 起始泵码，4 位小数
	PumpCode    float64 `json:"pumpCode"`  // 当前泵码，4 位小数
	Price       float64 `json:"price"`     // 单价，2 位小数
	Seconds     int     `json:"seconds"`   // 已加油时长（秒）
	Progress    float64 `json:"progress"`   // 定额进度 0~1
	Status      string  `json:"status"`
	Time        string  `json:"time"` // HH:MM:SS
}

// FuelOrder 加油订单：加油完成（挂枪结算）后推送的完整订单数据
type FuelOrder struct {
	OrderNo     string  `json:"orderNo"`     // 订单号
	DispenserNo string  `json:"dispenserNo"` // 油机号
	NozzleNo    string  `json:"nozzleNo"`    // 枪号
	FuelCode    string  `json:"fuelCode"`    // 油号
	FuelName    string  `json:"fuelName"`    // 油品名
	FuelColor   string  `json:"fuelColor"`   // 油品色
	Volume      float64 `json:"volume"`      // 升数，4 位小数
	Amount      float64 `json:"amount"`      // 金额，2 位小数
	Price       float64 `json:"price"`       // 单价，2 位小数
	PumpStart   float64 `json:"pumpStart"`   // 起始泵码，4 位小数
	PumpEnd     float64 `json:"pumpEnd"`     // 结束泵码，4 位小数
	Seconds     int     `json:"seconds"`     // 加油时长（秒）
	Progress    float64 `json:"progress"`    // 目标完成度 0~1
	StartAt     string  `json:"startAt"`     // HH:MM:SS
	EndAt       string  `json:"endAt"`       // HH:MM:SS
	StopReason  string  `json:"stopReason"`  // 停止原因
	Status      string  `json:"status"`      // fueling / settled
	CardNo      string  `json:"cardNo"`
	PlateNo     string  `json:"plateNo"`
}

// NozzleView 前端展示用油枪状态。
// 加油中与加油完成后的订单数据使用同一套字段，便于界面直接展示。
type NozzleView struct {
	No         string     `json:"no"`         // 枪号（字符串，1 ~ x）
	Status     string     `json:"status"`     // 状态码
	StatusText string     `json:"statusText"` // 中文状态
	Lifted     bool       `json:"lifted"`     // 是否已提枪
	FuelCode   string     `json:"fuelCode"`   // 油号
	FuelName   string     `json:"fuelName"`   // 油品名
	FuelColor  string     `json:"fuelColor"`  // 油品区分色
	Price      float64    `json:"price"`      // 单价（元/升）
	Volume     float64    `json:"volume"`     // 升数，4 位小数
	Amount     float64    `json:"amount"`     // 金额，2 位小数
	PumpStart  float64    `json:"pumpStart"`  // 起始泵码，4 位小数
	PumpEnd    float64    `json:"pumpEnd"`    // 当前 / 结束泵码，4 位小数
	FlowRate   float64    `json:"flowRate"`   // 瞬时流速 L/min
	Seconds    int        `json:"seconds"`    // 已加油时长（秒）
	Progress   float64    `json:"progress"`   // 0~1
	Target     FuelTarget `json:"target"`
	OrderNo    string     `json:"orderNo"`
	StopReason string     `json:"stopReason"`
	StartAt    string     `json:"startAt"`
	EndAt      string     `json:"endAt"`
	CardNo     string     `json:"cardNo"`
	PlateNo    string     `json:"plateNo"`
	Settled    bool       `json:"settled"` // 订单是否已结算
}

// DispenserView 前端展示用加油机状态。
// 加油量与金额按枪展示（见 NozzleView），油机层面不做汇总。
type DispenserView struct {
	ID          string       `json:"id"`
	DispenserNo string       `json:"dispenserNo"`
	Status      string       `json:"status"`
	StatusText  string       `json:"statusText"`
	NozzleCount int          `json:"nozzleCount"`
	LastError   string       `json:"lastError"`
	UpdatedAt   string       `json:"updatedAt"`
	Nozzles     []NozzleView `json:"nozzles"`
}
