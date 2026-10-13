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

// StopCharge 远程停机：按桩号 + 枪号终止充电，会走中止帧与交易记录上送。
func (l *StopChargeLogic) StopCharge(req *types.GunRequest) (resp *types.BaseResponse, err error) {
	no, perr := parseDeviceNo(req.DeviceNo)
	if perr != nil {
		return &types.BaseResponse{Code: 1, Msg: perr.Error()}, nil
	}
	gun, gerr := parseGunNo(req.GunNo)
	if gerr != nil {
		return &types.BaseResponse{Code: 1, Msg: gerr.Error()}, nil
	}
	if err = l.svcCtx.Manager.TriggerStop(no, gun); err != nil {
		return &types.BaseResponse{Code: 1, Msg: err.Error()}, nil
	}
	return &types.BaseResponse{Code: 0, Msg: "ok"}, nil
}
