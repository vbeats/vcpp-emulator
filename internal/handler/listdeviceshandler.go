// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package handler

import (
	"net/http"

	"github.com/csf/vcpp-emulator/internal/logic"
	"github.com/csf/vcpp-emulator/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ListDevicesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewListDevicesLogic(r.Context(), svcCtx)
		resp, err := l.ListDevices()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
