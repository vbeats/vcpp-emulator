package logic

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
)

// parseDeviceNo 把 HTTP 请求里的桩号解析为整型。
// 云快充桩号在线上为 7 字节 BCD，即 14 位十进制数字，内部一律用整型表示。
func parseDeviceNo(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("deviceNo 不能为空")
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 || n > ykc.MaxDeviceNo {
		return 0, fmt.Errorf("deviceNo 必须为 1~14 位数字，收到: %s", s)
	}
	return n, nil
}

// parseGunNo 把 HTTP 请求里的枪号解析为整型。
// 枪号对外用字符串表示（"01" / "02"），与协议里枪号的书写习惯一致；
// 内部仍按整型使用，最终编码成 1 字节。
func parseGunNo(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("gunNo 不能为空")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 255 {
		return 0, fmt.Errorf("gunNo 必须为 1~255 的数字（如 \"01\"），收到: %s", s)
	}
	return n, nil
}
