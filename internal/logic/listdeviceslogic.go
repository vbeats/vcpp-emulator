// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package logic

import (
	"context"

	"github.com/csf/vcpp-emulator/internal/svc"
	"github.com/csf/vcpp-emulator/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListDevicesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListDevicesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListDevicesLogic {
	return &ListDevicesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListDevicesLogic) ListDevices() (resp *types.DeviceListResponse, err error) {
	list := l.svcCtx.Manager.Status()
	devices := make([]types.DeviceStatus, 0, len(list))
	for _, d := range list {
		guns := make([]types.GunStatus, 0, len(d.Guns))
		for _, g := range d.Guns {
			guns = append(guns, types.GunStatus{
				GunNo:      g.GunNo,
				Charging:   g.Charging,
				TradeNo:    g.TradeNo,
				StartAt:    g.StartAt,
				ElapsedSec: g.ElapsedSec,
				Soc:        g.Soc,
				VoltageV:   g.VoltageV,
				CurrentA:   g.CurrentA,
				EnergyKwh:  g.EnergyKwh,
				AmountYuan: g.AmountYuan,
				StoppedBy:  g.StoppedBy,
			})
		}
		devices = append(devices, types.DeviceStatus{
			DeviceNo:     d.DeviceNo,
			GunCount:     d.GunCount,
			Running:      d.Running,
			Connected:    d.Connected,
			Stage:        d.Stage,
			Server:       d.Server,
			Protocol:     d.Protocol,
			BillingModel: d.BillingModel,
			Guns:         guns,
		})
	}
	return &types.DeviceListResponse{
		Code:    0,
		Msg:     "ok",
		Server:  l.svcCtx.Manager.Server(),
		Devices: devices,
	}, nil
}
