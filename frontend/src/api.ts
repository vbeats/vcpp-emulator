import * as App from '../bindings/vcpp-emulator/app'

import type {
  AppConfig,
  DispenserConfig,
  DispenserView,
  Entry,
  FuelOption,
  FuelOrder,
  GunView,
  NozzleView,
  PileConfig,
  PileView,
  PushMessage,
  StartChargeForm,
  StartFuelRequest,
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
const EMPTY_FUEL_TARGET = { mode: 'free' as string, amount: 0, volume: 0 }

const normalGun = (g: GunView): GunView => ({
  ...g,
  target: g.target ?? EMPTY_TARGET,
})

const normalPile = (p: PileView): PileView => ({
  ...p,
  guns: (p.guns ?? []).map(normalGun),
})

const normalNozzle = (n: NozzleView): NozzleView => ({
  ...n,
  target: n.target ?? EMPTY_FUEL_TARGET,
})

const normalDispenser = (d: DispenserView): DispenserView => ({
  ...d,
  nozzles: (d.nozzles ?? []).map(normalNozzle),
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

// 加油站配置兜底初值（与后端 store.DefaultGas 一致）
const DEFAULT_GAS: AppConfig['gas'] = {
  pushMode: 'mqtt',
  mqtt: {
    broker: '10.88.0.81',
    port: 1883,
    clientId: 'vcpp-emulator-gas',
    username: 'admin',
    password: 'admin123456',
    topicProgress: 'vcpp/gas/{dispenser}/nozzle/{nozzle}/progress',
    topicOrder: 'vcpp/gas/{dispenser}/nozzle/{nozzle}/order',
    qos: 1,
  },
  http: {
    baseUrl: 'http://10.88.0.81:18083',
    pathProgress: '/api/gas/fuel/progress',
    pathOrder: '/api/gas/fuel/order',
    token: '',
    timeoutSec: 10,
  },
  prices: [
    { code: '-10#', price: 7.25 },
    { code: '0#', price: 6.88 },
    { code: '92#', price: 7.68 },
    { code: '95#', price: 8.19 },
    { code: '98#', price: 8.96 },
    { code: '100#', price: 6.5 },
  ],
  dispensers: [
    {
      id: 'dispenser-1',
      dispenserNo: '01',
      nozzleCount: 2,
      nozzles: [
        { no: '1', fuelCode: '92#' },
        { no: '2', fuelCode: '95#' },
      ],
    },
  ],
  pushIntervalSec: 5,
  tankVolumeL: 60,
  lastStartFuel: { target: { mode: 'amount', amount: 200, volume: 30 }, cardNo: '88888888', plateNo: '京A12345' },
}

const normalGas = (g: AppConfig['gas'] | null | undefined): AppConfig['gas'] => ({
  ...DEFAULT_GAS,
  ...(g ?? {}),
  mqtt: g?.mqtt ?? DEFAULT_GAS.mqtt,
  http: g?.http ?? DEFAULT_GAS.http,
  prices: (g?.prices ?? []).length ? (g?.prices as NonNullable<AppConfig['gas']['prices']>) : DEFAULT_GAS.prices,
  dispensers: (g?.dispensers ?? []).length ? (g?.dispensers as NonNullable<AppConfig['gas']['dispensers']>) : DEFAULT_GAS.dispensers,
  lastStartFuel: g?.lastStartFuel ?? DEFAULT_GAS.lastStartFuel,
})

const normalConfig = (c: AppConfig): AppConfig => ({
  ...c,
  server: c.server ?? { host: '10.88.0.81', port: 9847, version: 'V15' },
  piles: (c.piles ?? []).map((p) => ({ ...p })),
  defaultTarget: c.defaultTarget ?? { mode: 'amount', amount: 30, minute: 30, energy: 20, soc: 90 },
  lastStartCharge: normalStartForm(c.lastStartCharge),
  gas: normalGas(c.gas),
})

// 事件推送的桩快照同样需要做空值收敛
export const normalizePiles = (list: PileView[] | null): PileView[] => (list ?? []).map(normalPile)

// 事件推送的油机快照同样需要做空值收敛
export const normalizeDispensers = (list: DispenserView[] | null): DispenserView[] =>
  (list ?? []).map(normalDispenser)

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

  // ---------------- 加油站 ----------------
  fuelOptions: async (): Promise<FuelOption[]> => (await call(App.GetFuelOptions())) ?? [],
  dispensers: async (): Promise<DispenserView[]> => ((await call(App.GetDispensers())) ?? []).map(normalDispenser),
  fuelOrders: async (): Promise<FuelOrder[]> => (await call(App.GetFuelOrders())) ?? [],
  pushTarget: async (): Promise<string> => (await call(App.GetPushTarget())) ?? '',
  pushMessages: async (): Promise<PushMessage[]> => (await call(App.GetPushMessages())) ?? [],

  addDispenser: (cfg: DispenserConfig) => call(App.AddDispenser(cfg)),
  updateDispenser: (cfg: DispenserConfig) => call(App.UpdateDispenser(cfg)),
  removeDispenser: (id: string) => call(App.RemoveDispenser(id)),
  generateDispensers: (count: number, nozzleCount: number) => call(App.GenerateDispensers(count, nozzleCount)),

  startFuel: (req: StartFuelRequest) => call(App.StartFuel(req)),
  stopFuel: (dispenserId: string, nozzleNo: string) => call(App.StopFuel(dispenserId, nozzleNo)),
  setNozzle: (dispenserId: string, nozzleNo: string, lifted: boolean) =>
    call(App.SetNozzle(dispenserId, nozzleNo, lifted)),
  setNozzleFault: (dispenserId: string, nozzleNo: string, fault: boolean) =>
    call(App.SetNozzleFault(dispenserId, nozzleNo, fault)),
  updateNozzleFuel: (dispenserId: string, nozzleNo: string, fuelCode: string) =>
    call(App.UpdateNozzleFuel(dispenserId, nozzleNo, fuelCode)),
}
