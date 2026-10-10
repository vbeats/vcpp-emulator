package transport

import (
	"bufio"
	"errors"
	"fmt"

	"github.com/csf/vcpp-emulator/internal/protocol"
	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
	"github.com/zeromicro/go-zero/core/logx"
)

// ReadBufSize 读缓冲大小，需大于最大帧长度，否则 bufio.Peek 会返回 ErrBufferFull。
const ReadBufSize = 1 << 17

// Framer 从 TCP 字节流中截取完整帧，处理粘包与半包。
type Framer struct {
	r          *bufio.Reader
	codec      protocol.Codec
	discarded  uint64
	resyncWarn bool
}

// NewFramer 构造拆帧器。
func NewFramer(r *bufio.Reader, codec protocol.Codec) *Framer {
	return &Framer{r: r, codec: codec}
}

// Next 阻塞读取下一帧；连接关闭时返回 io.EOF。
func (f *Framer) Next() (*ykc.Frame, error) {
	for {
		head, err := f.r.Peek(f.codec.HeaderLen())
		if err != nil {
			return nil, err
		}
		if head[0] != ykc.StartFlag {
			f.skip(1, "起始符非 0x68，丢弃 1 字节重新同步")
			continue
		}
		total, err := f.codec.FrameLen(head)
		if err != nil {
			if errors.Is(err, ykc.ErrShort) {
				return nil, err
			}
			f.skip(1, fmt.Sprintf("帧头非法(%v)，丢弃 1 字节重新同步", err))
			continue
		}
		peeked, err := f.r.Peek(total)
		if err != nil {
			// 半包：返回错误由上层等待更多数据（连接关闭时为 io.EOF）
			return nil, err
		}
		// Peek 返回的是读缓冲内部的切片，Discard 后会被后续数据覆盖；
		// 帧会异步交给 Actor 处理，必须先拷贝出来，否则会读到脏数据。
		raw := make([]byte, total)
		copy(raw, peeked)

		frame, err := f.codec.Decode(raw)
		if err != nil {
			f.skip(1, fmt.Sprintf("解码失败(%v)，丢弃 1 字节重新同步", err))
			continue
		}
		if _, err = f.r.Discard(total); err != nil {
			return nil, err
		}
		if f.discarded > 0 {
			logx.Errorf("拆帧过程中共丢弃 %d 个异常字节", f.discarded)
			f.discarded = 0
		}
		return frame, nil
	}
}

func (f *Framer) skip(n int, reason string) {
	if _, err := f.r.Discard(n); err != nil {
		return
	}
	f.discarded += uint64(n)
	if !f.resyncWarn {
		f.resyncWarn = true
		logx.Errorf("ykc 拆帧：%s", reason)
	}
}
