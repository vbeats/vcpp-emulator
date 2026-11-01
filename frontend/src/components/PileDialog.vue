<script setup lang="ts">
import { ref, watch } from 'vue'
import type { PileConfig } from '../types'

const props = defineProps<{ open: boolean; editing: PileConfig | null; suggest: string }>()
const emit = defineEmits<{ (e: 'submit', cfg: PileConfig): void; (e: 'close'): void }>()

const DEFAULTS: PileConfig = {
  id: '',
  pileNo: '',
  gunCount: 2,
  pileType: 0,
  ratedPower: 120,
  programVer: '220422',
  enabled: true,
}

const form = ref<PileConfig>({ ...DEFAULTS })

watch(
  () => [props.open, props.editing] as const,
  ([open, editing]) => {
    if (!open) return
    form.value = editing ? { ...DEFAULTS, ...editing } : { ...DEFAULTS, pileNo: props.suggest }
  },
  { immediate: true },
)

const submit = () => {
  emit('submit', { ...form.value })
}
</script>

<template>
  <div v-if="open" class="fixed inset-0 z-50 grid place-items-center bg-black/25 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel glow w-[420px] p-4 space-y-3">
      <div class="text-[13px] font-semibold">{{ editing?.id ? '编辑充电桩' : '新增充电桩' }}</div>

      <div class="space-y-2.5">
        <label class="block">
          <span class="text-[11px] text-mute">桩号（14 位数字）</span>
          <input v-model="form.pileNo" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]" />
        </label>
        <div class="grid grid-cols-2 gap-2.5">
          <label class="block">
            <span class="text-[11px] text-mute">枪数量</span>
            <input v-model.number="form.gunCount" type="number" min="1" max="10" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]" />
          </label>
          <label class="block">
            <span class="text-[11px] text-mute">桩类型</span>
            <select v-model.number="form.pileType" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]">
              <option :value="0">直流</option>
              <option :value="1">交流</option>
            </select>
          </label>
          <label class="block">
            <span class="text-[11px] text-mute">额定功率 kW</span>
            <input v-model.number="form.ratedPower" type="number" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]" />
          </label>
          <label class="block">
            <span class="text-[11px] text-mute">程序版本</span>
            <input v-model="form.programVer" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]" />
          </label>
        </div>
        <label class="flex items-center gap-2 text-[11px] text-mute cursor-pointer select-none">
          <input v-model="form.enabled" type="checkbox" class="accent-accent" />
          启用（应用配置了自动连接时，随启动自动建立连接）
        </label>
      </div>

      <div class="flex justify-end gap-2 pt-1">
        <button class="px-3 py-1.5 rounded-md border border-line text-[12px] text-mute hover:text-fg transition" @click="emit('close')">取消</button>
        <button class="px-3 py-1.5 rounded-md border border-accent/50 text-[12px] text-accent hover:bg-accent/10 transition" @click="submit">保存</button>
      </div>
    </div>
  </div>
</template>
