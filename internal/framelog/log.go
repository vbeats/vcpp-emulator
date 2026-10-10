package framelog

import (
	"fmt"
	"strings"

	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
	"github.com/zeromicro/go-zero/core/logx"
)

// Logger 通信日志：方向 + 十六进制原文 + 字段解析结果。
type Logger struct {
	Hex    bool // 是否打印十六进制原文
	Parsed bool // 是否打印字段解析结果
}

// Frame 打印一帧通信报文。
// dir=DirUp 表示上行（⬆️ 模拟器 -> server），dir=DirDown 表示下行（⬇️ server -> 模拟器）。
func (l *Logger) Frame(dir ykc.Direction, version, deviceNo string, gun byte, typ ykc.FrameType, raw []byte, parsed string) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("【云快充】%s 桩号:%s 枪号:%02d %s %s（0x%02X）",
		version, deviceNo, gun, dir.String(), ykc.FrameName(typ), typ))
	if l.Hex && len(raw) > 0 {
		sb.WriteString("\nHEX: ")
		sb.WriteString(Hex(raw))
	}
	if l.Parsed && parsed != "" {
		sb.WriteString("\n解析: ")
		sb.WriteString(parsed)
	}
	sb.WriteString("\n---------------------------------")
	logx.Info(sb.String())
}

// Event 打印与帧无关的状态日志。
func (l *Logger) Event(deviceNo string, gun byte, format string, args ...any) {
	logx.Info(fmt.Sprintf("【模拟器】桩号:%s 枪号:%02d %s", deviceNo, gun, fmt.Sprintf(format, args...)))
}

// Eventf 打印桩级别的状态日志。
func (l *Logger) Eventf(deviceNo, format string, args ...any) {
	logx.Info(fmt.Sprintf("【模拟器】桩号:%s %s", deviceNo, fmt.Sprintf(format, args...)))
}

// Hex 输出大写、按字节空格分隔的十六进制串。
func Hex(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) * 3)
	for i, v := range b {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(fmt.Sprintf("%02X", v))
	}
	return sb.String()
}
