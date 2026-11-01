package emu

import (
	"testing"

	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
)

// newTestManager 构造指向本机不可用端口的管理器，避免测试期间真实联网
func newTestManager(t *testing.T, autoConnect bool) (*Manager, *store.Store) {
	t.Helper()
	logBus, err := logger.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logBus.Close)

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Default()
	cfg.Server = store.ServerConfig{Host: "127.0.0.1", Port: 1, Version: "V15"}
	cfg.AutoConnect = autoConnect
	cfg.Piles = []store.PileConfig{{
		ID: "pile-1", PileNo: "20260501000001", GunCount: 1,
		PileType: 0, RatedPower: 120, ProgramVer: "220422", Enabled: true,
	}}
	if err := st.Save(cfg); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(logBus, st, func() {})
	t.Cleanup(mgr.Shutdown)
	return mgr, st
}

// TestApplyKeepsOfflineByDefault 默认不自动连接：应用配置同步后桩保持未连接
func TestApplyKeepsOfflineByDefault(t *testing.T) {
	mgr, st := newTestManager(t, false)
	mgr.Apply(st.Get())

	p := mgr.get("pile-1")
	if p == nil {
		t.Fatal("桩未创建")
	}
	if p.IsRunning() || p.IsConnected() {
		t.Fatal("未开启自动连接时不应主动建立连接")
	}
	if got := mgr.Snapshot()[0].Status; got != StatusOffline {
		t.Fatalf("启动后桩状态应为 offline，实际 %s", got)
	}
}

// TestApplyAutoConnectsWhenEnabled 仅当显式开启 auto-connect 时才随配置同步自动连接
func TestApplyAutoConnectsWhenEnabled(t *testing.T) {
	mgr, st := newTestManager(t, true)
	mgr.Apply(st.Get())

	if !mgr.get("pile-1").IsRunning() {
		t.Fatal("开启自动连接后应进入连接流程")
	}
}

// TestManualStartStop 手动连接后停止，应彻底释放协程与连接
func TestManualStartStop(t *testing.T) {
	mgr, st := newTestManager(t, false)
	mgr.Apply(st.Get())

	if err := mgr.Start("pile-1"); err != nil {
		t.Fatal(err)
	}
	if !mgr.get("pile-1").IsRunning() {
		t.Fatal("手动启动后应进入连接流程")
	}
	if err := mgr.Stop("pile-1"); err != nil {
		t.Fatal(err)
	}
	if mgr.get("pile-1").IsRunning() {
		t.Fatal("停止后应释放运行协程")
	}
	if n := mgr.ConnectedCount(); n != 0 {
		t.Fatalf("停止后仍存在 %d 条连接", n)
	}
}
