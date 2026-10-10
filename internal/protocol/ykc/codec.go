package ykc

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// StartFlag 帧起始符。
const StartFlag = 0x68

// MaxDataLen 数据域最大长度，对齐 vcpp 侧 LengthFieldBasedFrameDecoder(65540)。
const MaxDataLen = 65536

var ErrBadStart = errors.New("ykc: 帧起始符不是 0x68")

// 序列号域的字节序由协议与服务端实现共同决定，不属于可调配置：
//   - 上行（模拟器 -> server）：低字节在前，对齐 vcpp 的 readUnsignedShortLE
//   - 下行（server -> 模拟器）：高字节在前，对齐 vcpp 的 YkcAckUtil.writeShort
//
// 服务端两侧字节序不一致（见 vcpp/AGENTS.md 待办），若服务端后续统一，只需修改此处两个常量。
const (
	upSeqOrder   = false // 上行：小端（低字节在前）
	downSeqOrder = true  // 下行：大端（高字节在前）
)

// Frame 一帧完整报文。
type Frame struct {
	Version   string
	Sequence  uint16
	SeqRaw    [2]byte // 接收到的原始序列号字节，用于按原样回显
	Encrypt   byte
	Type      FrameType
	Body      []byte
	EventTime time.Time // 仅 V2.1+ 携带
	Raw       []byte    // 完整帧，供日志打印
}

// Codec 帧编解码器。编解码均为纯函数式，便于单测。
type Codec struct {
	version  string
	lenBytes int  // 数据长度字段字节数：V2.1 为 2，其余为 1
	withTime bool // 是否携带 7 字节发送时间：仅 V2.1+
}

// NewCodec 按协议版本构造编解码器。
func NewCodec(version string) *Codec {
	v := strings.ToUpper(strings.TrimSpace(version))
	c := &Codec{version: v, lenBytes: 1, withTime: false}
	if v == "V21" {
		c.lenBytes = 2
		c.withTime = true
	}
	return c
}

func (c *Codec) Version() string { return c.version }

// HeaderLen 起始符 + 数据长度字段的字节数。
func (c *Codec) HeaderLen() int { return 1 + c.lenBytes }

// FrameLen 依据缓冲区头部计算整帧长度，buf 至少要有 HeaderLen 个字节。
func (c *Codec) FrameLen(buf []byte) (int, error) {
	if len(buf) < c.HeaderLen() {
		return 0, ErrShort
	}
	if buf[0] != StartFlag {
		return 0, ErrBadStart
	}
	var dataLen int
	if c.lenBytes == 1 {
		dataLen = int(buf[1])
	} else {
		dataLen = int(buf[1]) | int(buf[2])<<8
	}
	if dataLen < 4 || dataLen > MaxDataLen {
		return 0, fmt.Errorf("ykc: 数据长度非法: %d", dataLen)
	}
	return c.HeaderLen() + dataLen + 2, nil
}

// Encode 组上行帧。序列号按上行字节序写入，CRC 低字节在前。
func (c *Codec) Encode(seq uint16, encrypt byte, typ FrameType, body []byte) []byte {
	return c.build(orderBytes(seq, upSeqOrder), encrypt, typ, body, time.Time{})
}

// EncodeEcho 组上行帧，序列号按接收到的原始字节原样回显（用于对时应答等需要对齐序列号的场景）。
func (c *Codec) EncodeEcho(seqRaw [2]byte, encrypt byte, typ FrameType, body []byte) []byte {
	return c.build(seqRaw[:], encrypt, typ, body, time.Time{})
}

func (c *Codec) build(seq []byte, encrypt byte, typ FrameType, body []byte, evt time.Time) []byte {
	timeLen := 0
	if c.withTime {
		timeLen = 7
	}
	dataLen := 2 + timeLen + 2 + len(body)
	raw := make([]byte, 0, c.HeaderLen()+dataLen+2)
	raw = append(raw, StartFlag)
	if c.lenBytes == 1 {
		raw = append(raw, byte(dataLen))
	} else {
		raw = append(raw, byte(dataLen), byte(dataLen>>8))
	}
	raw = append(raw, seq...)
	if c.withTime {
		if evt.IsZero() {
			evt = time.Now()
		}
		raw = append(raw, EncodeCP56(evt)...)
	}
	raw = append(raw, encrypt, typ)
	raw = append(raw, body...)

	data := raw[c.HeaderLen():]
	crc := CRC16(data)
	raw = append(raw, byte(crc), byte(crc>>8))
	return raw
}

// Decode 解析一帧完整报文（通常由 Framer 从流中截取后传入）。
func (c *Codec) Decode(raw []byte) (*Frame, error) {
	total, err := c.FrameLen(raw)
	if err != nil {
		return nil, err
	}
	if len(raw) != total {
		return nil, fmt.Errorf("%w: 帧长度不匹配，实际 %d 期望 %d", ErrShort, len(raw), total)
	}
	dataStart := c.HeaderLen()
	data := raw[dataStart : dataStart+total-c.HeaderLen()-2]
	crc := uint16(raw[total-2]) | uint16(raw[total-1])<<8
	if !CheckCRC(data, crc) {
		return nil, errors.New("ykc: CRC 校验未通过")
	}

	r := NewR(data)
	f := &Frame{Version: c.version, Raw: raw}
	copy(f.SeqRaw[:], data[:2])
	f.Sequence = c.readSeq(r)
	if c.withTime {
		f.EventTime = r.Time7()
	}
	f.Encrypt = r.U8()
	f.Type = r.U8()
	f.Body = r.Raw(r.Remain())
	if r.Err() != nil {
		return nil, r.Err()
	}
	return f, nil
}

func (c *Codec) readSeq(r *R) uint16 {
	if downSeqOrder {
		return r.U16BE()
	}
	return r.U16LE()
}

// orderBytes 按 bigEndian 标志序列化序列号。
func orderBytes(v uint16, bigEndian bool) []byte {
	if bigEndian {
		return []byte{byte(v >> 8), byte(v)}
	}
	return []byte{byte(v), byte(v >> 8)}
}
