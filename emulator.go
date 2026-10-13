// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/handler"
	"github.com/csf/vcpp-emulator/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/emulator.yaml", "the config file")

// Windows 下 go-zero 不会关闭 HTTP 监听，必须自己接管：
//  1. core/proc/signals.go 的构建约束是 linux || darwin || freebsd，Windows 不接管信号；
//  2. core/proc/shutdown+polyfill.go（//go:build windows）里 AddShutdownListener 是空实现，
//     而 rest/internal/starter.go 正是通过它注册 server.Shutdown 的；
//  3. rest.Server.Stop() 只做 logx.Close()。
//
// 三者叠加导致 Ctrl+C 后 ListenAndServe 永不返回，进程不退出、端口持续占用。
// 这里用 rest.StartOption 捕获内部的 *http.Server，收到退出信号时自己 Shutdown。
type httpServerHolder struct {
	mu      sync.Mutex
	server  *http.Server
	started chan struct{}
}

func newHTTPServerHolder() *httpServerHolder {
	return &httpServerHolder{started: make(chan struct{})}
}

// capture 作为 rest.StartOption 传给 StartWithOpts，在 ListenAndServe 之前执行。
func (h *httpServerHolder) capture(s *http.Server) {
	h.mu.Lock()
	h.server = s
	h.mu.Unlock()
	select {
	case <-h.started:
	default:
		close(h.started)
	}
}

// Load 返回已捕获的 *http.Server，未就绪时返回 nil。
func (h *httpServerHolder) Load() *http.Server {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.server
}

// Shutdown 优雅关闭 HTTP 服务。
func (h *httpServerHolder) Shutdown(timeout time.Duration) {
	// 等待捕获完成，避免信号早于 ListenAndServe 到达时拿不到 server
	select {
	case <-h.started:
	case <-time.After(timeout):
		logx.Errorf("等待 HTTP 服务就绪超时，放弃优雅关闭")
		return
	}
	s := h.Load()
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		logx.Errorf("关闭 HTTP 服务失败: %v", err)
	}
}

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	if err := c.Validate(); err != nil {
		logx.Must(fmt.Errorf("配置校验失败: %w", err))
	}
	logx.MustSetup(c.Log)
	logx.DisableStat()

	root, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx := svc.NewServiceContext(c, root)

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()
	handler.RegisterHandlers(server, ctx)

	n := ctx.Manager.Start(root)
	logx.Infof("模拟器已启动 %d/%d 个桩，server=%s 协议=%s %s，HTTP 控制面 %s:%d",
		n, len(c.Devices), c.Addr(), c.Protocol.Type, c.Protocol.Version, c.Host, c.Port)
	fmt.Printf("Starting emulator at %s:%d, server %s...\n", c.Host, c.Port, c.Addr())

	holder := newHTTPServerHolder()

	go func() {
		<-root.Done()
		// 立刻释放信号监听，第二次 Ctrl+C 走系统默认行为强制退出
		stop()
		logx.Info("收到退出信号，正在停止模拟器...")
		ctx.Manager.StopAll()
		holder.Shutdown(5 * time.Second)
	}()

	server.StartWithOpts(holder.capture)
	logx.Info("模拟器已退出")
}
