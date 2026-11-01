// Package ykc 实现云快充（YunKuaiChong）桩侧协议的编解码原语：
// BCD 码、BIN 小端码、CP56Time2a 时间、CRC16-Modbus 校验。
//
// 协议约定（与服务端 vcpp 实现逐字节对齐）：
//   - BIN 整数统一为小端（低位在前）
//   - BCD 码高位在前，不足补 0
//   - 电压/电流 2B：报文值 × 0.1
//   - 电量/金额 4B：报文值 × 0.0001
//   - 单价 4B：报文值 × 0.00001
//   - 温度 1B：报文值 - 50
package ykc

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// ---------------- BCD ----------------

// BcdEncode 数字字符串转 BCD 字节（高位在前）。
// 长度为奇数时左侧补 0；超出 size 时截断右侧对齐（保留低位数字），不足时左侧补 0。
func BcdEncode(digits string, size int) []byte {
	out := make([]byte, size)
	if size == 0 || digits == "" {
		return out
	}
	clean := make([]byte, 0, len(digits))
	for i := 0; i < len(digits); i++ {
		if digits[i] >= '0' && digits[i] <= '9' {
			clean = append(clean, digits[i])
		}
	}
	if len(clean) == 0 {
		return out
	}
	// 右对齐：数字串末位对齐字节数组末字节
	if len(clean) > size*2 {
		clean = clean[len(clean)-size*2:]
	}
	if len(clean)%2 == 1 {
		clean = append([]byte{'0'}, clean...)
	}
	bcd := make([]byte, len(clean)/2)
	for i := range bcd {
		bcd[i] = byte((clean[2*i]-'0')<<4 | (clean[2*i+1] - '0'))
	}
	copy(out[size-len(bcd):], bcd)
	return out
}

// BcdDecode BCD 字节转数字字符串
func BcdDecode(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, v := range b {
		sb.WriteByte('0' + (v>>4)&0x0F)
		sb.WriteByte('0' + v&0x0F)
	}
	return sb.String()
}

// BcdDecodeTrim BCD 字节转数字字符串并去除前导 0
func BcdDecodeTrim(b []byte) string {
	return strings.TrimLeft(BcdDecode(b), "0")
}

// ---------------- BIN（小端） ----------------

// PutU16LE 写入 uint16（小端）
func PutU16LE(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b, v)
}

// PutU32LE 写入 uint32（小端）
func PutU32LE(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b, v)
}

// PutU64LE 写入 uint64（小端）
func PutU64LE(b []byte, v uint64) {
	binary.LittleEndian.PutUint64(b, v)
}

// U16LE 读取 uint16（小端）
func U16LE(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

// U32LE 读取 uint32（小端）
func U32LE(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

// U64LE 读取 uint64（小端）
func U64LE(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// U16BE 读取 uint16（大端）
func U16BE(b []byte) uint16 { return binary.BigEndian.Uint16(b) }

// Bin5ToUint64 5 字节小端无符号数（电表起止值）
func Bin5ToUint64(b []byte) uint64 {
	var v uint64
	for i := 0; i < len(b) && i < 5; i++ {
		v |= uint64(b[i]) << (8 * i)
	}
	return v
}

// Uint64ToBin5 uint64 转 5 字节小端
func Uint64ToBin5(v uint64) []byte {
	b := make([]byte, 5)
	for i := 0; i < 5; i++ {
		b[i] = byte(v >> (8 * i))
	}
	return b
}

// ---------------- 工程量换算 ----------------

// WireVoltage 电压/电流报文值 → 实际值（×0.1）
func WireVoltage(v uint16) float64 { return float64(v) / 10 }

// ToWireVoltage 实际值 → 报文值
func ToWireVoltage(f float64) uint16 { return uint16(f * 10) }

// WireQuantity 电量/金额报文值 → 实际值（×0.0001）
func WireQuantity(v uint32) float64 { return float64(v) / 10000 }

// ToWireQuantity 实际值 → 报文值
func ToWireQuantity(f float64) uint32 { return uint32(f * 10000) }

// WirePrice 单价报文值 → 实际值（×0.00001）
func WirePrice(v uint32) float64 { return float64(v) / 100000 }

// ToWirePrice 实际值 → 报文值
func ToWirePrice(f float64) uint32 { return uint32(f * 100000) }

// WireTemp 温度报文值 → 实际值（-50 偏移）
func WireTemp(v byte) int { return int(v) - 50 }

// ToWireTemp 实际值 → 报文值
func ToWireTemp(t int) byte { return byte(t + 50) }

// ---------------- CP56Time2a（7 字节） ----------------

// EncodeCP56 编码 CP56Time2a。
// 布局：ms(2B LE) + 分 + 时 + 日 + 月 + 年(后两位)，与服务端的实现保持一致。
func EncodeCP56(t time.Time) []byte {
	b := make([]byte, 7)
	ms := t.Second()*1000 + t.Nanosecond()/1_000_000
	b[0] = byte(ms & 0xFF)
	b[1] = byte((ms >> 8) & 0xFF)
	b[2] = byte(t.Minute() & 0x3F)
	b[3] = byte(t.Hour() & 0x1F)
	b[4] = byte(t.Day() & 0x1F)
	b[5] = byte(t.Month() & 0x0F)
	b[6] = byte(t.Year() % 100)
	return b
}

// DecodeCP56 解码 CP56Time2a
func DecodeCP56(b []byte) (time.Time, error) {
	if len(b) < 7 {
		return time.Time{}, fmt.Errorf("CP56Time2a 长度不足: %d", len(b))
	}
	ms := int(b[0]) | int(b[1])<<8
	if ms > 59999 {
		return time.Time{}, fmt.Errorf("CP56Time2a 毫秒越界: %d", ms)
	}
	sec := ms / 1000
	nsec := (ms % 1000) * 1_000_000
	minute := int(b[2] & 0x3F)
	hour := int(b[3] & 0x1F)
	day := int(b[4] & 0x1F)
	month := int(b[5] & 0x0F)
	year := 2000 + int(b[6]&0x7F)
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("CP56Time2a 日期非法: %d-%d-%d", year, month, day)
	}
	return time.Date(year, time.Month(month), day, hour, minute, sec, nsec, time.Local), nil
}

// ---------------- CRC16-Modbus ----------------

// CRC16Modbus 计算 CRC16-Modbus（多项式 0xA001，初值 0xFFFF，输入输出均反转）。
// 返回值与 Java Hutool CRC16Modbus#getValue 的「大端值」一致，
// 写入报文时需要按低字节在前输出。
func CRC16Modbus(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// ToHex 字节转大写 HEX 字符串
func ToHex(b []byte) string {
	const digits = "0123456789ABCDEF"
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, v := range b {
		sb.WriteByte(digits[v>>4])
		sb.WriteByte(digits[v&0x0F])
	}
	return sb.String()
}
