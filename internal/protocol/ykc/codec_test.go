package ykc

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// 测试数据取自 doc/云快充/测试数据.md 中的真实报文。

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex 解码失败: %v", err)
	}
	return b
}

func hexUpper(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

// TestSamplesDecode 校验下行帧样例能被正确解析（含 CRC 校验）。
func TestSamplesDecode(t *testing.T) {
	c := NewCodec("V17")
	cases := []struct {
		name string
		hex  string
		typ  FrameType
	}{
		{"登录应答", "680C00000002202605010000010004BD", TypeLoginAck},
		{"心跳应答", "680D004F00042026050100000101004867", TypeHeartbeatAck},
		{"计费模型验证应答", "680E00000006202605010000010000011641", TypeBillingVerifyAck},
		{"计费模型请求应答", "685E0000000A20260501000001010078E0010090D00300A8D8010080A90300A086010070820300905F01005034030000000000000000000000000000000000000000000001010101020202020202030303030303030303030303030303030303FA22", TypeBillingModelAck},
		{"远程停机", "680C000000362026050100000101A3BC", TypeRemoteStop},
		{"交易记录确认", "68150038004020260501000001012606121004475025004416", TypeTradeAck},
		{"远程重启", "680C000000922026050100000101697B", TypeRemoteRestart},
		{"对时设置", "68120000005620260501000001C2E51A090C061AD46F", TypeTimeSync},
	}
	for _, tc := range cases {
		raw := mustHex(t, tc.hex)
		f, err := c.Decode(raw)
		if err != nil {
			t.Fatalf("%s: 解码失败: %v", tc.name, err)
		}
		if f.Type != tc.typ {
			t.Fatalf("%s: 帧类型不符，期望 0x%02X 实际 0x%02X", tc.name, tc.typ, f.Type)
		}
	}
}

// TestLoginAckDecode 校验登录应答字段。
func TestLoginAckDecode(t *testing.T) {
	f, err := NewCodec("V17").Decode(mustHex(t, "680C00000002202605010000010004BD"))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	deviceNo, result, _, err := DecodeLoginAck(f.Body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if deviceNo != 20260501000001 {
		t.Fatalf("桩号不符: %014d", deviceNo)
	}
	if result != 0x00 {
		t.Fatalf("登录结果不符: %d", result)
	}
}

// TestBillingVerifyAckDecode 校验计费模型验证应答（vcpp 固定应答不一致）。
func TestBillingVerifyAckDecode(t *testing.T) {
	f, err := NewCodec("V17").Decode(mustHex(t, "680E00000006202605010000010000011641"))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	_, modelNo, result, _, err := DecodeBillingVerifyAck(f.Body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if modelNo != "0000" || result != 0x01 {
		t.Fatalf("计费模型编号=%s 验证结果=%d", modelNo, result)
	}
}

// TestBillingModelAckDecode 校验 48 时段计费模型解析。
func TestBillingModelAckDecode(t *testing.T) {
	f, err := NewCodec("V17").Decode(mustHex(t,
		"685E0000000A20260501000001010078E0010090D00300A8D8010080A90300A086010070820300905F01005034030000000000000000000000000000000000000000000001010101020202020202030303030303030303030303030303030303FA22"))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	p, _, err := DecodeBillingModelAck(f.Body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if p.ModelNo != "0100" {
		t.Fatalf("计费模型编号不符: %s", p.ModelNo)
	}
	wantEp := [4]int64{123000, 121000, 100000, 90000}
	wantSp := [4]int64{250000, 240000, 230000, 210000}
	for i := 0; i < 4; i++ {
		if p.Ep[i] != wantEp[i] {
			t.Fatalf("%s段电费不符，期望 %d 实际 %d", SectionNames[i], wantEp[i], p.Ep[i])
		}
		if p.Sp[i] != wantSp[i] {
			t.Fatalf("%s段服务费不符，期望 %d 实际 %d", SectionNames[i], wantSp[i], p.Sp[i])
		}
	}
	// 样例时段：0~9:30 尖，10:00~11:30 峰，12:00~14:00 平，14:30~23:30 谷
	if p.Slots[0] != 0 || p.Slots[21] != 1 || p.Slots[25] != 2 || p.Slots[47] != 3 {
		t.Fatalf("时段费率号不符: [0]=%d [21]=%d [25]=%d [47]=%d",
			p.Slots[0], p.Slots[21], p.Slots[25], p.Slots[47])
	}
}

// TestRemoteStartDecode 校验远程启机解析。
func TestRemoteStartDecode(t *testing.T) {
	// V1.7 协议正文的 0x34 样例。测试数据.md 中同一帧的长度域与字节数不一致，
	// 协议正文样例的帧校验域也与字节不符，故此处直接校验消息体（44 字节）。
	raw := mustHex(t,
		"6830007C0034550314127823050120180619144446805503141278230501000000100000057300000000D14B0A54A08601004622")
	body := raw[6 : 6+44]
	v, _, err := DecodeRemoteStart(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if v.TradeNo != "55031412782305012018061914444680" {
		t.Fatalf("交易流水号不符: %s", v.TradeNo)
	}
	if v.DeviceNo != 55031412782305 || v.Gun != 1 {
		t.Fatalf("桩号/枪号不符: %014d %d", v.DeviceNo, v.Gun)
	}
	if v.LogicCard != "0000001000000573" {
		t.Fatalf("逻辑卡号不符: %s", v.LogicCard)
	}
	if v.BalanceYuan < 999.99 || v.BalanceYuan > 1000.01 {
		t.Fatalf("账户余额不符: %.2f", v.BalanceYuan)
	}
}

// TestTimeSyncDecode 校验对时设置的 CP56Time2a 解码。
func TestTimeSyncDecode(t *testing.T) {
	f, err := NewCodec("V17").Decode(mustHex(t, "68120000005620260501000001C2E51A090C061AD46F"))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	_, ts, _, err := DecodeTimeSync(f.Body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ts.Year() != 2026 || ts.Month() != 6 || ts.Day() != 12 || ts.Hour() != 9 || ts.Minute() != 26 {
		t.Fatalf("对时时间不符: %s", ts)
	}
}

// TestEncodeSamples 校验上行帧编码与官方样例逐字节一致。
func TestEncodeSamples(t *testing.T) {
	c := NewCodec("V17")

	loginBody, _ := EncodeLogin(LoginInfo{
		DeviceNo:   20260501000001,
		PileType:   0,
		GunCount:   2,
		ProtoVer:   0x0F,
		McuVersion: "220422",
		NetType:    1,
		Sim:        "00000000000000000000",
		Supplier:   0,
	})
	if got := hexUpper(c.Encode(0, 0, TypeLogin, loginBody)); got !=
		"6822000000012026050100000100020F3232303432320000010000000000000000000000563A" {
		t.Fatalf("登录帧不符:\n%s", got)
	}

	hbBody, _ := EncodeHeartbeat(20260501000001, 0x01, 0x00)
	if got := hexUpper(c.Encode(79, 0, TypeHeartbeat, hbBody)); got != "680D4F0000032026050100000101000659" {
		t.Fatalf("心跳帧不符:\n%s", got)
	}

	bvBody, _ := EncodeBillingVerify(20260501000001, "0000")
	if got := hexUpper(c.Encode(0, 0, TypeBillingVerify, bvBody)); got != "680D000000052026050100000100000D19" {
		t.Fatalf("计费模型验证帧不符:\n%s", got)
	}

	bmBody, _ := EncodeBillingModelRequest(20260501000001)
	if got := hexUpper(c.Encode(0, 0, TypeBillingModel, bmBody)); got != "680B0000000920260501000001EC76" {
		t.Fatalf("计费模型请求帧不符:\n%s", got)
	}

	bmsBody, _ := EncodeBmsStatus(BmsStatusInfo{
		TradeNo:      "20260501000001012606121004475025",
		DeviceNo:     20260501000001,
		Gun:          0x01,
		MaxCellNo:    2,
		BattMaxTempC: 25,
		MaxTempProbe: 1,
		BattMinTempC: 20,
		MinTempProbe: 1,
		Status1:      0x00,
		Status2:      0x10,
	})
	if got := hexUpper(c.Encode(0x30, 0, TypeChargingBmsStatus, bmsBody)); got !=
		"682330000025202605010000010126061210044750252026050100000101014B00460000100F0A" {
		t.Fatalf("BMS 信息帧不符:\n%s", got)
	}

	startAckBody, _ := EncodeRemoteStartAck("20260501000001012606120953054043", 20260501000001, 0x01, 0x01, 0x00)
	if got := hexUpper(c.Encode(0, 0, TypeRemoteStartAck, startAckBody)); got !=
		"681E0000003320260501000001012606120953054043202605010000010101001976" {
		t.Fatalf("远程启机回复帧不符:\n%s", got)
	}

	// 协议正文的 0x35 样例序列号为 0003，与上行小端不同，故只比对消息体
	stopAckBody, _ := EncodeRemoteStopAck(32010200000001, 0x01, 0x01, 0x00)
	if got := hexUpper(stopAckBody); got != "32010200000001010100" {
		t.Fatalf("远程停机回复消息体不符:\n%s", got)
	}

	stopBody, _ := EncodeStopReason(StopReasonInfo{
		TradeNo:  "20260501000001012606121004475025",
		DeviceNo: 20260501000001,
		Gun:      0x01,
		Reason:   0x40,
	})
	if got := hexUpper(c.Encode(0x36, 0, TypeChargingBmsStop, stopBody)); got !=
		"68203600001D20260501000001012606121004475025202605010000010140000000F84E" {
		t.Fatalf("BMS 中止帧不符:\n%s", got)
	}
}

// TestTradeRecordLength 校验交易记录两种帧长度。
func TestTradeRecordLength(t *testing.T) {
	base := TradeRecordInfo{
		TradeNo:     "20260501000001012606121004475025",
		DeviceNo:    20260501000001,
		Gun:         0x01,
		StartTime:   time.Date(2026, 6, 12, 10, 4, 47, 0, time.Local),
		EndTime:     time.Date(2026, 6, 12, 10, 34, 47, 0, time.Local),
		MeterNo:     "000000000001",
		TotalEnergy: 110000,
		TotalAmount: 281600,
		TradeTime:   time.Date(2026, 6, 12, 10, 34, 47, 0, time.Local),
		StopReason:  0x40,
	}
	newer := base
	newer.WithMeter = true
	if body, _ := EncodeTradeRecord(newer); len(body) != 201 {
		t.Fatalf("0x3D 交易记录体长度应为 201，实际 %d", len(body))
	}
	if body, _ := EncodeTradeRecord(base); len(body) != 158 {
		t.Fatalf("0x3B 交易记录体长度应为 158，实际 %d", len(body))
	}
}

// TestCodecRoundTrip 校验组帧后能被完整解回（含 CRC 与序列号字节序）。
func TestCodecRoundTrip(t *testing.T) {
	c := NewCodec("V17")
	for typ, body := range map[FrameType][]byte{
		TypeLogin:             make([]byte, 30),
		TypeHeartbeat:         make([]byte, 9),
		TypeRealTimeUp:        make([]byte, 60),
		TypeHandshake:         make([]byte, 73),
		TypeParamConfig:       make([]byte, 45),
		TypeChargingBmsStatus: make([]byte, 31),
		TypeChargingBmsQa:     make([]byte, 44),
		TypeChargeFinish:      make([]byte, 39),
	} {
		raw := c.Encode(258, 0, typ, body) // 0x0102：可验证小端字节序
		f, err := c.Decode(raw)
		if err != nil {
			t.Fatalf("0x%02X 解码失败: %v", typ, err)
		}
		if f.Type != typ {
			t.Fatalf("帧类型不一致: %02X != %02X", f.Type, typ)
		}
		if len(f.Body) != len(body) {
			t.Fatalf("0x%02X 消息体长度不一致: %d", typ, len(f.Body))
		}
	}
}

// TestSequenceDomainByteOrder 校验序列号域的字节序（上行小端 / 下行大端，协议与服务端实现对齐，不可配置）。
func TestSequenceDomainByteOrder(t *testing.T) {
	c := NewCodec("V17")
	raw := c.Encode(0x0102, 0, TypeHeartbeat, make([]byte, 9))
	if raw[2] != 0x02 || raw[3] != 0x01 {
		t.Fatalf("上行序列号应为小端，实际 %02X %02X", raw[2], raw[3])
	}
	// 下行解析按大端：样例 680D004F0004... 的序列号为 0x004F
	f, err := c.Decode(mustHex(t, "680D004F00042026050100000101004867"))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if f.Sequence != 0x004F {
		t.Fatalf("下行序列号应按大端解析，实际 %d", f.Sequence)
	}
}

// TestCP56 校验 CP56Time2a 编解码往返。
func TestCP56(t *testing.T) {
	// CP56Time2a 无秒字段，秒只以毫秒表示
	want := time.Date(2026, 6, 1, 9, 12, 0, 500*int(time.Millisecond), time.Local)
	got, err := DecodeCP56(EncodeCP56(want))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("时间不一致: %s != %s", got, want)
	}
}

// TestProtoVerByte 校验版本标签到协议版本字节的映射。
func TestProtoVerByte(t *testing.T) {
	cases := map[string]byte{"V15": 0x0F, "V16": 0x10, "V17": 0x11, "V18": 0x12, "V21": 0x15}
	for v, want := range cases {
		if got := ProtoVerByte(v); got != want {
			t.Fatalf("%s 期望 0x%02X 实际 0x%02X", v, want, got)
		}
	}
}

// TestTradeFrameType 校验交易记录帧类型由版本决定（协议约定，不可配置）。
func TestTradeFrameType(t *testing.T) {
	cases := map[string]byte{"V15": 0x3B, "V16": 0x3B, "V17": 0x3D, "V18": 0x3D, "V21": 0x3D}
	for v, want := range cases {
		if got := TradeFrameType(v); got != want {
			t.Fatalf("%s 期望 0x%02X 实际 0x%02X", v, want, got)
		}
	}
}

// TestTradeNo 校验交易流水号规则（桩号 + 枪号 + 时间 + 序号，共 32 位）。
func TestTradeNo(t *testing.T) {
	no := TradeNo(20260501000001, 1, time.Date(2026, 6, 12, 9, 53, 5, 0, time.Local), 67)
	if no != "20260501000001012606120953050067" {
		t.Fatalf("交易流水号不符: %s", no)
	}
	if len(no) != 32 {
		t.Fatalf("交易流水号长度应为 32，实际 %d", len(no))
	}
}
