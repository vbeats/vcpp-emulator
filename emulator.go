// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package main

import (
	"context"
	"flag"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/handler"
	"github.com/csf/vcpp-emulator/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/emulator.yaml", "the config file")

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

	go func() {
		<-root.Done()
		logx.Info("收到退出信号，正在停止模拟器...")
		ctx.Manager.StopAll()
		server.Stop()
	}()

	server.Start()
	logx.Info("模拟器已退出")
}
