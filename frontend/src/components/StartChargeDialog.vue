<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ChargeTarget, StartChargeForm } from '../types'

const props = defineProps<{
  open: boolean
  pileNo: string
  gunNo: number
  last: StartChargeForm | null
}>()
const emit = defineEmits<{
  (e: 'submit', req: {
    pileId: string
    gunNo: number
    startType: number
    cardNo: string
    vin: string
    target: ChargeTarget
    batteryKwh: number
    startSoc: number
    maxVoltage: number
    maxCurrent: number
  }): void
  (e: 'close'): void
}>()

// 无历史记录时的兜底初值，与后端 store.DefaultStartCharge 保持一致
const DEFAULTS = {
  startType: 1,
  cardNo: '88888888',
  vin: '',
  batteryKwh: 60,
  startSoc: 20,
  maxVoltage: 750,
  maxCurrent: 250,
  target: { mode: 'amount', amount: 30, minute: 30, energy: 20, soc: 90 } as ChargeTarget,
}

const startType = ref(DEFAULTS.startType)
const cardNo = ref(DEFAULTS.cardNo)
const vin = ref(DEFAULTS.vin)
const batteryKwh = ref(DEFAULTS.batteryKwh)
const startSoc = ref(DEFAULTS.startSoc)
const maxVoltage = ref(DEFAULTS.maxVoltage)
const maxCurrent = ref(DEFAULTS.maxCurrent)
const mode = ref(DEFAULTS.target.mode)
const amount = ref(DEFAULTS.target.amount)
const minute = ref(DEFAULTS.target.minute)
const energy = ref(DEFAULTS.target.energy)
const soc = ref(DEFAULTS.target.soc)

// 每次打开表单回填最近一次填写内容
watch(
  () => props.open,
  (open) => {
    if (!open) return
    const f = props.last
    const t = f?.target ?? DEFAULTS.target
    startType.value = f?.startType === 3 ? 3 : 1
    cardNo.value = f?.cardNo ?? DEFAULTS.cardNo
    vin.value = f?.vin ?? DEFAULTS.vin
    batteryKwh.value = f?.batteryKwh ?? DEFAULTS.batteryKwh
    startSoc.value = f?.startSoc ?? DEFAULTS.startSoc
    maxVoltage.value = f?.maxVoltage ?? DEFAULTS.maxVoltage
    maxCurrent.value = f?.maxCurrent ?? DEFAULTS.maxCurrent
    mode.value = t.mode || DEFAULTS.target.mode
    amount.value = t.amount ?? DEFAULTS.target.amount
    minute.value = t.minute ?? DEFAULTS.target.minute
    energy.value = t.energy ?? DEFAULTS.target.energy
    soc.value = t.soc ?? DEFAULTS.target.soc
  },
  { immediate: true },
)

const MODE_OPTIONS = [
  { value: 'amount', label: '金额', unit: '元' },
  { value: 'time', label: '时长', unit: '分钟' },
  { value: 'energy', label: '电量', unit: 'kWh' },
  { value: 'soc', label: 'SOC', unit: '%' },
]

const unit = computed(() => MODE_OPTIONS.find((m) => m.value === mode.value)?.unit ?? '')

// 四个目标值全部回传（而非只回传当前模式），保证最近填写的内容完整落库
const target = (): ChargeTarget => ({
  mode: mode.value,
  amount: Number(amount.value) || 0,
  minute: Number(minute.value) || 0,
  energy: Number(energy.value) || 0,
  soc: Number(soc.value) || 0,
})

const submit = () => {
  emit('submit', {
    pileId: '',
    gunNo: props.gunNo,
    startType: startType.value,
    // 卡号与 VIN 同时回传，由后端按启动方式裁剪；两个字段都会各自被记住
    cardNo: cardNo.value,
    vin: vin.value,
    target: target(),
    batteryKwh: Number(batteryKwh.value),
    startSoc: Number(startSoc.value),
    maxVoltage: Number(maxVoltage.value),
    maxCurrent: Number(maxCurrent.value),
  })
}
</script>

<template>
  <div v-if="open" class="fixed inset-0 z-50 grid place-items-center bg-black/25 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel glow w-[460px] p-4 space-y-3">
      <div class="flex items-center gap-2">
        <span class="text-[13px] font-semibold">启动充电</span>
        <span class="text-[11px] text-mute font-mono">{{ pileNo }} · 枪 {{ gunNo }}</span>
      </div>

      <div class="grid grid-cols-2 gap-2.5">
        <label class="block">
          <span class="text-[11px] text-mute">启动方式</span>
          <select v-model.number="startType" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]">
            <option :value="1">刷卡</option>
            <option :value="3">VIN</option>
          </select>
        </label>
        <label v-if="startType === 1" class="block">
          <span class="text-[11px] text-mute">卡号</span>
          <input v-model="cardNo" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]" />
        </label>
        <label v-else class="block">
          <span class="text-[11px] text-mute">VIN</span>
          <input v-model="vin" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]" />
        </label>
        <label class="block">
          <span class="text-[11px] text-mute">电池容量 kWh</span>
          <input v-model.number="batteryKwh" type="number" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]" />
        </label>
        <label class="block">
          <span class="text-[11px] text-mute">起始 SOC %</span>
          <input v-model.number="startSoc" type="number" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]" />
        </label>
        <label class="block">
          <span class="text-[11px] text-mute">最高允许电压 V</span>
          <input v-model.number="maxVoltage" type="number" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]" />
        </label>
        <label class="block">
          <span class="text-[11px] text-mute">最高允许电流 A</span>
          <input v-model.number="maxCurrent" type="number" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]" />
        </label>
      </div>

      <div class="rounded-lg border border-line bg-ink-850 p-2.5 space-y-2">
        <div class="text-[11px] text-mute">充电目标（达标后自动停止并上送交易记录）</div>
        <div class="flex items-center gap-2">
          <select v-model="mode" class="px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]">
            <option v-for="m in MODE_OPTIONS" :key="m.value" :value="m.value">{{ m.label }}</option>
          </select>
          <input
            v-if="mode === 'amount'"
            v-model.number="amount"
            type="number"
            class="flex-1 px-2.5 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
          <input
            v-else-if="mode === 'time'"
            v-model.number="minute"
            type="number"
            class="flex-1 px-2.5 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
          <input
            v-else-if="mode === 'energy'"
            v-model.number="energy"
            type="number"
            class="flex-1 px-2.5 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
          <input
            v-else
            v-model.number="soc"
            type="number"
            class="flex-1 px-2.5 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
          <span class="text-[11px] text-mute w-10">{{ unit }}</span>
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-1">
        <button class="px-3 py-1.5 rounded-md border border-line text-[12px] text-mute hover:text-fg transition" @click="emit('close')">取消</button>
        <button class="px-3 py-1.5 rounded-md border border-accent/50 text-[12px] text-accent hover:bg-accent/10 transition" @click="submit">启动</button>
      </div>
    </div>
  </div>
</template>
