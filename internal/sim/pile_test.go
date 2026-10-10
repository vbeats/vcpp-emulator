package sim

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/csf/vcpp-emulator/internal/billing"
	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/framelog"
	"github.com/csf/vcpp-emulator/internal/protocol"
	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
	"github.com/csf/vcpp-emulator/internal/transport"
)

const testDeviceNo = "20260501000001"

func testConfig(port int) *config.Config {
	c := &config.Config{}
	c.Server.Ip = "127.0.0.1"
	c.Server.Port = port
	c.Protocol.Type = "ykc"
	c.Protocol.Version = "V17"
	c.Timing.ReconnectSec = 1
	c.Timing.DialTimeoutSec = 1
	c.Timing.HeartbeatSec = 1
	c.Timing.HeartbeatTimeoutSec = 30
	c.Timing.ChargeReportSec = 1
	c.Timing.ChargeDurationSec = 2
	c.Timing.TradeRetry = 3
	c.Timing.TradeRetrySec = 1
	c.Billing.ChargePowerKw = 60
	c.Billing.PackCapacityKwh = 600
	c.Billing.MeterStartKwh = 100
	c.Billing.InitialSoc = 10
	c.FrameLog.Hex = true
	c.FrameLog.Parsed = true
	c.Devices = []config.DeviceConfig{{
		DeviceNo:   testDeviceNo,
		GunCount:   1,
		AutoStart:  true,
		VoltageV:   400,
		McuVersion: "v1.0.0",
	}}
	return c
}

// currentPile 供 waitFor 打印阶段轨迹使用。
var currentPile atomic.Pointer[Pile]

func pStage() string {
	if p := currentPile.Load(); p != nil {
		st := p.Status()
		return fmt.Sprintf("%s(连接=%v 计费模型=%v)", st.Stage, st.Connected, st.BillingModel != "")
	}
	return ""
}

// stubServer 模拟 vcpp 服务端：完成登录、对时、计费模型下发，并主动发起远程启机。
type stubServer struct {
	ln       net.Listener
	codec    protocol.Codec
	received chan ykc.FrameType
	trade    chan string
	writeMu  sync.Mutex
}

func newStubServer(t *testing.T) *stubServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	s := &stubServer{
		ln:       ln,
		codec:    ykc.NewCodec("V17"),
		received: make(chan ykc.FrameType, 256),
		trade:    make(chan string, 4),
	}
	go s.serve()
	return s
}

func (s *stubServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *stubServer) close() { _ = s.ln.Close() }

func (s *stubServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *stubServer) handle(conn net.Conn) {
	defer conn.Close()

	framer := transport.NewFramer(bufio.NewReaderSize(conn, transport.ReadBufSize), s.codec)
	billingDone := make(chan struct{})
	var once sync.Once

	// 计费模型下发完成后主动下发远程启机，模拟平台侧操作
	go func() {
		<-billingDone
		<-time.After(300 * time.Millisecond)
		s.write(conn, ykc.TypeRemoteStart, remoteStartBody(testDeviceNo, 1))
	}()

	for {
		f, err := framer.Next()
		if err != nil {
			return
		}
		select {
		case s.received <- f.Type:
		default:
		}
		switch f.Type {
		case ykc.TypeLogin:
			s.write(conn, ykc.TypeLoginAck, bcd7Plus(testDeviceNo, 0x00))
			s.write(conn, ykc.TypeTimeSync, timeSyncBody(testDeviceNo))
		case ykc.TypeBillingVerify:
			s.write(conn, ykc.TypeBillingVerifyAck, verifyAckBody(testDeviceNo, "0100", 0x01))
		case ykc.TypeBillingModel:
			s.write(conn, ykc.TypeBillingModelAck, billingModelBody(testDeviceNo))
			once.Do(func() { close(billingDone) })
		case ykc.TypeHeartbeat:
			s.write(conn, ykc.TypeHeartbeatAck, bcd7Plus(testDeviceNo, 0x01, 0x00))
		case ykc.TypeTradeUp, ykc.TypeTradeUp2:
			select {
			case s.trade <- ykc.FrameName(f.Type):
			default:
			}
			s.write(conn, ykc.TypeTradeAck, tradeAckBody(testTradeNo, 0x00))
		}
	}
}

func (s *stubServer) write(conn net.Conn, typ ykc.FrameType, body []byte) {
	// 加锁避免多个协程并发写导致帧交错
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := conn.Write(s.codec.Encode(0, 0, typ, body)); err != nil {
		return
	}
}

const testTradeNo = "20260501000001012606120953054043"

func bcd7Plus(deviceNo string, tail ...byte) []byte {
	w := ykc.NewW(16)
	w.BCD(deviceNo, 7)
	for _, b := range tail {
		w.U8(b)
	}
	return w.Bytes()
}

func timeSyncBody(deviceNo string) []byte {
	w := ykc.NewW(14)
	w.BCD(deviceNo, 7)
	w.Time7(time.Now())
	return w.Bytes()
}

func verifyAckBody(deviceNo, modelNo string, result byte) []byte {
	w := ykc.NewW(10)
	w.BCD(deviceNo, 7)
	w.BCD(modelNo, 2)
	w.U8(result)
	return w.Bytes()
}

func billingModelBody(deviceNo string) []byte {
	w := ykc.NewW(90)
	w.BCD(deviceNo, 7)
	w.BCD("0100", 2)
	ep := [4]uint32{123000, 121000, 100000, 90000}
	sp := [4]uint32{250000, 240000, 230000, 210000}
	for i := 0; i < 4; i++ {
		w.U32LE(ep[i])
		w.U32LE(sp[i])
	}
	w.U8(0)
	for i := 0; i < 48; i++ {
		w.U8(byte(i / 12))
	}
	return w.Bytes()
}

func remoteStartBody(deviceNo string, gun byte) []byte {
	w := ykc.NewW(44)
	w.BCD(testTradeNo, 16)
	w.BCD(deviceNo, 7)
	w.U8(gun)
	w.BCD("0000001000000573", 8)
	w.U64LE(0x540A4BD100000000)
	w.U32LE(100000)
	return w.Bytes()
}

func tradeAckBody(tradeNo string, result byte) []byte {
	w := ykc.NewW(17)
	w.BCD(tradeNo, 16)
	w.U8(result)
	return w.Bytes()
}

// waitFor 轮询等待条件成立，超时时打印阶段变化轨迹便于定位。
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	var trace []string
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if st := pStage(); st != "" {
			if n := len(trace); n == 0 || trace[n-1] != st {
				trace = append(trace, st)
			}
		}
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s，阶段轨迹: %v", what, trace)
}

// TestPileFlow 端到端验证：登录 -> 对时 -> 计费模型 -> 远程启机 -> 充电上报 -> 交易记录。
func TestPileFlow(t *testing.T) {
	srv := newStubServer(t)
	defer srv.close()

	cfg := testConfig(srv.port())
	if err := cfg.Validate(); err != nil {
		t.Fatalf("配置校验失败: %v", err)
	}

	codec, err := protocol.New(cfg.Protocol.Type, cfg.Protocol.Version)
	if err != nil {
		t.Fatalf("创建编解码器失败: %v", err)
	}
	logger := &framelog.Logger{Hex: false, Parsed: false}
	p := NewPile(cfg, cfg.Devices[0], codec, &billing.Cache{}, logger)
	currentPile.Store(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	waitFor(t, 10*time.Second, "桩进入空闲态（完成登录与计费模型获取）", func() bool {
		return p.Status().Stage == string(StageIdle)
	})

	st := p.Status()
	if !st.Connected {
		t.Fatalf("桩应处于已连接状态")
	}
	if st.BillingModel == "" {
		t.Fatalf("计费模型未缓存")
	}

	waitFor(t, 10*time.Second, "收到远程启机后进入充电态", func() bool {
		return p.Status().Stage == string(StageCharging)
	})

	waitFor(t, 15*time.Second, "充电到点后回到空闲态", func() bool {
		return p.Status().Stage == string(StageIdle)
	})

	select {
	case typ := <-srv.trade:
		if typ != "交易记录" {
			t.Fatalf("交易记录帧名称不符: %s", typ)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("未收到交易记录")
	}
}

// TestPileRejectStartWithoutBilling 未获取计费模型时按协议应拒绝启机。
func TestPileRejectStartWithoutBilling(t *testing.T) {
	cfg := testConfig(1) // 端口 1：无服务端在线
	codec, _ := protocol.New(cfg.Protocol.Type, cfg.Protocol.Version)
	logger := &framelog.Logger{}
	p := NewPile(cfg, cfg.Devices[0], codec, &billing.Cache{}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	time.Sleep(200 * time.Millisecond)
	if err := p.Submit(Command{Kind: CmdGunStart, Gun: 1}); err == nil {
		t.Fatalf("未获取计费模型时应当拒绝启机")
	}
}

// TestManagerLifecycle 校验桩的启停与状态查询。
func TestManagerLifecycle(t *testing.T) {
	cfg := testConfig(1)
	if _, err := NewManager(cfg); err != nil {
		t.Fatalf("创建 Manager 失败: %v", err)
	}
}
