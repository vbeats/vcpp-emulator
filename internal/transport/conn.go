package transport

import (
	"bufio"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/framelog"
	"github.com/csf/vcpp-emulator/internal/protocol"
	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
	"github.com/zeromicro/go-zero/core/logx"
)

// SendBufSize 发送队列缓冲；写满时丢弃并计数，避免 server 假死导致内存膨胀。
const SendBufSize = 256

// Conn 管理与 server 端的单条 TCP 长连接：读循环、写队列、断线重连。
type Conn struct {
	cfg      *config.Config
	codec    protocol.Codec
	logger   *framelog.Logger
	deviceNo uint64
	onFrame  func(*ykc.Frame)
	onState  func(connected bool)

	mu     sync.Mutex
	nc     net.Conn
	sendCh chan []byte

	enabled atomic.Bool
	dropped atomic.Uint64
}

// NewConn 构造连接管理器。
func NewConn(cfg *config.Config, codec protocol.Codec, logger *framelog.Logger,
	deviceNo uint64, onFrame func(*ykc.Frame), onState func(connected bool)) *Conn {
	c := &Conn{
		cfg:      cfg,
		codec:    codec,
		logger:   logger,
		deviceNo: deviceNo,
		onFrame:  onFrame,
		onState:  onState,
	}
	c.enabled.Store(true)
	return c
}

// SetEnabled 控制是否允许发起/保持连接（停止桩或进程退出时使用）。
func (c *Conn) SetEnabled(v bool) {
	c.enabled.Store(v)
	if !v {
		c.Close()
	}
}

// Run 运行重连循环，直到 ctx 取消。
func (c *Conn) Run(ctx context.Context) {
	interval := time.Duration(c.cfg.Timing.ReconnectSec) * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if !c.enabled.Load() {
			c.wait(ctx, interval)
			continue
		}
		if err := c.serve(ctx); err != nil {
			if ctx.Err() == nil {
				logx.Errorf("【模拟器】桩号:%014d 连接中断: %v，%s 后重连", c.deviceNo, err, interval)
			}
		}
		c.wait(ctx, interval)
	}
}

func (c *Conn) wait(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// serve 拨号并服务一条连接，直到连接断开。
func (c *Conn) serve(ctx context.Context) error {
	addr := c.cfg.Addr()
	dialer := net.Dialer{Timeout: time.Duration(c.cfg.Timing.DialTimeoutSec) * time.Second}
	nc, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	sendCh := make(chan []byte, SendBufSize)
	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }

	// ctx 取消时关闭 socket，令读写循环退出
	go func() {
		select {
		case <-ctx.Done():
			_ = nc.Close()
		case <-done:
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.writeLoop(nc, sendCh, done)
	}()

	c.mu.Lock()
	c.nc = nc
	c.sendCh = sendCh
	c.mu.Unlock()

	c.logger.Eventf(c.deviceNo, "已连接 server %s", addr)
	c.onState(true)

	rErr := c.readLoop(nc)
	finish()
	wg.Wait()
	_ = nc.Close()

	c.mu.Lock()
	c.nc = nil
	c.sendCh = nil
	c.mu.Unlock()
	c.onState(false)
	c.logger.Eventf(c.deviceNo, "与 server %s 的连接已断开", addr)
	return rErr
}

func (c *Conn) readLoop(nc net.Conn) error {
	framer := NewFramer(bufio.NewReaderSize(nc, ReadBufSize), c.codec)
	for {
		frame, err := framer.Next()
		if err != nil {
			return err
		}
		c.onFrame(frame)
	}
}

func (c *Conn) writeLoop(nc net.Conn, ch <-chan []byte, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case data := <-ch:
			if _, err := nc.Write(data); err != nil {
				logx.Errorf("【模拟器】桩号:%014d 发送失败: %v", c.deviceNo, err)
				return
			}
		}
	}
}

// Send 把整帧放入写队列，非阻塞；队列满时丢弃并返回 false。
func (c *Conn) Send(data []byte) bool {
	c.mu.Lock()
	ch := c.sendCh
	c.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- data:
		return true
	default:
		n := c.dropped.Add(1)
		logx.Errorf("【模拟器】桩号:%014d 发送队列已满，丢弃 %d 字节（累计丢弃 %d 次）",
			c.deviceNo, len(data), n)
		return false
	}
}

// Close 主动断开当前连接（心跳超时等场景）。
func (c *Conn) Close() {
	c.mu.Lock()
	nc := c.nc
	c.mu.Unlock()
	if nc != nil {
		_ = nc.Close()
	}
}
