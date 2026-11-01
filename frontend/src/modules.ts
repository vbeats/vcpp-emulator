// 应用模块定义：充电桩与加油站两套界面独立并行，通过左侧 tab 切换。
// 加油站模块当前为预留占位（界面骨架 + 静态示例），协议适配层后续按 internal/ykc 的方式另行接入。
export type ModuleKey = 'charge' | 'gas'

export interface ModuleMeta {
  key: ModuleKey
  label: string
  short: string
  desc: string
  reserved?: boolean
}

export const MODULES: ModuleMeta[] = [
  { key: 'charge', label: '充电桩', short: '充', desc: '云快充协议 · 设备侧模拟' },
  { key: 'gas', label: '加油站', short: '油', desc: '加油机 · 预留模块', reserved: true },
]
