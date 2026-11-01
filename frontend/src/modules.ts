// 应用模块定义：充电桩与加油站两套界面独立并行，通过左侧 tab 切换。
// 充电桩走云快充协议（TCP 对端为平台服务端）；加油站为本地加油机模拟，数据经 MQTT / HTTP 推送。
export type ModuleKey = 'charge' | 'gas'

export interface ModuleMeta {
  key: ModuleKey
  label: string
  short: string
  desc: string
}

export const MODULES: ModuleMeta[] = [
  { key: 'charge', label: '充电桩', short: '充', desc: '云快充协议 · 设备侧模拟' },
  { key: 'gas', label: '加油站', short: '油', desc: '加油机加油流程 · 数据推送' },
]
