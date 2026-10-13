package ykc

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 定点精度：协议中电量/金额保留 4 位小数，费率/单价保留 5 位小数，电压电流保留 1 位小数。
const (
	Scale1 = 10     // 电压 V / 电流 A
	Scale2 = 100    // 单体电压 V
	Scale4 = 10000  // 电量 kWh / 金额 元
	Scale5 = 100000 // 费率 / 单价 元
)

var ErrShort = errors.New("ykc: 字节不足")

// W 按协议顺序写入字节的只进游标。
type W struct {
	buf []byte
}

func NewW(n int) *W { return &W{buf: make([]byte, 0, n)} }

func (w *W) Bytes() []byte { return w.buf }
func (w *W) Len() int      { return len(w.buf) }

func (w *W) U8(v byte) { w.buf = append(w.buf, v) }

func (w *W) U16LE(v uint16) { w.buf = append(w.buf, byte(v), byte(v>>8)) }

func (w *W) U16BE(v uint16) { w.buf = append(w.buf, byte(v>>8), byte(v)) }

func (w *W) U24LE(v uint32) { w.buf = append(w.buf, byte(v), byte(v>>8), byte(v>>16)) }

func (w *W) U32LE(v uint32) { w.buf = append(w.buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }

func (w *W) U40LE(v uint64) {
	for i := 0; i < 5; i++ {
		w.buf = append(w.buf, byte(v>>(8*i)))
	}
}

func (w *W) U64LE(v uint64) {
	for i := 0; i < 8; i++ {
		w.buf = append(w.buf, byte(v>>(8*i)))
	}
}

func (w *W) Raw(b []byte) { w.buf = append(w.buf, b...) }

func (w *W) Zero(n int) {
	for i := 0; i < n; i++ {
		w.buf = append(w.buf, 0)
	}
}

// BCD 写入 n 字节 BCD。数字串左侧补 0，超长时保留右侧 2n 位。
func (w *W) BCD(digits string, n int) {
	digits = strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			return r
		}
		return -1
	}, digits)
	for len(digits) < 2*n {
		digits = "0" + digits
	}
	if len(digits) > 2*n {
		digits = digits[len(digits)-2*n:]
	}
	for i := 0; i < n; i++ {
		w.buf = append(w.buf, hexVal(digits[2*i])<<4|hexVal(digits[2*i+1]))
	}
}

// ASCII 写入定长 ASCII，不足补 0。
func (w *W) ASCII(s string, n int) {
	b := make([]byte, n)
	copy(b, s)
	w.buf = append(w.buf, b...)
}

// Temp 写入温度，协议偏移 -50。
func (w *W) Temp(celsius int) {
	v := celsius + 50
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	w.buf = append(w.buf, byte(v))
}

// Time7 写入 CP56Time2a 时间。
func (w *W) Time7(t time.Time) { w.Raw(EncodeCP56(t)) }

// ---------------- 桩号 ----------------

// 云快充桩号在线上为 7 字节 BCD，即 14 位十进制数字，故内存一律用 uint64 表示。
const (
	DeviceNoDigits = 14
	MaxDeviceNo    = 99999999999999 // 14 位最大值
)

// FormatDeviceNo 把桩号格式化为 14 位数字串（左侧补 0），用于 BCD 编码。
func FormatDeviceNo(v uint64) string {
	if v > MaxDeviceNo {
		v %= MaxDeviceNo + 1
	}
	return fmt.Sprintf("%0*d", DeviceNoDigits, v)
}

// ParseDeviceNo 把 BCD 读出的数字串解析为桩号，非法内容返回 0。
func ParseDeviceNo(s string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil || n > MaxDeviceNo {
		return 0
	}
	return n
}

// DeviceNo 写入 7 字节 BCD 桩号。
func (w *W) DeviceNo(v uint64) { w.BCD(FormatDeviceNo(v), 7) }

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// R 按协议顺序读取字节的只进游标。
type R struct {
	b   []byte
	i   int
	err error
}

func NewR(b []byte) *R { return &R{b: b} }

func (r *R) Err() error { return r.err }

func (r *R) Remain() int {
	if r.err != nil {
		return 0
	}
	return len(r.b) - r.i
}

func (r *R) need(n int) bool {
	if r.err != nil {
		return false
	}
	if n < 0 || r.i+n > len(r.b) {
		r.err = fmt.Errorf("%w: 需要 %d 字节，剩余 %d", ErrShort, n, len(r.b)-r.i)
		return false
	}
	return true
}

func (r *R) U8() byte {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.i]
	r.i++
	return v
}

func (r *R) U16LE() uint16 {
	if !r.need(2) {
		return 0
	}
	v := uint16(r.b[r.i]) | uint16(r.b[r.i+1])<<8
	r.i += 2
	return v
}

func (r *R) U16BE() uint16 {
	if !r.need(2) {
		return 0
	}
	v := uint16(r.b[r.i])<<8 | uint16(r.b[r.i+1])
	r.i += 2
	return v
}

func (r *R) U24LE() uint32 {
	if !r.need(3) {
		return 0
	}
	v := uint32(r.b[r.i]) | uint32(r.b[r.i+1])<<8 | uint32(r.b[r.i+2])<<16
	r.i += 3
	return v
}

func (r *R) U32LE() uint32 {
	if !r.need(4) {
		return 0
	}
	v := uint32(r.b[r.i]) | uint32(r.b[r.i+1])<<8 | uint32(r.b[r.i+2])<<16 | uint32(r.b[r.i+3])<<24
	r.i += 4
	return v
}

func (r *R) U40LE() uint64 {
	if !r.need(5) {
		return 0
	}
	var v uint64
	for i := 4; i >= 0; i-- {
		v = v<<8 | uint64(r.b[r.i+i])
	}
	r.i += 5
	return v
}

func (r *R) U64LE() uint64 {
	if !r.need(8) {
		return 0
	}
	var v uint64
	for i := 7; i >= 0; i-- {
		v = v<<8 | uint64(r.b[r.i+i])
	}
	r.i += 8
	return v
}

func (r *R) Raw(n int) []byte {
	if !r.need(n) {
		return make([]byte, n)
	}
	v := r.b[r.i : r.i+n]
	r.i += n
	return v
}

// BCD 读取 n 字节 BCD 并按字节顺序拼接为数字串。
func (r *R) BCD(n int) string {
	v := r.Raw(n)
	var sb strings.Builder
	for _, b := range v {
		sb.WriteString(fmt.Sprintf("%02X", b))
	}
	return sb.String()
}

// BCDReversed 读取 n 字节 BCD 并反序拼接（电表表号等"低位在前"字段使用）。
func (r *R) BCDReversed(n int) string {
	v := r.Raw(n)
	var sb strings.Builder
	for i := len(v) - 1; i >= 0; i-- {
		sb.WriteString(fmt.Sprintf("%02X", v[i]))
	}
	return sb.String()
}

// ASCII 读取定长 ASCII，去掉尾部 0x00。
func (r *R) ASCII(n int) string {
	return strings.TrimRight(string(r.Raw(n)), "\x00")
}

// Temp 读取温度，协议偏移 -50。
func (r *R) Temp() int {
	return int(r.U8()) - 50
}

// DeviceNo 读取 7 字节 BCD 桩号并解析为 uint64。
func (r *R) DeviceNo() uint64 { return ParseDeviceNo(r.BCD(7)) }

// Time7 读取 CP56Time2a 时间。
func (r *R) Time7() time.Time {
	t, err := DecodeCP56(r.Raw(7))
	if err != nil && r.err == nil {
		r.err = err
	}
	return t
}

// F1 读取 1 位小数定点数。
func (r *R) F1() float64 { return float64(r.U16LE()) / Scale1 }

// F4 读取 4 位小数定点数。
func (r *R) F4() float64 { return float64(r.U32LE()) / Scale4 }

// F5 读取 5 位小数定点数。
func (r *R) F5() float64 { return float64(r.U32LE()) / Scale5 }
