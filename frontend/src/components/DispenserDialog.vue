<script setup lang="ts">
import { ref, watch } from 'vue'
import type { DispenserConfig } from '../types'

const props = defineProps<{ open: boolean; editing: DispenserConfig | null; suggest: string }>()
const emit = defineEmits<{ (e: 'submit', cfg: DispenserConfig): void; (e: 'close'): void }>()

const DEFAULTS: DispenserConfig = {
  id: '',
  dispenserNo: '',
  nozzleCount: 2,
  nozzles: [],
}

const form = ref<DispenserConfig>({ ...DEFAULTS })

watch(
  () => [props.open, props.editing] as const,
  ([open, editing]) => {
    if (!open) return
    form.value = editing
      ? { ...DEFAULTS, ...editing, nozzles: (editing.nozzles ?? []).map((n) => ({ ...n })) }
      : { ...DEFAULTS, dispenserNo: props.suggest }
  },
  { immediate: true },
)
</script>

<template>
  <div v-if="open" class="fixed inset-0 z-50 grid place-items-center bg-black/25 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel glow w-[420px] p-4 space-y-3">
      <div class="text-[13px] font-semibold">{{ editing?.id ? '编辑加油机' : '新增加油机' }}</div>

      <div class="space-y-2.5">
        <label class="block">
          <span class="text-[11px] text-mute">油机号</span>
          <input
            v-model="form.dispenserNo"
            class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
          />
        </label>
        <label class="block">
          <span class="text-[11px] text-mute">油枪数量（1 ~ 8，枪号自动按 1 ~ x 排列）</span>
          <input
            v-model.number="form.nozzleCount"
            type="number"
            min="1"
            max="8"
            class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          />
        </label>
      </div>

      <div class="flex justify-end gap-2 pt-1">
        <button class="px-3 py-1.5 rounded-md border border-line text-[12px] text-mute hover:text-fg transition" @click="emit('close')">
          取消
        </button>
        <button class="px-3 py-1.5 rounded-md border border-accent/50 text-[12px] text-accent hover:bg-accent/10 transition" @click="emit('submit', { ...form })">
          保存
        </button>
      </div>
    </div>
  </div>
</template>
