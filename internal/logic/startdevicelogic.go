// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package logic

import (
	"context"

	"github.com/csf/vcpp-emulator/internal/svc"
	"github.com/csf/vcpp-emulator/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StartDeviceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStartDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartDeviceLogic {
	return &StartDeviceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StartDeviceLogic) StartDevice(req *types.DeviceRequest) (resp *types.BaseResponse, err error) {
	if req.DeviceNo == "" {
		return &types.BaseResponse{Code: 1, Msg: "deviceNo 不能为空"}, nil
	}
	if err = l.svcCtx.Manager.StartDevice(l.svcCtx.Root, req.DeviceNo); err != nil {
		return &types.BaseResponse{Code: 1, Msg: err.Error()}, nil
	}
	return &types.BaseResponse{Code: 0, Msg: "ok"}, nil
}
