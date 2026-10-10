package protocol

import (
	"fmt"
	"strings"

	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
)

// Codec 帧编解码器契约：上行组帧、下行解帧、流式取帧长度。
type Codec interface {
	Version() string
	HeaderLen() int
	FrameLen(buf []byte) (int, error)
	Encode(seq uint16, encrypt byte, typ byte, body []byte) []byte
	EncodeEcho(seqRaw [2]byte, encrypt byte, typ byte, body []byte) []byte
	Decode(raw []byte) (*ykc.Frame, error)
}

// New 按协议类型与版本创建编解码器。
// 帧格式、帧类型码、字节序等一律由协议与各版本包自身决定，不接受外部开关。
// 目前仅实现云快充（ykc）；OCPP 只保留入口，待后续扩展。
func New(kind, version string) (Codec, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ykc":
		return ykc.NewCodec(version), nil
	case "ocpp":
		return nil, fmt.Errorf("协议 ocpp 尚未实现，当前仅支持 ykc")
	}
	return nil, fmt.Errorf("未知协议类型: %s", kind)
}
