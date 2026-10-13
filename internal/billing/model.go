package billing

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/csf/vcpp-emulator/internal/protocol/ykc"
)

// 计费模型的定点精度：费率/单价保留 5 位小数，电量/金额保留 4 位小数。
// 全部使用 int64 定点运算，不引入外部 decimal 依赖。
const (
	Scale4 = 10000  // 电量 kWh / 金额 元
	Scale5 = 100000 // 费率 / 单价 元
)

// Section 一个费率段，单位价格的 raw 值（×0.00001 元/kWh）。
type Section struct {
	Ep int64 // 电费费率
	Sp int64 // 服务费费率
}

// Price 返回该段的单位价格（电费 + 服务费）raw 值。
func (s Section) Price() int64 { return s.Ep + s.Sp }

// Model 计费模型内存结构。
// Count 为费率段数：4 = 尖峰平谷，5 = 尖峰平谷深谷（深谷排在谷之后）。
// 两种模式由平台下发的计费模型帧长度决定，协议本身没有「模式/段数」字段。
type Model struct {
	No        string
	Count     int
	Sections  [ykc.MaxSections]Section
	LossRatio byte
	Slots     [48]byte
	UpdatedAt time.Time
}

// FromPayload 由下行帧解析结果构造内存模型。
func FromPayload(p *ykc.BillingPayload) *Model {
	m := &Model{
		No:        p.ModelNo,
		Count:     p.Sections,
		LossRatio: p.LossRatio,
		UpdatedAt: time.Now(),
	}
	if m.Count < 4 || m.Count > ykc.MaxSections {
		m.Count = 4
	}
	for i := 0; i < m.Count; i++ {
		m.Sections[i] = Section{Ep: p.Ep[i], Sp: p.Sp[i]}
	}
	copy(m.Slots[:], p.Slots[:])
	return m
}

// ModeName 返回费率模式名称，用于日志。
func (m *Model) ModeName() string {
	if m.Count == 5 {
		return "尖峰平谷深谷(5段)"
	}
	return "尖峰平谷(4段)"
}

// SlotAt 返回指定时刻所在的费率段索引。时段费率号超出段数时回退到 0（尖）。
func (m *Model) SlotAt(t time.Time) byte {
	idx := t.Hour()*2 + t.Minute()/30
	if idx < 0 {
		idx = 0
	}
	if idx > 47 {
		idx = 47
	}
	s := int(m.Slots[idx])
	if s >= m.Count {
		return 0
	}
	return byte(s)
}

// UnitPrice 返回指定费率段的单位价格 raw 值（×0.00001 元/kWh）。
func (m *Model) UnitPrice(slot byte) int64 {
	if int(slot) >= m.Count {
		return 0
	}
	return m.Sections[slot].Price()
}

// PriceYuan 返回指定费率段的单位价格（元/kWh），用于日志展示。
func (m *Model) PriceYuan(slot byte) float64 {
	return float64(m.UnitPrice(slot)) / Scale5
}

// String 返回计费模型摘要，用于日志。
func (m *Model) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("计费模型编号=%s 模式=%s ", m.No, m.ModeName()))
	for i := 0; i < m.Count; i++ {
		sb.WriteString(fmt.Sprintf("%s=%.5f元/kWh ", ykc.SectionNames[i], m.PriceYuan(byte(i))))
	}
	sb.WriteString("时段=")
	for i, s := range m.Slots {
		if i > 0 {
			sb.WriteString(",")
		}
		if int(s) < m.Count {
			sb.WriteString(ykc.SectionNames[s])
		} else {
			sb.WriteString("?")
		}
	}
	return sb.String()
}

// Cache 按桩号缓存计费模型，仅内存，不依赖外部组件。
type Cache struct {
	m sync.Map
}

// Set 缓存指定桩号的计费模型。
func (c *Cache) Set(deviceNo uint64, model *Model) {
	c.m.Store(deviceNo, model)
}

// Get 读取指定桩号的计费模型。
func (c *Cache) Get(deviceNo uint64) (*Model, bool) {
	v, ok := c.m.Load(deviceNo)
	if !ok {
		return nil, false
	}
	return v.(*Model), true
}

// Amount1e4 计算金额：电量（×0.0001 kWh）× 单价（×0.00001 元）→ 金额（×0.0001 元）。
func Amount1e4(energy1e4, price1e5 int64) int64 {
	if price1e5 <= 0 {
		return 0
	}
	return energy1e4 * price1e5 / Scale5
}

// Accum 按费率段累计的电量与金额，最多 5 段（尖/峰/平/谷/深谷）。
type Accum struct {
	Energy [ykc.MaxSections]int64 // 分段电量，×0.0001 kWh
	Amount [ykc.MaxSections]int64 // 分段金额，×0.0001 元
}

// Add 把一段电量按当段单价累计。
func (a *Accum) Add(slot byte, energy1e4 int64, price1e5 int64) {
	if int(slot) >= ykc.MaxSections {
		return
	}
	a.Energy[slot] += energy1e4
	a.Amount[slot] += Amount1e4(energy1e4, price1e5)
}

// TotalEnergy 返回总电量（×0.0001 kWh）。
func (a *Accum) TotalEnergy() int64 {
	var t int64
	for _, v := range a.Energy {
		t += v
	}
	return t
}

// TotalAmount 返回总金额（×0.0001 元）。
func (a *Accum) TotalAmount() int64 {
	var t int64
	for _, v := range a.Amount {
		t += v
	}
	return t
}

// Copy 返回当前累计的快照，用于生成账单。
func (a *Accum) Copy() Accum { return *a }

// EnergyKwh 返回指定段的电量（kWh）。
func (a *Accum) EnergyKwh(i int) float64 { return float64(a.Energy[i]) / Scale4 }

// AmountYuan 返回指定段的金额（元）。
func (a *Accum) AmountYuan(i int) float64 { return float64(a.Amount[i]) / Scale4 }

// TotalEnergyKwh 返回总电量（kWh）。
func (a *Accum) TotalEnergyKwh() float64 { return float64(a.TotalEnergy()) / Scale4 }

// TotalAmountYuan 返回总金额（元）。
func (a *Accum) TotalAmountYuan() float64 { return float64(a.TotalAmount()) / Scale4 }
