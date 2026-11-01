package main

import (
	"testing"

	"vcpp-emulator/internal/emu"
	"vcpp-emulator/internal/ykc"
)

// lastStartForm 需完整保留用户填写的卡号与 VIN，便于下次回填
func TestLastStartFormKeepsBothIdentities(t *testing.T) {
	req := emu.StartRequest{
		StartType:   ykc.StartTypeCard,
		CardNo:      "88888888",
		Vin:         "LSVAA1234567890123",
		BatteryKwh:  55.5,
		StartSoc:    35,
		MaxVoltage:  500,
		MaxCurrent:  120,
		Target:      emu.ChargeTarget{Mode: emu.TargetTime, Amount: 12, Minute: 45, Energy: 8, Soc: 60},
	}

	f := lastStartForm(req)
	if f.StartType != int(ykc.StartTypeCard) {
		t.Fatalf("启动方式映射错误: %d", f.StartType)
	}
	if f.CardNo != "88888888" || f.Vin != "LSVAA1234567890123" {
		t.Fatalf("卡号 / VIN 未完整保留: cardNo=%q vin=%q", f.CardNo, f.Vin)
	}
	if f.BatteryKwh != 55.5 || f.StartSoc != 35 || f.MaxVoltage != 500 || f.MaxCurrent != 120 {
		t.Fatalf("电池参数映射错误: %+v", f)
	}
	if f.Target.Mode != emu.TargetTime || f.Target.Minute != 45 || f.Target.Amount != 12 {
		t.Fatalf("充电目标映射错误: %+v", f.Target)
	}
}

// trimIdentities 需在记忆之后按启动方式剔除无关身份字段
func TestTrimIdentitiesByStartType(t *testing.T) {
	card := trimIdentities(emu.StartRequest{
		StartType: ykc.StartTypeCard,
		CardNo:    "88888888",
		Vin:       "LSVAA1234567890123",
	})
	if card.CardNo != "88888888" || card.Vin != "" {
		t.Fatalf("刷卡启动应剔除 VIN: cardNo=%q vin=%q", card.CardNo, card.Vin)
	}

	vin := trimIdentities(emu.StartRequest{
		StartType: ykc.StartTypeVin,
		CardNo:    "88888888",
		Vin:       "LSVAA1234567890123",
	})
	if vin.Vin != "LSVAA1234567890123" || vin.CardNo != "" {
		t.Fatalf("VIN 启动应剔除卡号: cardNo=%q vin=%q", vin.CardNo, vin.Vin)
	}
}
