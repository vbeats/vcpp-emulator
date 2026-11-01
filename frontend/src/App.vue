<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import * as WailsEvents from '../bindings/events/index'

import TopBar from './components/TopBar.vue'
import ModuleRail from './components/ModuleRail.vue'
import Sidebar from './components/Sidebar.vue'
import PileDetail from './components/PileDetail.vue'
import GasSidebar from './components/GasSidebar.vue'
import GasStationView from './components/GasStationView.vue'
import LogPanel from './components/LogPanel.vue'
import PileDialog from './components/PileDialog.vue'
import StartChargeDialog from './components/StartChargeDialog.vue'
import DispenserDialog from './components/DispenserDialog.vue'
import StartFuelDialog from './components/StartFuelDialog.vue'
import GasConfigDialog from './components/GasConfigDialog.vue'
import { type ModuleKey } from './modules'
import { api, normalizeDispensers, normalizePiles } from './api'
import type {
  AppConfig,
  DispenserConfig,
  DispenserView,
  Entry,
  FuelOption,
  FuelOrder,
  PileConfig,
  PileView,
  PushMessage,
  StartChargeForm,
  StartFuelRequest,
  VersionOption,
} from './types'

// 当前模块：充电桩 / 加油站，两套界面独立并行
const module = ref<ModuleKey>('charge')

const config = ref<AppConfig | null>(null)
const piles = ref<PileView[]>([])
const dispensers = ref<DispenserView[]>([])
const fuels = ref<FuelOption[]>([])
const orders = ref<FuelOrder[]>([])
const pushes = ref<PushMessage[]>([])
const pushTarget = ref('')
const logs = ref<Entry[]>([])
const versions = ref<VersionOption[]>([])
const logDir = ref('')
const selectedId = ref('')
const gasSelectedId = ref('')

// 弹窗状态
const pileForm = ref<{ open: boolean; editing: PileConfig | null }>({ open: false, editing: null })
const chargeForm = ref<{ open: boolean; gunNo: number }>({ open: false, gunNo: 1 })
const dispenserForm = ref<{ open: boolean; editing: DispenserConfig | null }>({ open: false, editing: null })
const fuelForm = ref<{ open: boolean; nozzleNo: string }>({ open: false, nozzleNo: '1' })
const gasConfigForm = ref<{ open: boolean }>({ open: false })

const toasts = ref<{ id: number; text: string; ok: boolean }[]>([])

const toast = (text: string, ok = true) => {
  const id = Date.now() + Math.random()
  toasts.value.push({ id, text, ok })
  setTimeout(() => {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }, 2800)
}

const selected = computed(() => piles.value.find((p) => p.id === selectedId.value) ?? null)
const onlineCount = computed(() => piles.value.filter((p) => p.status === 'online').length)
const selectedDispenser = computed(
  () => dispensers.value.find((d) => d.id === gasSelectedId.value) ?? null,
)
// fuellingNozzles 正在加油的枪数（油机无启用 / 停用状态，按枪统计即可）
const fuellingNozzles = computed(() =>
  dispensers.value.reduce(
    (sum, d) => sum + (d.nozzles ?? []).filter((n) => n.status === 'fueling').length,
    0,
  ),
)

// 建议桩号：在现有桩号基础上递增
const suggestPileNo = () => {
  const nums = piles.value.map((p) => Number(p.pileNo)).filter((n) => Number.isFinite(n) && n > 0)
  const max = nums.length ? Math.max(...nums) : 20260501000000
  return String(max + 1)
}

// 建议油机号：在现有机号基础上递增（保持两位宽度）
const suggestDispenserNo = () => {
  const nums = dispensers.value.map((d) => Number(d.dispenserNo)).filter((n) => Number.isFinite(n))
  const max = nums.length ? Math.max(...nums) : 0
  return String(max + 1).padStart(2, '0')
}

let offLog: (() => void) | undefined
let offState: (() => void) | undefined
let offGasState: (() => void) | undefined
let offGasPush: (() => void) | undefined

const load = async () => {
  try {
    const [cfg, ps, ds, fs, os, ms, pt, vs, ls, dir] = await Promise.all([
      api.config(),
      api.piles(),
      api.dispensers(),
      api.fuelOptions(),
      api.fuelOrders(),
      api.pushMessages(),
      api.pushTarget(),
      api.versions(),
      api.logs(),
      api.logDir(),
    ])
    config.value = cfg
    piles.value = ps
    dispensers.value = ds
    fuels.value = fs
    orders.value = os
    pushes.value = ms
    pushTarget.value = pt
    versions.value = vs
    logs.value = ls
    logDir.value = dir
    if (!selectedId.value && ps.length) selectedId.value = ps[0]?.id ?? ''
    if (!gasSelectedId.value && ds.length) gasSelectedId.value = ds[0]?.id ?? ''
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const refreshPiles = async () => {
  try {
    piles.value = await api.piles()
  } catch {
    /* 状态推送会持续刷新，忽略单次失败 */
  }
}

const refreshGas = async () => {
  try {
    const [ds, os, pt] = await Promise.all([api.dispensers(), api.fuelOrders(), api.pushTarget()])
    dispensers.value = ds
    orders.value = os
    pushTarget.value = pt
  } catch {
    /* 状态推送会持续刷新，忽略单次失败 */
  }
}

const saveServer = async (cfg: AppConfig) => {
  try {
    await api.saveConfig(cfg)
    config.value = cfg
    toast('服务端配置已保存')
    await refreshPiles()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const togglePile = async (pile: PileView) => {
  try {
    if (pile.status === 'offline') await api.startPile(pile.id)
    else await api.stopPile(pile.id)
    await refreshPiles()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

// 编辑桩时以持久化配置为准（PileView 中的桩类型是展示用文本）
const editPile = (pile: PileView) => {
  const saved = config.value?.piles?.find((p) => p.id === pile.id)
  pileForm.value = {
    open: true,
    editing: saved
      ? { ...saved }
      : {
          id: pile.id,
          pileNo: pile.pileNo,
          gunCount: pile.gunCount,
          pileType: pile.pileType === '交流' ? 1 : 0,
          ratedPower: pile.ratedPower,
          programVer: pile.programVer,
          enabled: pile.enabled,
        },
  }
}

const submitPile = async (cfg: PileConfig) => {
  try {
    if (cfg.id) await api.updatePile(cfg)
    else await api.addPile(cfg)
    pileForm.value = { open: false, editing: null }
    toast('充电桩已保存')
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const removePile = async (id: string) => {
  try {
    await api.removePile(id)
    if (selectedId.value === id) selectedId.value = ''
    toast('充电桩已删除')
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const startAll = async () => {
  await api.startAll()
  await refreshPiles()
  toast('已发起全部充电桩连接')
}

const stopAll = async () => {
  await api.stopAll()
  await refreshPiles()
  toast('已断开全部充电桩连接')
}

const stopCharge = async (gun: number) => {
  const pile = selected.value
  if (!pile) return
  try {
    await api.stopCharge(pile.id, gun)
    await refreshPiles()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const setPlug = async (gun: number, plugged: boolean) => {
  const pile = selected.value
  if (!pile) return
  try {
    await api.setPlug(pile.id, gun, plugged)
    await refreshPiles()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const submitCharge = async (req: {
  pileId: string
  gunNo: number
  startType: number
  cardNo: string
  vin: string
  target: { mode: string; amount: number; minute: number; energy: number; soc: number }
  batteryKwh: number
  startSoc: number
  maxVoltage: number
  maxCurrent: number
}) => {
  const pile = selected.value
  if (!pile) return
  try {
    await api.startCharge({ ...req, pileId: pile.id })
    // 后端已落盘，这里同步本地配置，保证再次打开表单读到的是最近一次填写
    if (config.value) {
      const last: StartChargeForm = {
        startType: req.startType,
        cardNo: req.cardNo,
        vin: req.vin,
        batteryKwh: req.batteryKwh,
        startSoc: req.startSoc,
        maxVoltage: req.maxVoltage,
        maxCurrent: req.maxCurrent,
        target: req.target,
      }
      config.value = { ...config.value, lastStartCharge: last }
    }
    chargeForm.value = { open: false, gunNo: 1 }
    toast('充电已启动')
    await refreshPiles()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

// ---------------- 加油站 ----------------

const editDispenser = (d: DispenserView) => {
  const saved = config.value?.gas?.dispensers?.find((it) => it.id === d.id)
  dispenserForm.value = {
    open: true,
    editing: saved
      ? { ...saved, nozzles: (saved.nozzles ?? []).map((n) => ({ ...n })) }
      : {
          id: d.id,
          dispenserNo: d.dispenserNo,
          nozzleCount: d.nozzleCount,
          nozzles: (d.nozzles ?? []).map((n) => ({ no: n.no, fuelCode: n.fuelCode })),
        },
  }
}

const submitDispenser = async (cfg: DispenserConfig) => {
  try {
    if (cfg.id) await api.updateDispenser(cfg)
    else await api.addDispenser(cfg)
    dispenserForm.value = { open: false, editing: null }
    toast('加油机已保存')
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const removeDispenser = async (id: string) => {
  try {
    await api.removeDispenser(id)
    if (gasSelectedId.value === id) gasSelectedId.value = ''
    toast('加油机已删除')
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const submitGasConfig = async (gas: AppConfig['gas']) => {
  if (!config.value) return
  try {
    await api.saveConfig({ ...config.value, gas })
    toast('油站配置已保存')
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const generateDispensers = async (count: number, nozzleCount: number) => {
  try {
    await api.generateDispensers(count, nozzleCount)
    gasSelectedId.value = ''
    toast(`已生成 ${count} 台油机 × ${nozzleCount} 枪`)
    await load()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const submitFuel = async (req: StartFuelRequest) => {
  const d = selectedDispenser.value
  if (!d) return
  try {
    await api.startFuel({ ...req, dispenserId: d.id })
    // 后端已落盘，这里同步本地配置，保证再次打开表单读到的是最近一次填写
    if (config.value) {
      config.value = { ...config.value, gas: { ...config.value.gas, lastStartFuel: lastStartFuel(req) } }
    }
    fuelForm.value = { open: false, nozzleNo: '1' }
    toast('加油已开始')
    await refreshGas()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const lastStartFuel = (req: StartFuelRequest) => ({
  target: {
    mode: req.target?.mode ?? 'free',
    amount: req.target?.amount ?? 0,
    volume: req.target?.volume ?? 0,
  },
  cardNo: req.cardNo ?? '',
  plateNo: req.plateNo ?? '',
})

const stopFuel = async (nozzleNo: string) => {
  const d = selectedDispenser.value
  if (!d) return
  try {
    await api.stopFuel(d.id, nozzleNo)
    await refreshGas()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const liftNozzle = async (nozzleNo: string, lifted: boolean) => {
  const d = selectedDispenser.value
  if (!d) return
  try {
    await api.setNozzle(d.id, nozzleNo, lifted)
    await refreshGas()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

const changeFuel = async (nozzleNo: string, code: string) => {
  const d = selectedDispenser.value
  if (!d) return
  try {
    await api.updateNozzleFuel(d.id, nozzleNo, code)
    await refreshGas()
  } catch (e) {
    toast(e instanceof Error ? e.message : String(e), false)
  }
}

onMounted(async () => {
  // 日志事件：仅保留最近 100 条
  offLog = Events.On(WailsEvents.Log, (ev) => {
    const entry = ev.data as Entry
    if (entry) logs.value = [...logs.value, entry].slice(-100)
  })
  // 状态事件：充电桩快照整体刷新
  offState = Events.On(WailsEvents.State, (ev) => {
    piles.value = normalizePiles(ev.data as PileView[] | null)
  })
  // 状态事件：加油机快照整体刷新
  offGasState = Events.On(WailsEvents.GasState, (ev) => {
    dispensers.value = normalizeDispensers(ev.data as DispenserView[] | null)
  })
  // 推送事件：加油数据待推送记录
  offGasPush = Events.On(WailsEvents.GasPush, (ev) => {
    const msg = ev.data as PushMessage
    if (msg) pushes.value = [...pushes.value, msg].slice(-50)
    void refreshGas()
  })
  await load()
})

onBeforeUnmount(() => {
  offLog?.()
  offState?.()
  offGasState?.()
  offGasPush?.()
})
</script>

<template>
  <div class="h-full flex flex-col p-3 gap-3">
    <TopBar
      :config="config"
      :versions="versions"
      :online="module === 'charge' ? onlineCount : fuellingNozzles"
      :total="module === 'charge' ? piles.length : dispensers.length"
      :module="module"
      :push-target="pushTarget"
      @save="saveServer"
      @start-all="startAll"
      @stop-all="stopAll"
      @open-gas-config="gasConfigForm = { open: true }"
    />

    <div class="flex-1 min-h-0 flex gap-3">
      <ModuleRail :active="module" @change="module = $event" />

      <template v-if="module === 'charge'">
        <Sidebar
          :piles="piles"
          :selected-id="selectedId"
          @select="selectedId = $event"
          @add="pileForm = { open: true, editing: null }"
          @edit="editPile"
          @remove="removePile"
          @toggle="togglePile"
        />

        <div class="flex-1 min-w-0">
          <PileDetail
            :pile="selected"
            @edit="editPile"
            @toggle="togglePile"
            @start="chargeForm = { open: true, gunNo: $event }"
            @stop="stopCharge"
            @plug="setPlug"
          />
        </div>
      </template>

      <template v-else>
        <GasSidebar
          :dispensers="dispensers"
          :selected-id="gasSelectedId"
          @select="gasSelectedId = $event"
          @add="dispenserForm = { open: true, editing: null }"
          @edit="editDispenser"
          @remove="removeDispenser"
          @config="gasConfigForm = { open: true }"
          @generate="generateDispensers"
        />

        <div class="flex-1 min-w-0">
          <GasStationView
            :dispenser="selectedDispenser"
            :fuels="fuels"
            :orders="orders"
            :pushes="pushes"
            :push-target="pushTarget"
            @edit="editDispenser"
            @start="fuelForm = { open: true, nozzleNo: $event }"
            @stop="stopFuel"
            @lift="liftNozzle"
            @fuel="changeFuel"
          />
        </div>
      </template>

      <div class="w-[420px] shrink-0">
        <LogPanel :logs="logs" :log-dir="logDir" />
      </div>
    </div>

    <PileDialog
      :open="pileForm.open"
      :editing="pileForm.editing"
      :suggest="suggestPileNo()"
      @submit="submitPile"
      @close="pileForm = { open: false, editing: null }"
    />

    <StartChargeDialog
      :open="chargeForm.open"
      :pile-no="selected?.pileNo ?? ''"
      :gun-no="chargeForm.gunNo"
      :last="config?.lastStartCharge ?? null"
      @submit="submitCharge"
      @close="chargeForm = { open: false, gunNo: 1 }"
    />

    <DispenserDialog
      :open="dispenserForm.open"
      :editing="dispenserForm.editing"
      :suggest="suggestDispenserNo()"
      @submit="submitDispenser"
      @close="dispenserForm = { open: false, editing: null }"
    />

    <StartFuelDialog
      :open="fuelForm.open"
      :dispenser-no="selectedDispenser?.dispenserNo ?? ''"
      :nozzle-no="fuelForm.nozzleNo"
      :last="config?.gas?.lastStartFuel ?? null"
      @submit="submitFuel"
      @close="fuelForm = { open: false, nozzleNo: '1' }"
    />

    <GasConfigDialog
      :open="gasConfigForm.open"
      :gas="config?.gas ?? null"
      :fuels="fuels"
      @submit="submitGasConfig"
      @close="gasConfigForm = { open: false }"
    />

    <!-- 轻提示 -->
    <div class="fixed bottom-4 right-4 z-[60] flex flex-col gap-2">
      <div
        v-for="t in toasts"
        :key="t.id"
        class="panel px-3 py-2 text-[12px] border"
        :class="t.ok ? 'border-accent/40 text-accent' : 'border-rose-400/50 text-rose-600'"
      >
        {{ t.text }}
      </div>
    </div>
  </div>
</template>
