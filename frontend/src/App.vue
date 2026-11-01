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
import { type ModuleKey } from './modules'
import { api, normalizePiles } from './api'
import type { AppConfig, Entry, PileConfig, PileView, StartChargeForm, VersionOption } from './types'

// 当前模块：充电桩 / 加油站，两套界面独立并行
const module = ref<ModuleKey>('charge')

const config = ref<AppConfig | null>(null)
const piles = ref<PileView[]>([])
const logs = ref<Entry[]>([])
const versions = ref<VersionOption[]>([])
const logDir = ref('')
const selectedId = ref('')

// 弹窗状态
const pileForm = ref<{ open: boolean; editing: PileConfig | null }>({ open: false, editing: null })
const chargeForm = ref<{ open: boolean; gunNo: number }>({ open: false, gunNo: 1 })

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

// 建议桩号：在现有桩号基础上递增
const suggestPileNo = () => {
  const nums = piles.value.map((p) => Number(p.pileNo)).filter((n) => Number.isFinite(n) && n > 0)
  const max = nums.length ? Math.max(...nums) : 20260501000000
  return String(max + 1)
}

let offLog: (() => void) | undefined
let offState: (() => void) | undefined

const load = async () => {
  try {
    const [cfg, ps, vs, ls, dir] = await Promise.all([api.config(), api.piles(), api.versions(), api.logs(), api.logDir()])
    config.value = cfg
    piles.value = ps
    versions.value = vs
    logs.value = ls
    logDir.value = dir
    if (!selectedId.value && ps.length) selectedId.value = ps[0]?.id ?? ''
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

onMounted(async () => {
  // 日志事件：仅保留最近 100 条
  offLog = Events.On(WailsEvents.Log, (ev) => {
    const entry = ev.data as Entry
    if (entry) logs.value = [...logs.value, entry].slice(-100)
  })
  // 状态事件：桩快照整体刷新
  offState = Events.On(WailsEvents.State, (ev) => {
    piles.value = normalizePiles(ev.data as PileView[] | null)
  })
  await load()
})

onBeforeUnmount(() => {
  offLog?.()
  offState?.()
})
</script>

<template>
  <div class="h-full flex flex-col p-3 gap-3">
    <TopBar
      :config="config"
      :versions="versions"
      :online="onlineCount"
      :total="piles.length"
      :module="module"
      @save="saveServer"
      @start-all="startAll"
      @stop-all="stopAll"
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
        <GasSidebar />

        <div class="flex-1 min-w-0">
          <GasStationView />
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
