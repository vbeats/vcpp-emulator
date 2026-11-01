package ykc

import (
	"encoding/hex"
	"testing"
	"time"
)

// 真实桩上送报文（取自现场充电日志），用于校验帧编解码与 CRC 实现
var realFrames = []struct {
	hex     string
	cmd     byte
	bodyLen int
}{
	// 心跳：68 0D FF FF 00 03 37000005270004 01 00 F6 3D
	{"680DFFFF0003370000052700040100F63D", CmdHeartbeat, 9},
	// 心跳（枪 2）
	{"680D00000003370000052700040200F526", CmdHeartbeat, 9},
	// BMS 需求与输出
	{"6830C33C00233700000527000501261009231255808237000005270005010410880E02E10E870E8E11631200B40E830E2E000C72", 0x23, 44},
	// 实时数据（67 字节消息体，桩侧多带了 V2.1 扩展字段）
	{"6847CA3E0013370000052700050126100923125580823700000527000501030201BB0E1D0143000000000000000063582F0012005AF703005AF703005034030000005A00628DDF06008DEE", CmdRealTimeData, 67},
}

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex 解码失败: %v", err)
	}
	return b
}

func TestCRC16Modbus(t *testing.T) {
	for _, tc := range realFrames {
		raw := decodeHex(t, tc.hex)
		want := uint16(raw[len(raw)-2]) | uint16(raw[len(raw)-1])<<8
		if got := CRC16Modbus(raw[2 : len(raw)-2]); got != want {
			t.Fatalf("CRC 不匹配: 期望 %04X 实际 %04X (%s)", want, got, tc.hex)
		}
	}
}

func TestDecodeRealFrame(t *testing.T) {
	for _, tc := range realFrames {
		raw := decodeHex(t, tc.hex)
		f, n, err := (&specV15).Decode(raw)
		if err != nil {
			t.Fatalf("解码失败: %v (%s)", err, tc.hex)
		}
		if n != len(raw) {
			t.Fatalf("消耗字节数不符: %d != %d", n, len(raw))
		}
		if f.Cmd != tc.cmd {
			t.Fatalf("命令码不符: 期望 %02X 实际 %02X", tc.cmd, f.Cmd)
		}
		if len(f.Body) != tc.bodyLen {
			t.Fatalf("消息体长度不符: 期望 %d 实际 %d", tc.bodyLen, len(f.Body))
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	for _, spec := range AllSpecs() {
		body := make([]byte, 40)
		for i := range body {
			body[i] = byte(i)
		}
		raw := spec.Encode(0x1234, CmdHeartbeat, body, time.Now())
		// 上行序列号按小端写入
		headerLen := 1 + spec.LengthFieldLen
		if raw[headerLen] != 0x34 || raw[headerLen+1] != 0x12 {
			t.Fatalf("%s 上行序列号应为小端", spec.Code)
		}
		// 下行帧序列号按大端回写，构造后验证解码
		down := append([]byte(nil), raw...)
		down[headerLen] = 0x12
		down[headerLen+1] = 0x34
		crc := CRC16Modbus(down[headerLen : len(down)-2])
		down[len(down)-2] = byte(crc)
		down[len(down)-1] = byte(crc >> 8)

		f, n, err := spec.Decode(down)
		if err != nil {
			t.Fatalf("%s 解码失败: %v", spec.Code, err)
		}
		if n != len(down) || f.Cmd != CmdHeartbeat || f.Seq != 0x1234 {
			t.Fatalf("%s 回环校验失败: n=%d cmd=%02X seq=%04X", spec.Code, n, f.Cmd, f.Seq)
		}
		if len(f.Body) != len(body) {
			t.Fatalf("%s 消息体长度不符", spec.Code)
		}
		for i := range body {
			if f.Body[i] != body[i] {
				t.Fatalf("%s 消息体内容不符", spec.Code)
			}
		}
	}
}

func TestBcd(t *testing.T) {
	if got := BcdEncode("37000005270005", 7); ToHex(got) != "37000005270005" {
		t.Fatalf("BCD 编码错误: %s", ToHex(got))
	}
	if got := BcdDecode(decodeHex(t, "37000005270005")); got != "37000005270005" {
		t.Fatalf("BCD 解码错误: %s", got)
	}
	if got := ToHex(BcdEncode("123", 4)); got != "00000123" {
		t.Fatalf("BCD 右对齐错误: %s", got)
	}
}

func TestCP56(t *testing.T) {
	want := time.Date(2026, 10, 10, 9, 30, 45, 0, time.Local)
	raw := EncodeCP56(want)
	got, err := DecodeCP56(raw)
	if err != nil {
		t.Fatalf("CP56 解码失败: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("CP56 回环失败: 期望 %v 实际 %v", want, got)
	}
}

func TestTradeBodyLength(t *testing.T) {
	rec := TradeRecord{
		TradeNo: "37000005270005012610092312558082",
		PileNo:  "37000005270005",
		GunNo:   1,
		Rates:   []RateAmount{{}, {}, {}, {}},
	}
	if n := len(buildTradeV15(rec)); n != 158 {
		t.Fatalf("0x3B 消息体长度应为 158，实际 %d", n)
	}
	if n := len(buildTradeV17(rec)); n != 201 {
		t.Fatalf("0x3D(V1.7) 消息体长度应为 201，实际 %d", n)
	}
	rec.Rates = append(rec.Rates, RateAmount{})
	if n := len(buildTradeV17(rec)); n != 217 {
		t.Fatalf("0x3D(V1.7 深谷) 消息体长度应为 217，实际 %d", n)
	}
	if n := len(buildTradeV21(rec)); n != 330+16*5 {
		t.Fatalf("0x3D(V2.1) 消息体长度不符: %d", n)
	}
}

func TestRealTimeBodyLength(t *testing.T) {
	d := RealTimeData{TradeNo: "1234", PileNo: "37000005270005"}
	if n := len((&specV15).BuildRealTimeData(d)); n != 60 {
		t.Fatalf("V1.5 实时数据应为 60B，实际 %d", n)
	}
	if n := len((&specV21).BuildRealTimeData(d)); n != 67 {
		t.Fatalf("V2.1 实时数据应为 67B，实际 %d", n)
	}
}

func TestLoginBodyLength(t *testing.T) {
	info := LoginInfo{PileNo: "37000005270005", GunCount: 2}
	if n := len((&specV15).BuildLogin(info)); n != 30 {
		t.Fatalf("V1.5 登录应为 30B，实际 %d", n)
	}
	if n := len((&specV18).BuildLogin(info)); n != 127 {
		t.Fatalf("V1.8 登录应为 127B，实际 %d", n)
	}
	if n := len((&specV21).BuildLogin(info)); n != 148 {
		t.Fatalf("V2.1 登录应为 148B，实际 %d", n)
	}
}
