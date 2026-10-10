// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package handler

import (
	"net/http"

	"github.com/csf/vcpp-emulator/internal/logic"
	"github.com/csf/vcpp-emulator/internal/svc"
	"github.com/csf/vcpp-emulator/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func StartDeviceHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DeviceRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := logic.NewStartDeviceLogic(r.Context(), svcCtx)
		resp, err := l.StartDevice(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
