package ykc

import (
	"fmt"
	"time"
)

// CP56Time2a 为 7 字节：毫秒(2B, LE) + 分 + 时 + 日(高3位为星期) + 月 + 年(2000 起)。
// V1.7 用于 0x56 对时、0x55 对时应答、0x3D 交易记录的时间字段。

// EncodeCP56 编码为 7 字节 CP56Time2a。
func EncodeCP56(t time.Time) []byte {
	b := make([]byte, 7)
	ms := uint16(t.Nanosecond() / int(time.Millisecond))
	b[0] = byte(ms)
	b[1] = byte(ms >> 8)
	b[2] = byte(t.Minute())
	b[3] = byte(t.Hour())
	b[4] = byte(t.Day())
	b[5] = byte(t.Month())
	b[6] = byte(t.Year() - 2000)
	return b
}

// DecodeCP56 解码 7 字节 CP56Time2a。
func DecodeCP56(b []byte) (time.Time, error) {
	if len(b) != 7 {
		return time.Time{}, fmt.Errorf("%w: CP56Time2a 需要 7 字节，收到 %d", ErrShort, len(b))
	}
	ms := uint16(b[0]) | uint16(b[1])<<8
	year := 2000 + int(b[6]&0x7F)
	month := time.Month(b[5] & 0x0F)
	day := int(b[4] & 0x1F)
	hour := int(b[3] & 0x1F)
	minute := int(b[2] & 0x3F)
	return time.Date(year, month, day, hour, minute, 0, int(ms)*int(time.Millisecond), time.Local), nil
}
