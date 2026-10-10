// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package logic

import (
	"context"

	"github.com/csf/vcpp-emulator/internal/svc"
	"github.com/csf/vcpp-emulator/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StopChargeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStopChargeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StopChargeLogic {
	return &StopChargeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// StopCharge 手动终止指定枪充电，会走中止帧与交易记录上送。
func (l *StopChargeLogic) StopCharge(req *types.GunRequest) (resp *types.BaseResponse, err error) {
	if req.DeviceNo == "" {
		return &types.BaseResponse{Code: 1, Msg: "deviceNo 不能为空"}, nil
	}
	gun := req.GunNo
	if gun <= 0 {
		gun = 1
	}
	if err = l.svcCtx.Manager.TriggerStop(req.DeviceNo, gun); err != nil {
		return &types.BaseResponse{Code: 1, Msg: err.Error()}, nil
	}
	return &types.BaseResponse{Code: 0, Msg: "ok"}, nil
}
