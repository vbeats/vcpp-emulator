<script setup lang="ts">
import { computed } from 'vue'
import type { GunView } from '../types'

const props = defineProps<{ gun: GunView; offline?: boolean }>()
const emit = defineEmits<{
  (e: 'start', gun: number): void
  (e: 'stop', gun: number): void
  (e: 'plug', gun: number, plugged: boolean): void
}>()

const STATUS: Record<string, { text: string; cls: string }> = {
  idle: { text: '空闲', cls: 'text-mute border-line' },
  plugged: { text: '已插枪', cls: 'text-accent-2 border-accent-2/40 bg-accent-2/10' },
  starting: { text: '启动中', cls: 'text-amber-600 border-amber-400/50 bg-amber-400/10' },
  charging: { text: '充电中', cls: 'text-accent border-accent/40 bg-accent/10' },
  stopping: { text: '停止中', cls: 'text-amber-600 border-amber-400/50 bg-amber-400/10' },
  fault: { text: '故障', cls: 'text-rose-600 border-rose-400/40 bg-rose-400/10' },
}

const TARGET_TEXT: Record<string, string> = {
  amount: '金额',
  time: '时长',
  energy: '电量',
  soc: 'SOC',
}

const st = computed(() => STATUS[props.gun.status] ?? { text: props.gun.status || '未知', cls: 'text-mute border-line' })
const busy = computed(() => props.gun.status === 'charging' || props.gun.status === 'starting' || props.gun.status === 'stopping')
const progress = computed(() => Math.round(Math.min(1, Math.max(0, props.gun.progress)) * 100))
const targetValue = computed(() => {
  const t = props.gun.target
  if (!t) return ''
  switch (t.mode) {
    case 'amount':
      return `${t.amount.toFixed(2)} 元`
    case 'time':
      return `${t.minute.toFixed(0)} 分钟`
    case 'energy':
      return `${t.energy.toFixed(2)} kWh`
    case 'soc':
      return `${t.soc}%`
    default:
      return '—'
  }
})

const num = (v: number, d = 1) => (Number.isFinite(v) ? v.toFixed(d) : '0.' + '0'.repeat(d))
</script>

<template>
  <div class="panel p-3.5 flex flex-col gap-3 transition-colors" :class="busy ? 'glow' : ''">
    <!-- 枪头信息 -->
    <div class="flex items-center justify-between gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <span class="text-[13px] font-semibold tracking-wide">枪 {{ gun.no }}</span>
        <span class="px-2 py-0.5 rounded-full border text-[11px] leading-tight" :class="st.cls">{{ st.text }}</span>
      </div>
      <div class="flex items-center gap-1.5 shrink-0">
        <button
          class="px-2 py-1 rounded-md border border-line text-[11px] text-mute hover:text-fg hover:border-accent-2/60 transition"
          :title="gun.plugged ? '拔枪' : '插枪'"
          @click="emit('plug', gun.no, !gun.plugged)"
        >
          {{ gun.plugged ? '拔枪' : '插枪' }}
        </button>
        <button
          v-if="busy"
          class="px-2.5 py-1 rounded-md border border-rose-400/40 text-[11px] text-rose-600 hover:bg-rose-400/10 transition"
          @click="emit('stop', gun.no)"
        >
          停止
        </button>
        <button
          v-else
          class="px-2.5 py-1 rounded-md border border-accent/40 text-[11px] text-accent hover:bg-accent/10 transition disabled:opacity-40"
          :disabled="offline || gun.status === 'fault'"
          @click="emit('start', gun.no)"
        >
          启动充电
        </button>
      </div>
    </div>

    <!-- SOC -->
    <div class="flex items-end gap-3">
      <div class="font-mono text-[26px] leading-none text-fg">{{ gun.soc }}<span class="text-[13px] text-mute ml-0.5">%</span></div>
      <div class="flex-1 pb-1">
        <div class="h-1.5 rounded-full bg-ink-700 overflow-hidden">
          <div
            class="h-full rounded-full transition-[width] duration-500"
            :class="busy ? 'bg-accent' : 'bg-accent-2'"
            :style="{ width: (busy ? progress : gun.soc) + '%' }"
          />
        </div>
        <div class="mt-1 flex justify-between text-[10px] text-mute">
          <span v-if="busy">目标进度 {{ progress }}%</span>
          <span v-else>电池 SOC</span>
          <span v-if="targetValue">目标{{ TARGET_TEXT[gun.target?.mode ?? ''] ?? '' }} {{ targetValue }}</span>
        </div>
      </div>
    </div>

    <!-- 实时参数 -->
    <div class="grid grid-cols-3 gap-1.5">
      <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
        <div class="text-[10px] text-mute">电压</div>
        <div class="font-mono text-[13px]">{{ num(gun.voltage) }}<span class="text-[10px] text-mute ml-0.5">V</span></div>
      </div>
      <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
        <div class="text-[10px] text-mute">电流</div>
        <div class="font-mono text-[13px]">{{ num(gun.current) }}<span class="text-[10px] text-mute ml-0.5">A</span></div>
      </div>
      <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
        <div class="text-[10px] text-mute">功率</div>
        <div class="font-mono text-[13px]">{{ num(gun.power) }}<span class="text-[10px] text-mute ml-0.5">kW</span></div>
      </div>
      <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
        <div class="text-[10px] text-mute">已充电量</div>
        <div class="font-mono text-[13px]">{{ num(gun.energy, 3) }}<span class="text-[10px] text-mute ml-0.5">kWh</span></div>
      </div>
      <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
        <div class="text-[10px] text-mute">金额</div>
        <div class="font-mono text-[13px]">{{ num(gun.amount, 2) }}<span class="text-[10px] text-mute ml-0.5">元</span></div>
      </div>
      <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
        <div class="text-[10px] text-mute">时长 / 剩余</div>
        <div class="font-mono text-[13px]">{{ gun.minutes }}<span class="text-[10px] text-mute mx-0.5">/</span>{{ gun.remainMinutes }}<span class="text-[10px] text-mute ml-0.5">min</span></div>
      </div>
    </div>

    <!-- 结算与环境量 -->
    <div v-if="busy || gun.stopReason" class="space-y-1.5">
      <div class="flex items-center justify-between text-[11px]">
        <span class="text-mute">枪温 / 电池温</span>
        <span class="font-mono">{{ gun.gunTemp }}℃ / {{ gun.batteryTemp }}℃</span>
      </div>
      <div class="flex items-center justify-between text-[11px]">
        <span class="text-mute">BMS 需求</span>
        <span class="font-mono">{{ num(gun.maxVolts) }}V / {{ num(gun.maxAmps) }}A</span>
      </div>
      <div v-if="gun.cardNo" class="flex items-center justify-between text-[11px]">
        <span class="text-mute">{{ gun.startType || '卡号' }}</span>
        <span class="font-mono truncate max-w-[65%]">{{ gun.cardNo || gun.vin }}</span>
      </div>
      <div v-if="gun.stopReason" class="text-[11px] text-amber-600">停止原因：{{ gun.stopReason }}</div>
      <div v-if="gun.waitSeconds > 0" class="text-[11px] text-mute">等待平台确认 {{ gun.waitSeconds }}s</div>
      <div v-if="gun.tradeNo" class="text-[10px] text-mute truncate">流水号 {{ gun.tradeNo }}</div>
    </div>
  </div>
</template>
