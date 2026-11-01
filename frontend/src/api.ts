import * as App from '../bindings/vcpp-emulator/app'

import type {
  AppConfig,
  Entry,
  GunView,
  PileConfig,
  PileView,
  StartChargeForm,
  StartRequest,
  VersionOption,
} from './types'

// 后端方法返回可取消的 Promise，这里统一转为普通 Promise 并规整错误信息
async function call<T>(p: Promise<T>): Promise<T> {
  try {
    return await p
  } catch (err) {
    throw new Error(err instanceof Error ? err.message : String(err))
  }
}

// Go 侧的切片可能为 nil，绑定层对应字段允许为 null，这里统一收敛成前端可用的空值
const EMPTY_TARGET = { mode: 'amount', amount: 0, minute: 0, energy: 0, soc: 0 }

const normalGun = (g: GunView): GunView => ({
  ...g,
  target: g.target ?? EMPTY_TARGET,
})

const normalPile = (p: PileView): PileView => ({
  ...p,
  guns: (p.guns ?? []).map(normalGun),
})

// 最近一次「启动充电」表单填写内容的兜底初值（与后端 store.DefaultStartCharge 一致）
const DEFAULT_START_FORM: StartChargeForm = {
  startType: 1,
  cardNo: '88888888',
  vin: '',
  batteryKwh: 60,
  startSoc: 20,
  maxVoltage: 750,
  maxCurrent: 250,
  target: { mode: 'amount', amount: 30, minute: 30, energy: 20, soc: 90 },
}

const normalStartForm = (f: AppConfig['lastStartCharge'] | null | undefined): StartChargeForm => ({
  ...DEFAULT_START_FORM,
  ...(f ?? {}),
  target: f?.target ?? DEFAULT_START_FORM.target,
})

const normalConfig = (c: AppConfig): AppConfig => ({
  ...c,
  server: c.server ?? { host: '10.88.0.81', port: 9847, version: 'V15' },
  piles: (c.piles ?? []).map((p) => ({ ...p })),
  defaultTarget: c.defaultTarget ?? { mode: 'amount', amount: 30, minute: 30, energy: 20, soc: 90 },
  lastStartCharge: normalStartForm(c.lastStartCharge),
})

// 事件推送的桩快照同样需要做空值收敛
export const normalizePiles = (list: PileView[] | null): PileView[] => (list ?? []).map(normalPile)

export const api = {
  versions: async (): Promise<VersionOption[]> => (await call(App.GetVersions())) ?? [],
  config: async (): Promise<AppConfig> => normalConfig(await call(App.GetConfig())),
  saveConfig: (cfg: AppConfig) => call(App.SaveConfig(cfg)),
  piles: async (): Promise<PileView[]> => ((await call(App.GetPiles())) ?? []).map(normalPile),
  logs: async (): Promise<Entry[]> => (await call(App.GetLogs())) ?? [],
  logDir: () => call(App.GetLogDir()),

  addPile: (cfg: PileConfig) => call(App.AddPile(cfg)),
  updatePile: (cfg: PileConfig) => call(App.UpdatePile(cfg)),
  removePile: (id: string) => call(App.RemovePile(id)),
  startPile: (id: string) => call(App.StartPile(id)),
  stopPile: (id: string) => call(App.StopPile(id)),
  startAll: () => call(App.StartAll()),
  stopAll: () => call(App.StopAll()),

  startCharge: (req: StartRequest) => call(App.StartCharge(req)),
  stopCharge: (pileId: string, gun: number) => call(App.StopCharge(pileId, gun)),
  setPlug: (pileId: string, gun: number, plugged: boolean) => call(App.SetPlug(pileId, gun, plugged)),
}
