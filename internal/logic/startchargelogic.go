// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package logic

import (
	"context"

	"github.com/csf/vcpp-emulator/internal/svc"
	"github.com/csf/vcpp-emulator/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StartChargeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStartChargeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartChargeLogic {
	return &StartChargeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// StartCharge 远程启机：按桩号 + 枪号启动一次充电，等价于桩侧收到平台的远程启机指令。
func (l *StartChargeLogic) StartCharge(req *types.GunRequest) (resp *types.BaseResponse, err error) {
	no, perr := parseDeviceNo(req.DeviceNo)
	if perr != nil {
		return &types.BaseResponse{Code: 1, Msg: perr.Error()}, nil
	}
	gun, gerr := parseGunNo(req.GunNo)
	if gerr != nil {
		return &types.BaseResponse{Code: 1, Msg: gerr.Error()}, nil
	}
	if err = l.svcCtx.Manager.TriggerStart(no, gun); err != nil {
		return &types.BaseResponse{Code: 1, Msg: err.Error()}, nil
	}
	return &types.BaseResponse{Code: 0, Msg: "ok"}, nil
}
