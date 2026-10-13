package sim

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/csf/vcpp-emulator/internal/billing"
	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/framelog"
	"github.com/csf/vcpp-emulator/internal/protocol"
	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
	"github.com/csf/vcpp-emulator/internal/transport"
)

// versionTestConfig 构造指定版本与端口的最小配置。
func versionTestConfig(version string, port int) *config.Config {
	c := &config.Config{}
	c.Protocol.Type = "ykc"
	c.Protocol.Version = version
	c.Server.Ip = "127.0.0.1"
	c.Server.Port = port
	c.Timing.ReconnectSec = 1
	c.Timing.DialTimeoutSec = 2
	c.Timing.HeartbeatSec = 10
	c.Timing.HeartbeatTimeoutSec = 30
	c.Timing.ChargeReportSec = 15
	c.Timing.ChargeDurationSec = 600
	c.Timing.TradeRetry = 3
	c.Timing.TradeRetrySec = 1
	c.Billing.ChargePowerKw = 60
	c.Billing.PackCapacityKwh = 600
	c.Billing.MeterStartKwh = 100
	c.Billing.InitialSoc = 10
	c.Devices = []config.DeviceConfig{{
		DeviceNo:   37000005270002,
		GunCount:   1,
		AutoStart:  true,
		VoltageV:   400,
		McuVersion: "v1.0.0",
	}}
	return c
}

// captureLogin 起一个只接收登录帧的桩服务端，返回登录帧消息体通道。
func captureLogin(t *testing.T, version string) (int, chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	got := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		framer := transport.NewFramer(bufio.NewReaderSize(conn, transport.ReadBufSize), ykc.NewCodec(version))
		for {
			f, err := framer.Next()
			if err != nil {
				return
			}
			if f.Type == ykc.TypeLogin {
				got <- append([]byte(nil), f.Body...)
				return
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

// TestPileLoginVersion 校验 V1.5 / V1.6 / V1.7 上送的登录帧携带各自的通信协议版本字节。
func TestPileLoginVersion(t *testing.T) {
	for version, want := range map[string]byte{"V15": 0x0F, "V16": 0x10, "V17": 0x11} {
		port, got := captureLogin(t, version)
		cfg := versionTestConfig(version, port)
		codec, err := protocol.New(cfg.Protocol.Type, cfg.Protocol.Version)
		if err != nil {
			t.Fatalf("%s 创建编解码器失败: %v", version, err)
		}
		p := NewPile(cfg, cfg.Devices[0], codec, &billing.Cache{}, &framelog.Logger{})
		if p.DeviceNo() != 37000005270002 {
			t.Fatalf("%s 桩号不符: %014d", version, p.DeviceNo())
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			p.Run(ctx)
			close(done)
		}()

		select {
		case body := <-got:
			if len(body) != 30 {
				t.Fatalf("%s 登录消息体应为 30 字节，实际 %d", version, len(body))
			}
			if body[9] != want {
				t.Fatalf("%s 登录帧协议版本字节期望 0x%02X 实际 0x%02X", version, want, body[9])
			}
		case <-time.After(8 * time.Second):
			t.Fatalf("%s 未收到登录帧", version)
		}
		cancel()
		<-done
	}
}
