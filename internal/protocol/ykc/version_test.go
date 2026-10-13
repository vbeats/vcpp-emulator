package ykc

import "testing"

// TestLoginProtoVer 校验登录帧第 10 个字节为通信协议版本字节，随版本变化。
func TestLoginProtoVer(t *testing.T) {
	for v, want := range map[string]byte{V15: 0x0F, V16: 0x10, V17: 0x11} {
		body, _ := EncodeLogin(LoginInfo{DeviceNo: 37000005270002, ProtoVer: ProtoVerByte(v)})
		if len(body) != 30 {
			t.Fatalf("%s 登录消息体应为 30 字节，实际 %d", v, len(body))
		}
		if body[9] != want {
			t.Fatalf("%s 登录帧协议版本字节期望 0x%02X 实际 0x%02X", v, want, body[9])
		}
	}
}

// TestDeviceNoCodec 校验桩号在 14 位数字与 7 字节 BCD 之间无损往返。
func TestDeviceNoCodec(t *testing.T) {
	for _, v := range []uint64{37000005270002, 20260501000001, 1, MaxDeviceNo} {
		w := NewW(7)
		w.DeviceNo(v)
		if got := NewR(w.Bytes()).DeviceNo(); got != v {
			t.Fatalf("桩号往返失败: %014d -> %014d", v, got)
		}
		if got := FormatDeviceNo(v); len(got) != DeviceNoDigits {
			t.Fatalf("桩号格式化应为 %d 位: %s", DeviceNoDigits, got)
		}
	}
}

// TestTradeWithMeter 校验只有 V1.7 起才带电表块。
func TestTradeWithMeter(t *testing.T) {
	if TradeWithMeter(V15) || TradeWithMeter(V16) {
		t.Fatalf("V1.5/V1.6 交易记录不应带电表块")
	}
	if !TradeWithMeter(V17) {
		t.Fatalf("V1.7 交易记录应带电表块")
	}
}

// TestSupportsParallel 校验并充帧从 V1.6 起支持。
func TestSupportsParallel(t *testing.T) {
	if SupportsParallel(V15) {
		t.Fatalf("V1.5 不应支持并充")
	}
	if !SupportsParallel(V16) || !SupportsParallel(V17) {
		t.Fatalf("V1.6/V1.7 应支持并充")
	}
}

// TestTradeRecordBodyLen 校验交易记录消息体长度随「段数 × 是否带电表块」变化：
// 4 段 158（V1.5/V1.6）/ 5 段 174 / V1.7 的 4 段 201 / V1.7 的 5 段 217。
func TestTradeRecordBodyLen(t *testing.T) {
	cases := []struct {
		sections int
		meter    bool
		want     int
	}{
		{4, false, 158},
		{5, false, 174},
		{4, true, 201},
		{5, true, 217},
	}
	for _, tc := range cases {
		body, _ := EncodeTradeRecord(TradeRecordInfo{Sections: tc.sections, WithMeter: tc.meter})
		if len(body) != tc.want {
			t.Fatalf("%d 段(电表=%v) 期望 %d 字节 实际 %d", tc.sections, tc.meter, tc.want, len(body))
		}
	}
}

// TestTradeRecordDeepValley 校验 5 段模式会把深谷数据写在谷之后。
func TestTradeRecordDeepValley(t *testing.T) {
	info := TradeRecordInfo{Sections: 5, TradeNo: "37000005270002022610101001506379", DeviceNo: 37000005270002}
	info.Energy[4] = 12345
	info.Amount[4] = 54321
	body, desc := EncodeTradeRecord(info)
	if len(body) != 174 {
		t.Fatalf("5 段 0x3B 应为 174 字节，实际 %d", len(body))
	}
	r := NewR(body[38+64:]) // 跳过流水号/桩号/枪号/时间与 4 段，定位到第 5 段
	if got := int64(r.U32LE()); got != 0 {
		t.Fatalf("深谷单价应为 0，实际 %d", got)
	}
	if got := int64(r.U32LE()); got != 12345 {
		t.Fatalf("深谷电量不符: %d", got)
	}
	r.U32LE()
	if got := int64(r.U32LE()); got != 54321 {
		t.Fatalf("深谷金额不符: %d", got)
	}
	if !contains(desc, "尖峰平谷深谷") || !contains(desc, "深谷=1.2345kWh") {
		t.Fatalf("解析描述未体现深谷: %s", desc)
	}
}

// TestBillingBodyLenBySections 校验计费模型按消息体长度区分 4 段与 5 段。
func TestBillingBodyLenBySections(t *testing.T) {
	build := func(n int) []byte {
		w := NewW(8 + 8*n + 1 + 48)
		w.DeviceNo(37000005270002)
		w.BCD("0100", 2)
		for i := 0; i < n; i++ {
			w.U32LE(130000)
			w.U32LE(200000)
		}
		w.U8(0)
		for i := 0; i < 48; i++ {
			w.U8(byte(i % n))
		}
		return w.Bytes()
	}
	for _, tc := range []struct{ n, want int }{{4, 4}, {5, 5}} {
		p, _, err := DecodeBillingModelAck(build(tc.n))
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if p.Sections != tc.want {
			t.Fatalf("%d 段模型被识别为 %d 段", tc.n, p.Sections)
		}
		for i := 0; i < tc.n; i++ {
			if p.Ep[i] != 130000 || p.Sp[i] != 200000 {
				t.Fatalf("第 %d 段费率不符: %d/%d", i, p.Ep[i], p.Sp[i])
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
