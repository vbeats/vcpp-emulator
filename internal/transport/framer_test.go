package transport

import (
	"bufio"
	"bytes"
	"io"
	"testing"

	"github.com/csf/vcpp-emulator/internal/protocol"
	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
)

func newCodec() protocol.Codec {
	return ykc.NewCodec("V17")
}

// TestFramerStickyAndGarbage 校验拆帧能处理粘包、垃圾前缀与不完整尾帧。
func TestFramerStickyAndGarbage(t *testing.T) {
	c := newCodec()
	body1, _ := ykc.EncodeHeartbeat("20260501000001", 0x01, 0x00)
	body2, _ := ykc.EncodeBillingVerify("20260501000001", "0000")
	f1 := c.Encode(1, 0, ykc.TypeHeartbeat, body1)
	f2 := c.Encode(2, 0, ykc.TypeBillingVerify, body2)

	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0x00}) // 垃圾前缀，应被丢弃
	buf.Write(f1)
	buf.Write(f2)
	buf.Write(f1[:len(f1)-3]) // 半包，不足以构成一帧

	framer := NewFramer(bufio.NewReaderSize(&buf, ReadBufSize), c)

	got1, err := framer.Next()
	if err != nil {
		t.Fatalf("读取第一帧失败: %v", err)
	}
	if got1.Type != ykc.TypeHeartbeat {
		t.Fatalf("第一帧类型不符: 0x%02X", got1.Type)
	}
	got2, err := framer.Next()
	if err != nil {
		t.Fatalf("读取第二帧失败: %v", err)
	}
	if got2.Type != ykc.TypeBillingVerify {
		t.Fatalf("第二帧类型不符: 0x%02X", got2.Type)
	}
	if _, err = framer.Next(); err == nil || err != io.EOF {
		t.Fatalf("半包应返回 io.EOF，实际: %v", err)
	}
}

// TestFramerBufferReuse 校验帧数据不会被后续读取覆盖（帧会异步交给 Actor 处理）。
func TestFramerBufferReuse(t *testing.T) {
	c := newCodec()
	body1, _ := ykc.EncodeHeartbeat("20260501000001", 0x01, 0x00)
	body2, _ := ykc.EncodeBillingVerify("20260501000001", "0000")
	f1 := c.Encode(1, 0, ykc.TypeHeartbeat, body1)
	f2 := c.Encode(2, 0, ykc.TypeBillingVerify, body2)

	var buf bytes.Buffer
	buf.Write(f1)
	buf.Write(f2)
	// 使用远小于帧长的内部缓冲，强制 bufio 在读第二帧时搬迁/复用缓冲
	framer := NewFramer(bufio.NewReaderSize(&buf, 32), c)

	got1, err := framer.Next()
	if err != nil {
		t.Fatalf("读取第一帧失败: %v", err)
	}
	if _, err = framer.Next(); err != nil {
		t.Fatalf("读取第二帧失败: %v", err)
	}
	if !bytes.Equal(got1.Body, body1) {
		t.Fatalf("第一帧消息体被后续读取覆盖: %X", got1.Body)
	}
	if !bytes.Equal(got1.Raw, f1) {
		t.Fatalf("第一帧原始字节被后续读取覆盖: %X", got1.Raw)
	}
}

// TestFramerCRCFail 校验 CRC 错误的帧会被丢弃并重新同步到下一帧。
func TestFramerCRCFail(t *testing.T) {
	c := newCodec()
	body, _ := ykc.EncodeHeartbeat("20260501000001", 0x01, 0x00)
	bad := c.Encode(1, 0, ykc.TypeHeartbeat, body)
	bad[len(bad)-1] ^= 0xFF
	good := c.Encode(2, 0, ykc.TypeHeartbeat, body)

	buf := bytes.NewBuffer(bad)
	buf.Write(good)
	framer := NewFramer(bufio.NewReaderSize(buf, ReadBufSize), c)

	f, err := framer.Next()
	if err != nil {
		t.Fatalf("应跳过错误帧后读到正确帧: %v", err)
	}
	// 下行按大端解析，编码时以小端写入的 seq=2 会被读成 0x0200
	if f.Sequence != 512 {
		t.Fatalf("应同步到第二帧，实际序列号 %d", f.Sequence)
	}
}

// TestFramerOversize 校验非法长度域（0）会被丢弃且不会误判为合法帧。
func TestFramerOversize(t *testing.T) {
	c := newCodec()
	body, _ := ykc.EncodeHeartbeat("20260501000001", 0x01, 0x00)
	good := c.Encode(1, 0, ykc.TypeHeartbeat, body)

	buf := bytes.NewBuffer([]byte{0x68, 0x00, 0x00, 0x00})
	buf.Write(good)
	framer := NewFramer(bufio.NewReaderSize(buf, ReadBufSize), c)

	f, err := framer.Next()
	if err != nil {
		t.Fatalf("应跳过非法帧后读到正确帧: %v", err)
	}
	if f.Type != ykc.TypeHeartbeat {
		t.Fatalf("帧类型不符: 0x%02X", f.Type)
	}
}
