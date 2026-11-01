package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"vcpp-emulator/internal/emu"
	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
	"vcpp-emulator/internal/ykc"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 前端事件名，需与 main.go 中 RegisterEvent 注册的名称一致
const (
	EventState = "state" // 桩状态快照
	EventLog   = "log"   // 单条日志
)

// App 应用服务层，向前端暴露配置与模拟器控制能力
type App struct {
	wailsApp *application.App
	logBus   *logger.Bus
	store    *store.Store
	mgr      *emu.Manager
	logDir   string
}

// setApp 注入应用实例用于事件推送，小写方法不参与前端绑定
func (a *App) setApp(app *application.App) {
	a.wailsApp = app
}

// ServiceStartup 服务启动初始化（wails v3 生命周期钩子）
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	// 日志文件位于应用安装目录下的 logs 目录
	dir := appDir()
	a.logDir = filepath.Join(dir, "logs")

	logBus, err := logger.New(a.logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "日志初始化失败: %v\n", err)
		logBus, _ = logger.New(filepath.Join(os.TempDir(), "vcpp-emulator", "logs"))
		a.logDir = filepath.Join(os.TempDir(), "vcpp-emulator", "logs")
	}
	a.logBus = logBus
	a.logBus.SetEmitter(func(e logger.Entry) {
		if a.wailsApp != nil {
			a.wailsApp.Event.Emit(EventLog, e)
		}
	})

	st, err := store.New(filepath.Join(dir, "data"))
	if err != nil {
		a.logBus.Errorf("配置初始化失败: %v", err)
		st, _ = store.New(filepath.Join(os.TempDir(), "vcpp-emulator", "data"))
	}
	a.store = st
	// 与服务端「缺值回退默认、越界钳制」一致：这里把配置来源与修正项落到日志，便于排障
	a.logBus.Infof("已加载配置文件: %s", st.Path())
	for _, n := range st.Notes() {
		a.logBus.Warnf("配置修正: %s", n)
	}

	a.mgr = emu.NewManager(a.logBus, st, func() {
		if a.wailsApp != nil {
			a.wailsApp.Event.Emit(EventState, a.mgr.Snapshot())
		}
	})

	a.logBus.Infof("充电桩模拟器启动，日志目录: %s", a.logDir)
	cfg := st.Get()
	a.mgr.Apply(cfg)
	// 默认不自动连接：启动后保持未连接，由用户在界面手动连接，避免配好服务端就直连现场平台
	if !cfg.AutoConnect {
		a.logBus.Infof("未开启自动连接，启动后保持未连接状态，请在界面手动连接服务端")
	}
	return nil
}

// ServiceShutdown 服务退出，释放全部连接与协程（wails v3 生命周期钩子）。
// 窗口关闭 / 进程退出时由框架保证回调，这里逐个销毁已建立的 TCP 连接后再关闭日志。
func (a *App) ServiceShutdown() error {
	if a.mgr != nil {
		a.mgr.Shutdown()
	}
	if a.logBus != nil {
		a.logBus.Infof("TCP 连接已全部销毁，充电桩模拟器退出")
		a.logBus.Close()
	}
	return nil
}

// ---------------- 配置 ----------------

// VersionOption 协议版本选项
type VersionOption struct {
	Code string `json:"code"`
	Desc string `json:"desc"`
	Port int    `json:"port"`
}

// GetVersions 获取支持的协议版本与默认端口
func (a *App) GetVersions() []VersionOption {
	specs := ykc.AllSpecs()
	out := make([]VersionOption, 0, len(specs))
	for _, s := range specs {
		out = append(out, VersionOption{Code: string(s.Code), Desc: s.Desc, Port: s.DefaultPort})
	}
	return out
}

// GetConfig 获取当前配置
func (a *App) GetConfig() store.AppConfig {
	return a.store.Get()
}

// SaveConfig 保存配置并重新应用到模拟器
func (a *App) SaveConfig(cfg store.AppConfig) error {
	if err := a.store.Save(cfg); err != nil {
		a.logBus.Errorf("配置保存失败: %v", err)
		return err
	}
	a.logBus.Infof("配置已保存：%s:%d %s，桩数量 %d",
		cfg.Server.Host, cfg.Server.Port, cfg.Server.Version, len(cfg.Piles))
	a.mgr.Apply(cfg)
	return nil
}

// GetLogDir 获取日志文件目录
func (a *App) GetLogDir() string { return a.logDir }

// GetConfigPath 获取当前生效的配置文件路径（首次运行会生成默认配置文件）
func (a *App) GetConfigPath() string { return a.store.Path() }

// ---------------- 桩管理 ----------------

// GetPiles 获取全部桩状态
func (a *App) GetPiles() []emu.PileView { return a.mgr.Snapshot() }

// StartPile 启动指定桩
func (a *App) StartPile(id string) error { return a.mgr.Start(id) }

// StopPile 停止指定桩
func (a *App) StopPile(id string) error { return a.mgr.Stop(id) }

// StartAll 启动全部桩
func (a *App) StartAll() { a.mgr.StartAll() }

// StopAll 停止全部桩
func (a *App) StopAll() { a.mgr.StopAll() }

// AddPile 新增充电桩
func (a *App) AddPile(cfg store.PileConfig) error {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("pile-%d", len(a.store.Get().Piles)+1)
	}
	if cfg.PileNo == "" {
		return fmt.Errorf("桩号不能为空")
	}
	if cfg.GunCount <= 0 {
		cfg.GunCount = 1
	}
	if cfg.RatedPower <= 0 {
		cfg.RatedPower = 120
	}
	if cfg.ProgramVer == "" {
		cfg.ProgramVer = "220422"
	}
	if len(cfg.PileNo) != 14 {
		return fmt.Errorf("桩号必须为 14 位数字")
	}
	cur := a.store.Get()
	for _, p := range cur.Piles {
		if p.ID == cfg.ID {
			return fmt.Errorf("桩 ID 已存在: %s", cfg.ID)
		}
		if p.PileNo == cfg.PileNo {
			return fmt.Errorf("桩号已存在: %s", cfg.PileNo)
		}
	}
	cur.Piles = append(cur.Piles, cfg)
	return a.SaveConfig(cur)
}

// UpdatePile 更新充电桩配置
func (a *App) UpdatePile(cfg store.PileConfig) error {
	cur := a.store.Get()
	for i, p := range cur.Piles {
		if p.ID == cfg.ID {
			cur.Piles[i] = cfg
			return a.SaveConfig(cur)
		}
	}
	return fmt.Errorf("桩不存在: %s", cfg.ID)
}

// RemovePile 删除充电桩
func (a *App) RemovePile(id string) error {
	cur := a.store.Get()
	rest := make([]store.PileConfig, 0, len(cur.Piles))
	for _, p := range cur.Piles {
		if p.ID != id {
			rest = append(rest, p)
		}
	}
	if len(rest) == len(cur.Piles) {
		return fmt.Errorf("桩不存在: %s", id)
	}
	cur.Piles = rest
	return a.SaveConfig(cur)
}

// ---------------- 充电控制 ----------------

// StartCharge 发起充电（全自动模拟整个充电过程）。
// 发起时同步持久化界面填写内容，下次打开「启动充电」时自动回填最近一次录入。
func (a *App) StartCharge(req emu.StartRequest) error {
	cfg := a.store.Get()
	cfg.LastStartCharge = lastStartForm(req)
	if err := a.store.Save(cfg); err != nil {
		// 记忆失败不影响本次充电
		a.logBus.Errorf("充电表单填写内容保存失败: %v", err)
	}
	return a.mgr.StartCharge(trimIdentities(req))
}

// trimIdentities 按启动方式剔除无关身份字段，
// 避免刷卡启动时报文携带 VIN（或反之），这里在完整记忆之后裁剪。
func trimIdentities(req emu.StartRequest) emu.StartRequest {
	switch req.StartType {
	case ykc.StartTypeVin:
		req.CardNo = ""
	case ykc.StartTypeCard:
		req.Vin = ""
	}
	return req
}

// lastStartForm 提取启动请求中属于用户填写的部分
func lastStartForm(req emu.StartRequest) store.StartChargeForm {
	return store.StartChargeForm{
		StartType:  int(req.StartType),
		CardNo:     req.CardNo,
		Vin:        req.Vin,
		BatteryKwh: req.BatteryKwh,
		StartSoc:   req.StartSoc,
		MaxVoltage: req.MaxVoltage,
		MaxCurrent: req.MaxCurrent,
		// emu 与 store 各自定义了 ChargeTarget，这里逐字段转换以保持分层边界
		Target: store.ChargeTarget{
			Mode:   req.Target.Mode,
			Amount: req.Target.Amount,
			Minute: req.Target.Minute,
			Energy: req.Target.Energy,
			Soc:    req.Target.Soc,
		},
	}
}

// StopCharge 停止充电
func (a *App) StopCharge(pileID string, gun int) error {
	return a.mgr.StopCharge(pileID, gun)
}

// SetPlug 插枪 / 拔枪
func (a *App) SetPlug(pileID string, gun int, plugged bool) error {
	return a.mgr.SetPlug(pileID, gun, plugged)
}

// ---------------- 日志 ----------------

// GetLogs 获取最近日志（最多 100 条）
func (a *App) GetLogs() []logger.Entry { return a.logBus.Recent() }

// appDir 应用安装目录（可执行文件所在目录），失败时回退用户配置目录
func appDir() string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		return filepath.Dir(exe)
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "vcpp-emulator")
	}
	return "."
}
