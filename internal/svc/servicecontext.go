// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package svc

import (
	"context"
	"fmt"

	"github.com/csf/vcpp-emulator/internal/config"
	"github.com/csf/vcpp-emulator/internal/sim"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config  config.Config
	Manager *sim.Manager
	Root    context.Context
}

func NewServiceContext(c config.Config, root context.Context) *ServiceContext {
	mgr, err := sim.NewManager(&c)
	if err != nil {
		logx.Must(fmt.Errorf("创建模拟器失败: %w", err))
	}
	return &ServiceContext{
		Config:  c,
		Manager: mgr,
		Root:    root,
	}
}
