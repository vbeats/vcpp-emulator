// 复用 wails 绑定生成器输出的模型类型，避免与 Go 侧结构定义重复维护。
export type { PileView, GunView, StartRequest, ChargeTarget } from '../bindings/vcpp-emulator/internal/emu/models'
export type { AppConfig, ServerConfig, PileConfig, StartChargeForm } from '../bindings/vcpp-emulator/internal/store/models'
export type { Entry } from '../bindings/vcpp-emulator/internal/logger/models'
export type { VersionOption } from '../bindings/vcpp-emulator/models'
