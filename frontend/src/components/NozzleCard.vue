<script setup lang="ts">
import { computed } from 'vue'
import type { FuelOption, NozzleView } from '../types'

const props = defineProps<{ nozzle: NozzleView; fuels: FuelOption[] }>()
const emit = defineEmits<{
  (e: 'start', no: string): void
  (e: 'stop', no: string): void
  (e: 'lift', no: string, lifted: boolean): void
  (e: 'fuel', no: string, code: string): void
}>()

const STATUS: Record<string, string> = {
  idle: 'text-mute border-line',
  lifted: 'text-accent-2 border-accent-2/40 bg-accent-2/10',
  fueling: 'text-accent border-accent/40 bg-accent/10',
  settled: 'text-amber-600 border-amber-400/50 bg-amber-400/10',
  fault: 'text-rose-600 border-rose-400/40 bg-rose-400/10',
}

const num = (v: number | null | undefined, d = 2) =>
  Number.isFinite(v as number) ? (v as number).toFixed(d) : (0).toFixed(d)

const st = computed(() => STATUS[props.nozzle.status] ?? 'text-mute border-line')
const fueling = computed(() => props.nozzle.status === 'fueling')
const busy = computed(() => fueling.value)
const progress = computed(() => Math.round(Math.min(1, Math.max(0, props.nozzle.progress ?? 0)) * 100))
const canFuel = computed(() => props.nozzle.status !== 'fault')

// fuelTargetText 加油目标文案：定额金额 / 定量升数 / 加满
const fuelTargetText = (n: NozzleView): string => {
  const t = n.target
  if (!t) return '加满'
  if (t.mode === 'amount') return `定额 ${(t.amount ?? 0).toFixed(2)} 元`
  if (t.mode === 'volume') return `定量 ${(t.volume ?? 0).toFixed(2)} L`
  return '加满'
}
</script>

<template>
  <div class="panel p-3.5 flex flex-col gap-3 transition-colors" :class="busy ? 'glow' : ''">
    <!-- 枪头信息 -->
    <div class="flex items-center justify-between gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <span class="text-[13px] font-semibold tracking-wide">枪 {{ nozzle.no }}</span>
        <span class="px-2 py-0.5 rounded-full border text-[11px] leading-tight" :class="st">
          {{ nozzle.statusText || nozzle.status }}
        </span>
      </div>
      <div class="flex items-center gap-1.5 shrink-0">
        <button
          class="px-2 py-1 rounded-md border border-line text-[11px] text-mute hover:text-fg hover:border-accent-2/60 transition"
          :title="nozzle.lifted ? '挂枪' : '提枪'"
          @click="emit('lift', nozzle.no, !nozzle.lifted)"
        >
          {{ nozzle.lifted ? '挂枪' : '提枪' }}
        </button>
        <button
          v-if="busy"
          class="px-2.5 py-1 rounded-md border border-rose-400/40 text-[11px] text-rose-600 hover:bg-rose-400/10 transition"
          @click="emit('stop', nozzle.no)"
        >
          停止
        </button>
        <button
          v-else
          class="px-2.5 py-1 rounded-md border border-accent/40 text-[11px] text-accent hover:bg-accent/10 transition disabled:opacity-40"
          :disabled="!canFuel"
          @click="emit('start', nozzle.no)"
        >
          开始加油
        </button>
      </div>
    </div>

      <!-- 油品 -->
      <div class="flex items-center gap-2">
        <span class="w-1 h-8 rounded-full shrink-0" :style="{ background: nozzle.fuelColor || '#6f7b93' }" />
        <div class="min-w-0 flex-1">
          <div class="text-[10px] text-mute">油号 / 油品</div>
          <div class="text-[12px] truncate">{{ nozzle.fuelCode }} · {{ nozzle.fuelName }}</div>
        </div>
        <select
          class="px-2 py-1 rounded-md bg-white border border-line text-[11px] outline-none focus:border-accent-2/60 disabled:opacity-40"
          :value="nozzle.fuelCode"
          :disabled="busy"
          title="修改该枪油号（修改后立即保存）"
          @change="emit('fuel', nozzle.no, ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="f in fuels" :key="f.code" :value="f.code">{{ f.code }}</option>
        </select>
      </div>

      <!-- 升数 -->
      <div class="flex items-end gap-3">
        <div class="font-mono text-[26px] leading-none text-fg">
          {{ num(nozzle.volume, 4) }}<span class="text-[13px] text-mute ml-0.5">L</span>
        </div>
        <div class="flex-1 pb-1">
          <div class="h-1.5 rounded-full bg-ink-700 overflow-hidden">
            <div
              class="h-full rounded-full transition-[width] duration-500"
              :class="busy ? 'bg-accent' : 'bg-accent-2'"
              :style="{ width: (busy ? progress : 0) + '%' }"
            />
          </div>
          <div class="mt-1 flex justify-between text-[10px] text-mute">
            <span v-if="busy">加油进度 {{ progress }}%</span>
            <span v-else>{{ nozzle.settled ? '订单已结算' : '待加油' }}</span>
            <span v-if="busy">{{ num(nozzle.flowRate, 1) }} L/min</span>
          </div>
        </div>
      </div>

      <!-- 实时参数 -->
      <div class="grid grid-cols-3 gap-1.5">
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
          <div class="text-[10px] text-mute">金额</div>
          <div class="font-mono text-[13px]">{{ num(nozzle.amount) }}<span class="text-[10px] text-mute ml-0.5">元</span></div>
        </div>
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
          <div class="text-[10px] text-mute">单价</div>
          <div class="font-mono text-[13px]">{{ num(nozzle.price) }}<span class="text-[10px] text-mute ml-0.5">元/L</span></div>
        </div>
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
          <div class="text-[10px] text-mute">当前泵码</div>
          <div class="font-mono text-[13px]">{{ num(nozzle.pumpEnd, 4) }}</div>
        </div>
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
          <div class="text-[10px] text-mute">起始泵码</div>
          <div class="font-mono text-[13px]">{{ num(nozzle.pumpStart, 4) }}</div>
        </div>
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
          <div class="text-[10px] text-mute">时长</div>
          <div class="font-mono text-[13px]">{{ nozzle.seconds ?? 0 }}<span class="text-[10px] text-mute ml-0.5">s</span></div>
        </div>
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2 py-1.5">
          <div class="text-[10px] text-mute">加油目标</div>
          <div class="text-[12px] truncate">{{ fuelTargetText(nozzle) }}</div>
        </div>
      </div>

      <!-- 订单数据 -->
      <div v-if="nozzle.orderNo" class="rounded-lg border border-line bg-ink-850 px-2.5 py-2 space-y-1">
        <div class="flex items-center justify-between text-[11px]">
          <span class="text-mute">订单号</span>
          <span class="font-mono truncate max-w-[70%]">{{ nozzle.orderNo }}</span>
        </div>
        <div v-if="nozzle.cardNo" class="flex items-center justify-between text-[11px]">
          <span class="text-mute">卡号</span>
          <span class="font-mono truncate max-w-[70%]">{{ nozzle.cardNo }}</span>
        </div>
        <div v-if="nozzle.plateNo" class="flex items-center justify-between text-[11px]">
          <span class="text-mute">车牌</span>
          <span class="font-mono truncate max-w-[70%]">{{ nozzle.plateNo }}</span>
        </div>
        <div class="flex items-center justify-between text-[11px]">
          <span class="text-mute">泵码</span>
          <span class="font-mono">{{ num(nozzle.pumpStart, 4) }} → {{ num(nozzle.pumpEnd, 4) }}</span>
        </div>
        <div class="flex items-center justify-between text-[11px]">
          <span class="text-mute">时间</span>
          <span class="font-mono">{{ nozzle.startAt || '—' }} → {{ nozzle.endAt || '进行中' }}</span>
        </div>
        <div v-if="nozzle.stopReason" class="text-[11px] text-amber-600">停止原因：{{ nozzle.stopReason }}</div>
      </div>
  </div>
</template>
