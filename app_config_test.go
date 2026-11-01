package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vcpp-emulator/internal/emu"
	"vcpp-emulator/internal/logger"
	"vcpp-emulator/internal/store"
)

// TestSaveConfigPersistsToLocalFile 模拟界面「保存」操作，
// 确认修改确实写入本地配置文件，且应用重启后能原样恢复。
func TestSaveConfigPersistsToLocalFile(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	logBus, err := logger.New(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	defer logBus.Close()

	st, err := store.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := emu.NewManager(logBus, st, func() {})
	defer mgr.Shutdown()

	svc := &App{logBus: logBus, store: st, mgr: mgr, logDir: filepath.Join(dir, "logs")}

	// 界面保存的等价调用：关闭自动连接，避免测试期间联网
	cfg := store.Default()
	cfg.Piles = []store.PileConfig{{
		ID: "pile-1", PileNo: "20260501000001", GunCount: 2,
		PileType: 0, RatedPower: 120, ProgramVer: "220422", Enabled: false,
	}}
	cfg.Server.Host = "192.168.1.99"
	cfg.Server.Port = 9851
	cfg.Server.Version = "V21"
	cfg.HeartbeatSec = 30
	if err := svc.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	path := st.Path()
	if filepath.Ext(path) != ".yml" {
		t.Fatalf("配置文件应为 YAML: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("配置文件未生成: %v", err)
	}
	text := string(raw)
	wants := []string{
		"# vcpp-emulator 运行配置",
		"host: 192.168.1.99",
		"port: 9851",
		"version: V21",
		"heartbeat-sec: 30",
		`pile-no: "20260501000001"`,
		"last-start-charge:",
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("配置文件缺少 %q\n%s", want, text)
		}
	}

	// 重启后（重新创建 Store）应原样恢复
	restarted, err := store.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	got := restarted.Get()
	if got.Server.Host != "192.168.1.99" || got.Server.Port != 9851 ||
		got.Server.Version != "V21" || got.HeartbeatSec != 30 {
		t.Fatalf("重启后服务端配置不一致: %+v", got.Server)
	}
	if len(got.Piles) != 1 || got.Piles[0].PileNo != "20260501000001" {
		t.Fatalf("重启后桩配置不一致: %+v", got.Piles)
	}
	if got.LastStartCharge != store.DefaultStartCharge() {
		t.Fatalf("重启后最近表单不一致: %+v", got.LastStartCharge)
	}
}

// TestConfigNotesReportRepairedValues 越界取值应在启动日志的修正说明中体现
func TestConfigNotesReportRepairedValues(t *testing.T) {
	dir := t.TempDir()
	bad := "server:\n  host: \"\"\n  port: 99999\n  version: \"\"\nheartbeat-sec: 1\n"
	if err := os.WriteFile(filepath.Join(dir, store.ConfigFile), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := st.Get()
	if cfg.Server.Host != "10.88.0.81" || cfg.Server.Port != 9847 || cfg.HeartbeatSec != 10 {
		t.Fatalf("越界取值未回退默认值: %+v", cfg)
	}
	notes := strings.Join(st.Notes(), "\n")
	if !strings.Contains(notes, "heartbeat-sec") || !strings.Contains(notes, "server.port") {
		t.Fatalf("修正说明缺失: %s", notes)
	}
}
