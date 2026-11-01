package main

import (
	"embed"
	"log"

	"vcpp-emulator/internal/emu"
	"vcpp-emulator/internal/gas"
	"vcpp-emulator/internal/logger"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails 使用 Go 的 embed 包将前端产物嵌入二进制，frontend/dist 下的全部文件都会被打包。
//
//go:embed all:frontend/dist
var assets embed.FS

// 注册自定义事件，绑定生成器会据此生成强类型的前端事件 API。
// 注意：RegisterEvent 只应在 init 中以常量参数直接调用，否则绑定生成器无法识别。
func init() {
	application.RegisterEvent[logger.Entry]("log")
	application.RegisterEvent[[]emu.PileView]("state")
	application.RegisterEvent[[]gas.DispenserView]("gasState")
	application.RegisterEvent[gas.PushMessage]("gasPush")
}

func main() {
	svc := &App{}

	app := application.New(application.Options{
		Name:        "设备模拟器",
		Description: "云快充协议充电桩 / 加油机设备模拟器",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// 事件推送依赖应用实例，需在 Run 之前注入
	svc.setApp(app)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "设备模拟器 · vcpp-emulator",
		Width:            1728,
		Height:           1080,
		MinWidth:         1100,
		MinHeight:        700,
		BackgroundColour: application.NewRGB(238, 242, 248),
		URL:              "/",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	})

	// Run 会阻塞直到应用退出
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
