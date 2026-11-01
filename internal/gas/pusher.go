package gas

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vcpp-emulator/internal/store"
)

// 推送数据类型
const (
	PushKindProgress = "progress" // 加油过程进度
	PushKindOrder    = "order"    // 加油完成订单
)

// 推送通道标识
const (
	ChannelNone = "none"
	ChannelMqtt = "mqtt"
	ChannelHTTP = "http"
)

// ErrNotImplemented 表示推送通道的配置与报文已就绪，但尚未接入实际网络发送。
// 当前版本只把待推送内容交给界面展示（并记录到运行日志），不做真实外发。
var ErrNotImplemented = errors.New("推送通道尚未实现实际网络发送")

// PushMessage 一次待推送内容：通道、目标（topic 或 URL）与 JSON 报文。
// 既作为推送实现的入参，也用于界面展示最近的推送记录。
type PushMessage struct {
	ID      int64  `json:"id"`      // 自增序号，前端排序 / 去重
	Time    string `json:"time"`    // HH:MM:SS
	Kind    string `json:"kind"`    // progress / order
	Channel string `json:"channel"` // none / mqtt / http
	Target  string `json:"target"`  // topic 或完整 URL
	Payload string `json:"payload"` // JSON 文本
}

// Pusher 数据推送通道抽象。
// 实现方负责把加油进度 / 加油订单投递到配置的目标；
// 当前内置实现仅落界面与日志（见 ErrNotImplemented）。
type Pusher interface {
	// Channel 通道标识：none / mqtt / http
	Channel() string
	// Describe 通道的可读描述，供界面展示当前推送目标
	Describe() string
	// PushProgress 推送加油过程实时进度
	PushProgress(p FuelProgress) error
	// PushOrder 推送加油完成订单数据
	PushOrder(o FuelOrder) error
}

// NewPusher 按配置创建推送通道，emit 用于把待推送内容回传给界面。
func NewPusher(cfg store.GasConfig, emit func(PushMessage)) Pusher {
	if emit == nil {
		emit = func(PushMessage) {}
	}
	switch cfg.PushMode {
	case store.PushModeMqtt:
		return &mqttPusher{cfg: cfg.Mqtt, emit: emit}
	case store.PushModeHTTP:
		return &httpPusher{cfg: cfg.HTTP, emit: emit}
	default:
		return &nonePusher{}
	}
}

// nonePusher 未启用推送
type nonePusher struct{}

func (p *nonePusher) Channel() string  { return ChannelNone }
func (p *nonePusher) Describe() string { return "未启用数据推送" }
func (p *nonePusher) PushProgress(FuelProgress) error {
	return nil
}
func (p *nonePusher) PushOrder(FuelOrder) error { return nil }

// mqttPusher MQTT 推送通道。
// topic 由配置给出，支持 {dispenser} / {nozzle} 占位符；
// 实际 MQTT 连接与 publish 待后续版本接入客户端后补齐。
type mqttPusher struct {
	cfg  store.MqttConfig
	emit func(PushMessage)
}

func (p *mqttPusher) Channel() string {
	return ChannelMqtt
}

func (p *mqttPusher) Describe() string {
	return fmt.Sprintf("MQTT %s:%d · 进度 %s · 订单 %s",
		p.cfg.Broker, p.cfg.Port, p.cfg.TopicProgress, p.cfg.TopicOrder)
}

func (p *mqttPusher) PushProgress(v FuelProgress) error {
	return p.push(PushKindProgress, p.cfg.TopicProgress, v)
}

func (p *mqttPusher) PushOrder(o FuelOrder) error {
	return p.push(PushKindOrder, p.cfg.TopicOrder, o)
}

func (p *mqttPusher) push(kind, topic string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化推送报文失败: %w", err)
	}
	p.emit(PushMessage{Kind: kind, Channel: ChannelMqtt, Target: mqttTopic(topic, body), Payload: string(payload)})
	return ErrNotImplemented
}

// mqttTopic 把 topic 模板中的 {dispenser} / {nozzle} 替换为实际编号
func mqttTopic(tpl string, body any) string {
	dispenser, nozzle := "-", "-"
	switch v := body.(type) {
	case FuelProgress:
		dispenser, nozzle = v.DispenserNo, v.NozzleNo
	case *FuelProgress:
		dispenser, nozzle = v.DispenserNo, v.NozzleNo
	case FuelOrder:
		dispenser, nozzle = v.DispenserNo, v.NozzleNo
	case *FuelOrder:
		dispenser, nozzle = v.DispenserNo, v.NozzleNo
	}
	out := strings.ReplaceAll(tpl, "{dispenser}", dispenser)
	return strings.ReplaceAll(out, "{nozzle}", nozzle)
}

// httpPusher HTTP POST JSON 推送通道。
// 目标地址为 base-url + 路径；实际请求待后续版本接入 http client 后补齐。
type httpPusher struct {
	cfg  store.HTTPPushConfig
	emit func(PushMessage)
}

func (p *httpPusher) Channel() string { return ChannelHTTP }

func (p *httpPusher) Describe() string {
	return fmt.Sprintf("HTTP %s · 进度 %s · 订单 %s",
		strings.TrimRight(p.cfg.BaseURL, "/"), p.cfg.PathProgress, p.cfg.PathOrder)
}

func (p *httpPusher) PushProgress(v FuelProgress) error {
	return p.push(PushKindProgress, p.cfg.PathProgress, v)
}

func (p *httpPusher) PushOrder(o FuelOrder) error {
	return p.push(PushKindOrder, p.cfg.PathOrder, o)
}

func (p *httpPusher) push(kind, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化推送报文失败: %w", err)
	}
	p.emit(PushMessage{Kind: kind, Channel: ChannelHTTP, Target: joinURL(p.cfg.BaseURL, path), Payload: string(payload)})
	return ErrNotImplemented
}

// joinURL 拼接基址与路径，路径为空时返回基址
func joinURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.TrimSpace(path)
	if base == "" {
		return path
	}
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
