package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastStartChargeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	cfg := s.Get()
	cfg.LastStartCharge = StartChargeForm{
		StartType:  3,
		CardNo:     "1234",
		Vin:        "LSVAA1234567890123",
		BatteryKwh: 55.5,
		StartSoc:   35,
		MaxVoltage: 500,
		MaxCurrent: 120,
		Target:     ChargeTarget{Mode: "time", Amount: 12, Minute: 45, Energy: 8, Soc: 60},
	}
	if err := s.Save(cfg); err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Get().LastStartCharge
	if got.StartType != 3 || got.CardNo != "1234" || got.Vin != "LSVAA1234567890123" ||
		got.BatteryKwh != 55.5 || got.StartSoc != 35 || got.MaxVoltage != 500 || got.MaxCurrent != 120 ||
		got.Target.Mode != "time" || got.Target.Minute != 45 {
		t.Fatalf("往返后表单内容不符: %+v", got)
	}
}

func TestLegacyConfigGetsDefaultForm(t *testing.T) {
	dir := t.TempDir()
	old := `{"server":{"host":"1.2.3.4","port":9848,"version":"V16"},"piles":[],"defaultTarget":{"mode":"amount","amount":30,"minute":30,"energy":20,"soc":90}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := s.Get().LastStartCharge
	def := DefaultStartCharge()
	if f.StartType != def.StartType || f.BatteryKwh != def.BatteryKwh || f.StartSoc != def.StartSoc ||
		f.MaxVoltage != def.MaxVoltage || f.MaxCurrent != def.MaxCurrent || f.Target.Mode != def.Target.Mode {
		t.Fatalf("旧配置未补齐表单默认值: %+v", f)
	}
	if s.Get().Server.Host != "1.2.3.4" || s.Get().Server.Port != 9848 {
		t.Fatalf("旧配置服务端信息被覆盖: %+v", s.Get().Server)
	}
}

func TestNormalizeStartChargeAbnormal(t *testing.T) {
	f := StartChargeForm{StartType: 9, BatteryKwh: 0, StartSoc: 150, MaxVoltage: -5, MaxCurrent: 0}
	normalizeStartCharge(&f)
	def := DefaultStartCharge()
	if f.StartType != def.StartType || f.BatteryKwh != def.BatteryKwh || f.StartSoc != def.StartSoc ||
		f.MaxVoltage != def.MaxVoltage || f.MaxCurrent != def.MaxCurrent || f.Target.Mode != defaultTargetMode {
		t.Fatalf("异常值未归一化: %+v", f)
	}
}

// 旧版 JSON 配置应在读取后自动迁移为 YAML，原文件保留为 .bak
func TestLegacyJSONMigratesToYAML(t *testing.T) {
	dir := t.TempDir()
	old := `{"server":{"host":"1.2.3.4","port":9848,"version":"V16"},"piles":[],"heartbeatSec":20}`
	if err := os.WriteFile(filepath.Join(dir, LegacyConfigFile), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(s.Path()) != ConfigFile {
		t.Fatalf("迁移后应写入 %s，实际: %s", ConfigFile, s.Path())
	}
	if _, err := os.Stat(filepath.Join(dir, LegacyConfigFile+".bak")); err != nil {
		t.Fatalf("旧配置备份缺失: %v", err)
	}
	if raw, err := os.ReadFile(s.Path()); err != nil || !strings.Contains(string(raw), "heartbeat-sec: 20") {
		t.Fatalf("YAML 配置内容不符: %v %s", err, string(raw))
	}
	if got := s.Get(); got.Server.Port != 9848 || got.HeartbeatSec != 20 {
		t.Fatalf("迁移后配置丢失: %+v", got.Server)
	}
}
