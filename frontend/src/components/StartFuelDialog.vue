<script setup lang="ts">
import { ref, watch } from 'vue'
import type { FuelTarget, StartFuelForm, StartFuelRequest } from '../types'

const props = defineProps<{
  open: boolean
  dispenserNo: string
  nozzleNo: string
  last: StartFuelForm | null
}>()
const emit = defineEmits<{
  (e: 'submit', req: StartFuelRequest): void
  (e: 'close'): void
}>()

// 无历史记录时的兜底初值，与后端 store.DefaultStartFuel 保持一致
const DEFAULTS: StartFuelForm = {
  target: { mode: 'amount', amount: 200, volume: 30 } as FuelTarget,
  cardNo: '88888888',
  plateNo: '京A12345',
}

const MODES = [
  { value: 'free', label: '加满', unit: '' },
  { value: 'amount', label: '定额（元）', unit: '元' },
  { value: 'volume', label: '定量（L）', unit: 'L' },
]

const mode = ref(DEFAULTS.target.mode)
const amount = ref(DEFAULTS.target.amount)
const volume = ref(DEFAULTS.target.volume)
const cardNo = ref(DEFAULTS.cardNo)
const plateNo = ref(DEFAULTS.plateNo)

// 每次打开表单回填最近一次填写内容
watch(
  () => props.open,
  (open) => {
    if (!open) return
    const t = props.last?.target ?? DEFAULTS.target
    mode.value = t.mode || DEFAULTS.target.mode
    amount.value = t.amount ?? DEFAULTS.target.amount
    volume.value = t.volume ?? DEFAULTS.target.volume
    cardNo.value = props.last?.cardNo ?? DEFAULTS.cardNo
    plateNo.value = props.last?.plateNo ?? DEFAULTS.plateNo
  },
  { immediate: true },
)

const submit = () => {
  emit('submit', {
    dispenserId: '',
    nozzleNo: props.nozzleNo,
    target: {
      mode: mode.value,
      amount: Number(amount.value) || 0,
      volume: Number(volume.value) || 0,
    } as FuelTarget,
    cardNo: cardNo.value,
    plateNo: plateNo.value,
  })
}
</script>

<template>
  <div v-if="open" class="fixed inset-0 z-50 grid place-items-center bg-black/25 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel glow w-[440px] p-4 space-y-3">
      <div class="flex items-center gap-2">
        <span class="text-[13px] font-semibold">开始加油</span>
        <span class="text-[11px] text-mute font-mono">{{ dispenserNo }} 号机 · {{ nozzleNo }} 号枪</span>
      </div>

      <div class="rounded-lg border border-line bg-ink-850 p-2.5 space-y-2">
        <div class="text-[11px] text-mute">加油目标（定额 / 定量达标后自动停枪并生成订单）</div>
        <div class="flex items-center gap-2">
          <select v-model="mode" class="px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]">
            <option v-for="m in MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
          </select>
          <input
            v-if="mode === 'amount'"
            v-model.number="amount"
            type="number"
            step="0.01"
            min="0"
            class="flex-1 px-2.5 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
          <input
            v-else-if="mode === 'volume'"
            v-model.number="volume"
            type="number"
            step="0.01"
            min="0"
            class="flex-1 px-2.5 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
          <div v-else class="flex-1 text-[11px] text-mute">加入油箱容积后自动跳枪</div>
          <span class="text-[11px] text-mute w-8">{{ MODES.find((m) => m.value === mode)?.unit }}</span>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-2.5">
        <label class="block">
          <span class="text-[11px] text-mute">卡号</span>
          <input v-model="cardNo" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]" />
        </label>
        <label class="block">
          <span class="text-[11px] text-mute">车牌号</span>
          <input v-model="plateNo" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]" />
        </label>
      </div>

      <div class="flex justify-end gap-2 pt-1">
        <button class="px-3 py-1.5 rounded-md border border-line text-[12px] text-mute hover:text-fg transition" @click="emit('close')">取消</button>
        <button class="px-3 py-1.5 rounded-md border border-accent/50 text-[12px] text-accent hover:bg-accent/10 transition" @click="submit">开始加油</button>
      </div>
    </div>
  </div>
</template>
