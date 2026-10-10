package billing

import (
	"testing"
	"time"
)

func testModel() *Model {
	m := &Model{No: "0100", LossRatio: 0}
	m.Sections[0] = Section{Ep: 123000, Sp: 250000} // 尖 3.73 元/kWh
	m.Sections[1] = Section{Ep: 121000, Sp: 240000} // 峰 3.61
	m.Sections[2] = Section{Ep: 100000, Sp: 230000} // 平 3.30
	m.Sections[3] = Section{Ep: 90000, Sp: 210000}  // 谷 3.00
	for i := range m.Slots {
		m.Slots[i] = 0
	}
	for i := 20; i < 24; i++ { // 10:00 ~ 11:30 峰
		m.Slots[i] = 1
	}
	for i := 24; i < 29; i++ { // 12:00 ~ 14:00 平
		m.Slots[i] = 2
	}
	for i := 29; i < 48; i++ { // 14:30 ~ 23:30 谷
		m.Slots[i] = 3
	}
	return m
}

func TestSlotAt(t *testing.T) {
	m := testModel()
	cases := []struct {
		hour, minute int
		want         byte
	}{
		{0, 0, 0},
		{9, 59, 0},
		{10, 15, 1},
		{13, 0, 2},
		{23, 59, 3},
	}
	for _, tc := range cases {
		got := m.SlotAt(time.Date(2026, 6, 1, tc.hour, tc.minute, 0, 0, time.Local))
		if got != tc.want {
			t.Fatalf("%02d:%02d 期望费率段 %d 实际 %d", tc.hour, tc.minute, tc.want, got)
		}
	}
}

func TestAmount1e4(t *testing.T) {
	// 1 kWh（10000，×0.0001）按 1.3 元/kWh（130000，×0.00001）计费 = 1.3 元（13000，×0.0001）
	if got := Amount1e4(10000, 130000); got != 13000 {
		t.Fatalf("金额不符: %d", got)
	}
	if got := Amount1e4(0, 130000); got != 0 {
		t.Fatalf("零电量金额应为 0: %d", got)
	}
}

func TestAccum(t *testing.T) {
	m := testModel()
	var a Accum
	a.Add(0, 10000, m.UnitPrice(0)) // 尖 1 kWh
	a.Add(3, 20000, m.UnitPrice(3)) // 谷 2 kWh
	if a.TotalEnergy() != 30000 {
		t.Fatalf("总电量不符: %d", a.TotalEnergy())
	}
	if got := a.Amount[0]; got != 37300 {
		t.Fatalf("尖金额不符: %d", got)
	}
	if got := a.Amount[3]; got != 60000 {
		t.Fatalf("谷金额不符: %d", got)
	}
	if a.TotalAmount() != 97300 {
		t.Fatalf("总金额不符: %d", a.TotalAmount())
	}
	if a.TotalEnergyKwh() != 3 {
		t.Fatalf("总电量(kWh)不符: %.4f", a.TotalEnergyKwh())
	}
	if a.TotalAmountYuan() != 9.73 {
		t.Fatalf("总金额(元)不符: %.4f", a.TotalAmountYuan())
	}
}

func TestCache(t *testing.T) {
	c := &Cache{}
	if _, ok := c.Get("20260501000001"); ok {
		t.Fatalf("未设置时不应命中缓存")
	}
	c.Set("20260501000001", testModel())
	m, ok := c.Get("20260501000001")
	if !ok || m.No != "0100" {
		t.Fatalf("缓存读取失败: %v %v", m, ok)
	}
}
