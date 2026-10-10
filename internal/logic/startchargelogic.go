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

// StartCharge 手动触发指定枪启机，等价于桩侧收到一次远程启机指令。
func (l *StartChargeLogic) StartCharge(req *types.GunRequest) (resp *types.BaseResponse, err error) {
	if req.DeviceNo == "" {
		return &types.BaseResponse{Code: 1, Msg: "deviceNo 不能为空"}, nil
	}
	gun := req.GunNo
	if gun <= 0 {
		gun = 1
	}
	if err = l.svcCtx.Manager.TriggerStart(req.DeviceNo, gun); err != nil {
		return &types.BaseResponse{Code: 1, Msg: err.Error()}, nil
	}
	return &types.BaseResponse{Code: 0, Msg: "ok"}, nil
}
