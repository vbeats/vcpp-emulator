package emu

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
	"vcpp-emulator/internal/ykc"
)

// TestSmokeServer 对真实服务端做一次握手冒烟：登录 → 心跳 → 计费模型 → 申请启动。
// 默认跳过，需显式指定目标服务端：
//
//	VCPP_SMOKE_HOST=10.88.0.81 VCPP_SMOKE_PORT=9847 go test ./internal/emu -run TestSmokeServer -v
func TestSmokeServer(t *testing.T) {
	host := os.Getenv("VCPP_SMOKE_HOST")
	if host == "" {
		t.Skip("未配置 VCPP_SMOKE_HOST，跳过联网冒烟测试")
	}
	port := 9847
	if v := os.Getenv("VCPP_SMOKE_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	pileNo := os.Getenv("VCPP_SMOKE_PILE")
	if pileNo == "" {
		pileNo = "20260501000001"
	}

	spec := ykc.SpecByPort(port)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("连接 %s 失败: %v", addr, err)
	}
	defer conn.Close()

	reader := ykc.NewFrameReader(spec)
	var seq uint16

	send := func(cmd byte, body []byte) {
		seq++
		raw := spec.Encode(seq, cmd, body, time.Now())
		if _, err := conn.Write(raw); err != nil {
			t.Fatalf("发送 0x%02X 失败: %v", cmd, err)
		}
		t.Logf("TX 0x%02X %s | %s", cmd, ykc.CmdDesc(cmd), ykc.ToHex(raw))
	}

	expect := func(want byte, timeout time.Duration) ykc.Frame {
		deadline := time.Now().Add(timeout)
		buf := make([]byte, 4096)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			n, err := conn.Read(buf)
			if n > 0 {
				raw := ykc.ToHex(buf[:n])
				frames, ferr := reader.Feed(buf[:n])
				if ferr != nil {
					t.Logf("拆帧异常: %v | raw=%s", ferr, raw)
				}
				if len(frames) == 0 {
					t.Logf("RAW RX(未成帧): %s", raw)
				}
				for _, f := range frames {
					t.Logf("RX 0x%02X %s | body=%s", f.Cmd, ykc.CmdDesc(f.Cmd), ykc.ToHex(f.Body))
					if f.Cmd == want {
						return f
					}
				}
			}
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					if time.Now().After(deadline) {
						t.Fatalf("等待 0x%02X 超时", want)
					}
					continue
				}
				t.Fatalf("读取失败: %v", err)
			}
			if time.Now().After(deadline) {
				t.Fatalf("等待 0x%02X 超时", want)
			}
		}
	}

	// 1. 登录
	send(ykc.CmdLogin, spec.BuildLogin(ykc.LoginInfo{
		PileNo: pileNo, PileType: 0, GunCount: 2, ProgramVer: "220422",
		NetType: 1, Sim: "89860123456789012345", Supplier: 4, Token: pileNo, Phone: "13800138000",
	}))
	ack := expect(ykc.CmdLoginAck, 10*time.Second)
	login := ykc.ParseLoginAck(ack.Body)
	if login.Result != 0x00 {
		t.Fatalf("登录失败，结果码 0x%02X", login.Result)
	}
	t.Logf("登录成功，平台时间 %s", login.Time.Format("2006-01-02 15:04:05"))

	// 2. 心跳（服务端会下发对时设置，需应答）
	send(ykc.CmdHeartbeat, ykc.BuildHeartbeat(pileNo, 1, 0x00))
	expect(ykc.CmdHeartbeatAck, 5*time.Second)

	// 3. 计费模型
	send(ykc.CmdBillingVerify, ykc.BuildBillingVerify(pileNo, 0x0100))
	verify := expect(ykc.CmdBillingVerifyAck, 5*time.Second)
	modelNo, result := ykc.ParseBillingVerifyAck(verify.Body)
	t.Logf("计费模型验证：编号 0x%04X 结果 %d", modelNo, result)
	send(ykc.CmdBillingModel, spec.BuildBillingRequest(pileNo, 0x0100))
	model := spec.ParseBillingModel(expect(ykc.CmdBillingModelAck, 5*time.Second).Body)
	t.Logf("计费模型：%s", model.Describe())

	// 4. 申请启动充电
	tradeNo := fmt.Sprintf("%s01%s0001", pileNo, time.Now().Format("060102150405"))
	send(spec.ApplyCmd, ykc.BuildApplyCharge(ykc.ApplyCharge{
		TradeNo: tradeNo, PileNo: pileNo, GunNo: 1, StartType: ykc.StartTypeCard, CardNo: "88888888",
	}))
	apply := ykc.ParseApplyChargeAck(expect(spec.ApplyAckCmd, 10*time.Second).Body)
	t.Logf("启动确认：流水号 %s 鉴权标志 0x%02X 失败原因 %d", apply.TradeNo, apply.AuthFlag, apply.FailReason)
	if apply.TradeNo != tradeNo {
		t.Fatalf("流水号不匹配：期望 %s 实际 %s", tradeNo, apply.TradeNo)
	}

	// 5. 实时数据
	send(ykc.CmdRealTimeData, spec.BuildRealTimeData(ykc.RealTimeData{
		TradeNo:        tradeNo,
		PileNo:         pileNo,
		GunNo:          1,
		Status:         ykc.GunStatusCharging,
		GunHome:        ykc.GunHomeUnkown,
		Plugged:        0x01,
		Voltage:        377.1,
		Current:        28.5,
		GunTemp:        17,
		Soc:            56,
		BatteryMaxTemp: 37,
		ChargeMinutes:  12,
		RemainMinutes:  18,
		Energy:         12.3456,
		LossEnergy:     12.3456,
		Amount:         18.5184,
		PileTemp:       30,
		Smoke:          0x02,
		MeterValue:     1024.5,
	}))

	// 6. 交易记录并等待平台确认
	send(spec.TradeCmd, spec.BuildTrade(ykc.TradeRecord{
		TradeNo:     tradeNo,
		PileNo:      pileNo,
		GunNo:       1,
		StartTime:   time.Now().Add(-20 * time.Minute),
		EndTime:     time.Now(),
		MeterNo:     "20260501",
		Rates: []ykc.RateAmount{
			{Price: 1.6, Energy: 6, LossEnergy: 6, Amount: 9.6},
			{Price: 1.4, Energy: 6.3456, LossEnergy: 6.3456, Amount: 8.8838},
			{Price: 1.2},
			{Price: 0.9},
		},
		MeterStart:  1000,
		MeterEnd:    1012.3456,
		TotalEnergy: 12.3456,
		LossEnergy:  12.3456,
		Amount:      18.5184,
		TradeType:   ykc.TradeTypeCard,
		TradeTime:   time.Now(),
		StopReason:  ykc.StopReasonAmount,
		CardNo:      88888888,
		HalfHourKwh: make([]float64, 48),
	}))
	tradeAck := expect(ykc.CmdTradeAck, 10*time.Second)
	no, result := ykc.ParseTradeAck(tradeAck.Body)
	t.Logf("交易记录确认：流水号 %s 结果 %d", no, result)
	if no != tradeNo {
		t.Fatalf("交易流水号不匹配：期望 %s 实际 %s", tradeNo, no)
	}
}

// TestFullChargeFlow 验证完整充电流程状态机：连接 → 登录 → 启动充电 → 实时数据 → 达标停止 → 交易记录确认。
// 默认跳过，需显式指定目标服务端：
//
//	VCPP_SMOKE_HOST=10.88.0.81 VCPP_SMOKE_PORT=9847 go test ./internal/emu -run TestFullChargeFlow -v
func TestFullChargeFlow(t *testing.T) {
	host := os.Getenv("VCPP_SMOKE_HOST")
	if host == "" {
		t.Skip("未配置 VCPP_SMOKE_HOST，跳过联网全流程测试")
	}
	port := 9847
	if v := os.Getenv("VCPP_SMOKE_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}

	logBus, err := logger.New(t.TempDir())
	if err != nil {
		t.Fatalf("日志初始化失败: %v", err)
	}
	defer logBus.Close()

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("配置初始化失败: %v", err)
	}
	cfg := store.Default()
	cfg.Server = store.ServerConfig{Host: host, Port: port, Version: string(ykc.SpecByPort(port).Code)}
	cfg.Piles = []store.PileConfig{{
		ID: "pile-flow", PileNo: "20260501000002", GunCount: 1, PileType: 0,
		RatedPower: 120, ProgramVer: "220422", Enabled: true,
	}}
	cfg.HeartbeatSec = 10
	cfg.RealTimeChargeSec = 5
	if err := st.Save(cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	mgr := NewManager(logBus, st, nil)
	defer mgr.Shutdown()
	mgr.Apply(st.Get())
	// 默认不自动连接，这里显式发起手动连接后再观察流程
	if err := mgr.Start("pile-flow"); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	waitFor := func(cond func(PileView) bool, timeout time.Duration, desc string) PileView {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			list := mgr.Snapshot()
			if len(list) > 0 && cond(list[0]) {
				return list[0]
			}
			time.Sleep(200 * time.Millisecond)
		}
		recent := logBus.Recent()
		start := 0
		if len(recent) > 15 {
			start = len(recent) - 15
		}
		for _, e := range recent[start:] {
			t.Logf("[超时诊断] %s %s %s %s", e.Time, e.Dir, e.Cmd, e.Message)
		}
		t.Fatalf("等待超时：%s", desc)
		return PileView{}
	}

	// 1. 等待登录在线并获取计费模型
	online := waitFor(func(p PileView) bool {
		return p.Status == StatusOnline && p.LoggedIn && p.Billing != ""
	}, 20*time.Second, "桩登录在线并获取计费模型")
	t.Logf("桩 %s 已在线：%s", online.PileNo, online.Billing)

	// 2. 发起充电（目标时长 0.2 分钟，约 12 秒后自动停止）
	if err := mgr.StartCharge(StartRequest{
		PileID:     "pile-flow",
		GunNo:      1,
		StartType:  ykc.StartTypeCard,
		CardNo:     "88888888",
		Target:     ChargeTarget{Mode: TargetTime, Minute: 0.2},
		BatteryKwh: 60,
		StartSoc:   20,
	}); err != nil {
		t.Fatalf("启动充电失败: %v", err)
	}

	charging := waitFor(func(p PileView) bool {
		return len(p.Guns) > 0 && p.Guns[0].Status == GunCharging
	}, 20*time.Second, "进入充电状态")
	t.Logf("枪 1 进入充电：流水号 %s", charging.Guns[0].TradeNo)

	// 3. 等待达到目标自动停止（结算快照在会话释放后仍可读取）
	stopped := waitFor(func(p PileView) bool {
		return len(p.Guns) > 0 && p.Guns[0].Status != GunCharging && p.Guns[0].StopReason != ""
	}, 40*time.Second, "达到目标自动停止")
	t.Logf("充电结束：状态 %s，停止原因 %s，电量 %.4f kWh，金额 %.2f 元，SOC %d%%，时长 %d 分钟",
		stopped.Guns[0].Status, stopped.Guns[0].StopReason,
		stopped.Guns[0].Energy, stopped.Guns[0].Amount, stopped.Guns[0].Soc, stopped.Guns[0].Minutes)

	if stopped.Guns[0].Energy <= 0 {
		t.Fatalf("充电电量应大于 0，实际 %.4f", stopped.Guns[0].Energy)
	}
	if stopped.Guns[0].Amount <= 0 {
		t.Fatalf("充电金额应大于 0，实际 %.2f", stopped.Guns[0].Amount)
	}
	if stopped.Guns[0].Soc < 20 {
		t.Fatalf("SOC 不应低于起始值，实际 %d", stopped.Guns[0].Soc)
	}
	if stopped.Guns[0].StopReason == "" {
		t.Fatalf("缺少停止原因")
	}

	// 4. 等待平台确认交易记录后会话释放
	settled := waitFor(func(p PileView) bool {
		return len(p.Guns) > 0 && p.Guns[0].Status != GunCharging && p.Guns[0].Status != GunStopping
	}, 30*time.Second, "平台确认交易记录")
	t.Logf("会话已释放：状态 %s", settled.Guns[0].Status)

	// 4. 校验日志中出现了交易记录上送与平台确认
	var sawTrade, sawAck bool
	for _, e := range logBus.Recent() {
		if strings.Contains(e.Message, "交易记录") && e.Dir == "TX" {
			sawTrade = true
		}
		if strings.Contains(e.Message, "交易记录确认") && e.Dir == "RX" {
			sawAck = true
		}
	}
	if !sawTrade {
		t.Fatalf("未上送交易记录")
	}
	if !sawAck {
		t.Fatalf("未收到平台交易记录确认")
	}
}
