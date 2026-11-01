package gas

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
)

// newTestStore 构造写入临时目录的配置存储
func newTestStore(t *testing.T, cfg store.AppConfig) *store.Store {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return st
}

// newTestDispenser 构造一台测试用油机：单枪、92# 汽油、单价 8.00 元/升
func newTestDispenser(t *testing.T, tankVolumeL float64) *Dispenser {
	t.Helper()
	logBus, err := logger.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logBus.Close)

	gas := store.DefaultGas()
	gas.Prices = []store.FuelPrice{{Code: "92#", Price: 8}}
	gas.TankVolumeL = tankVolumeL
	gas.PushIntervalSec = 1

	cfg := store.DispenserConfig{
		ID:          "dispenser-1",
		DispenserNo: "01",
		NozzleCount: 1,
		Nozzles:     []store.NozzleConfig{{No: "1", FuelCode: "92#"}},
	}
	return NewDispenser(cfg, gas, logBus, func() {}, nil)
}

// runToStop 以 1 秒步长推进加油过程，返回停止原因
func runToStop(d *Dispenser, n *nozzle) string {
	now := time.Now()
	for i := 0; i < 1000; i++ {
		now = now.Add(time.Second)
		if reason := d.advance(n, 1, now); reason != "" {
			return reason
		}
	}
	return ""
}

// TestQuotaFuelingStopsAtAmount 定额加油：累计金额达到目标后自动停止
func TestQuotaFuelingStopsAtAmount(t *testing.T) {
	d := newTestDispenser(t, 60)
	n := d.nozzles[0]

	d.startFueling(n, StartFuelRequest{
		DispenserID: d.cfg.ID,
		NozzleNo:    "1",
		Target:      FuelTarget{Mode: TargetAmount, Amount: 40},
		CardNo:      "88888888",
		PlateNo:     "京A12345",
	})
	if n.status != NozzleFueling {
		t.Fatalf("开始加油后状态应为 fueling: %s", n.status)
	}

	reason := runToStop(d, n)
	if reason != StopAmount {
		t.Fatalf("定额加油停止原因错误: %q", reason)
	}
	d.settle(n, reason)

	o := n.order
	if math.Abs(o.Volume-5) > 1e-4 {
		t.Fatalf("定额 40 元 / 8 元每升应加 5 L，实际 %.4f L", o.Volume)
	}
	if math.Abs(o.Amount-40) > 1e-2 {
		t.Fatalf("定额金额应为 40.00 元，实际 %.2f 元", o.Amount)
	}
	if o.Status != OrderSettled || n.status != NozzleSettled {
		t.Fatalf("结算后状态错误: order=%s nozzle=%s", o.Status, n.status)
	}
	if math.Abs(o.PumpEnd-o.PumpStart-o.Volume) > 1e-4 {
		t.Fatalf("泵码差应等于升数: %.4f → %.4f / %.4f L", o.PumpStart, o.PumpEnd, o.Volume)
	}
	if o.OrderNo == "" || o.CardNo != "88888888" || o.PlateNo != "京A12345" {
		t.Fatalf("订单信息缺失: %+v", o)
	}
}

// TestVolumeFuelingStopsAtLiters 定量加油：累计升数达到目标后自动停止
func TestVolumeFuelingStopsAtLiters(t *testing.T) {
	d := newTestDispenser(t, 60)
	n := d.nozzles[0]

	d.startFueling(n, StartFuelRequest{
		DispenserID: d.cfg.ID,
		NozzleNo:    "1",
		Target:      FuelTarget{Mode: TargetVolume, Volume: 12},
	})
	reason := runToStop(d, n)
	if reason != StopVolume {
		t.Fatalf("定量加油停止原因错误: %q", reason)
	}
	d.settle(n, reason)
	if math.Abs(n.order.Volume-12) > 1e-4 {
		t.Fatalf("定量应加 12 L，实际 %.4f L", n.order.Volume)
	}
	if math.Abs(n.order.Amount-96) > 1e-2 {
		t.Fatalf("定量金额应为 96.00 元，实际 %.2f 元", n.order.Amount)
	}
}

// TestFreeFuelingStopsOnTankFull 加满模式：达到油箱容积后跳枪自动停止
func TestFreeFuelingStopsOnTankFull(t *testing.T) {
	d := newTestDispenser(t, 60)
	n := d.nozzles[0]

	d.startFueling(n, StartFuelRequest{
		DispenserID: d.cfg.ID,
		NozzleNo:    "1",
		Target:      FuelTarget{Mode: TargetFree},
	})
	reason := runToStop(d, n)
	if reason != StopTankFull {
		t.Fatalf("加满模式停止原因错误: %q", reason)
	}
	d.settle(n, reason)
	if math.Abs(n.order.Volume-60) > 1e-4 {
		t.Fatalf("加满应加 60 L，实际 %.4f L", n.order.Volume)
	}
	if math.Abs(n.order.PumpStart-n.order.PumpEnd+60) > 1e-4 {
		t.Fatalf("泵码累计错误: %.4f → %.4f", n.order.PumpStart, n.order.PumpEnd)
	}
}

// TestGenerateDispensersNumbering 生成的油机 / 枪号按序排列且类型均为字符串
func TestGenerateDispensersNumbering(t *testing.T) {
	list := store.GenerateDispensers(3, 2)
	if len(list) != 3 {
		t.Fatalf("油机数量错误: %d", len(list))
	}
	for i, d := range list {
		want := fmt.Sprintf("%02d", i+1)
		if d.DispenserNo != want {
			t.Fatalf("油机号应为 %s，实际 %s", want, d.DispenserNo)
		}
		if len(d.Nozzles) != 2 {
			t.Fatalf("油机 %s 枪数错误: %d", d.DispenserNo, len(d.Nozzles))
		}
		for j, n := range d.Nozzles {
			no := strconv.Itoa(j + 1)
			if n.No != no {
				t.Fatalf("枪号应为 %s，实际 %s", no, n.No)
			}
			if _, ok := store.FuelByCode(n.FuelCode); !ok {
				t.Fatalf("油号 %s 不在目录中", n.FuelCode)
			}
		}
	}
}

// TestMqttTopicPlaceholder 推送 topic 中的占位符替换为实际机号 / 枪号
func TestMqttTopicPlaceholder(t *testing.T) {
	cfg := store.DefaultMqtt()
	got := mqttTopic(cfg.TopicProgress, FuelProgress{DispenserNo: "02", NozzleNo: "3"})
	if got != "vcpp/gas/02/nozzle/3/progress" {
		t.Fatalf("topic 占位符替换错误: %s", got)
	}
}

// TestPushOrderRecorded 订单推送后写入历史，供界面展示
func TestPushOrderRecorded(t *testing.T) {
	logBus, err := logger.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logBus.Close()

	cfg := store.Default()
	cfg.Gas = store.DefaultGas()
	m := NewManager(logBus, newTestStore(t, cfg), func() {}, func(PushMessage) {})
	// 油机无启用 / 停用状态：Apply 后即投入运行
	m.Apply(cfg)
	defer m.Shutdown()

	id := cfg.Gas.Dispensers[0].ID
	if err := m.StartFuel(StartFuelRequest{
		DispenserID: id,
		NozzleNo:    "1",
		Target:      FuelTarget{Mode: TargetVolume, Volume: 2},
	}); err != nil {
		t.Fatal(err)
	}
	// 定量 2 L 约需 3 秒，等待自动结算并完成推送
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.Orders()) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := m.Orders()
	if len(got) == 0 {
		t.Fatal("订单历史未记录")
	}
	if math.Abs(got[0].Volume-2) > 1e-4 {
		t.Fatalf("订单升数应为 2 L，实际 %.4f L", got[0].Volume)
	}
	if got[0].StopReason != StopVolume {
		t.Fatalf("停止原因错误: %s", got[0].StopReason)
	}
}

// TestPushChannelTarget mqtt / http 通道分别给出 topic 与完整 URL
func TestPushChannelTarget(t *testing.T) {
	gas := store.DefaultGas()
	mqtt := NewPusher(gas, func(PushMessage) {})
	if mqtt.Channel() != ChannelMqtt {
		t.Fatalf("通道标识错误: %s", mqtt.Channel())
	}

	var mqttMsg PushMessage
	mqtt.PushProgress(FuelProgress{DispenserNo: "01", NozzleNo: "2"})
	mqtt.(*mqttPusher).emit = func(p PushMessage) { mqttMsg = p }
	if err := mqtt.PushProgress(FuelProgress{DispenserNo: "01", NozzleNo: "2"}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("推送通道尚未实装，应返回 ErrNotImplemented，实际 %v", err)
	}
	if mqttMsg.Target != "vcpp/gas/01/nozzle/2/progress" {
		t.Fatalf("MQTT topic 错误: %s", mqttMsg.Target)
	}

	gasHTTP := gas
	gasHTTP.PushMode = store.PushModeHTTP
	httpPush := NewPusher(gasHTTP, func(PushMessage) {})
	if httpPush.Channel() != ChannelHTTP {
		t.Fatalf("通道标识错误: %s", httpPush.Channel())
	}
	var httpMsg PushMessage
	httpPush.(*httpPusher).emit = func(p PushMessage) { httpMsg = p }
	if err := httpPush.PushOrder(FuelOrder{DispenserNo: "01", NozzleNo: "2"}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("推送通道尚未实装，应返回 ErrNotImplemented，实际 %v", err)
	}
	if httpMsg.Target != "http://10.88.0.81:18083/api/gas/fuel/order" {
		t.Fatalf("HTTP 目标地址错误: %s", httpMsg.Target)
	}
	if !strings.Contains(httpMsg.Payload, `"dispenserNo":"01"`) {
		t.Fatalf("HTTP 报文内容错误: %s", httpMsg.Payload)
	}
}
