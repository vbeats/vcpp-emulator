<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { Entry } from '../types'

const props = defineProps<{ logs: Entry[]; logDir?: string }>()

const keyword = ref('')
const autoScroll = ref(true)
const expanded = ref<number[]>([])
const body = ref<HTMLDivElement | null>(null)

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return props.logs
  return props.logs.filter(
    (e) =>
      e.message.toLowerCase().includes(kw) ||
      e.cmd.toLowerCase().includes(kw) ||
      e.pile.toLowerCase().includes(kw) ||
      (e.hex ?? '').toLowerCase().includes(kw),
  )
})

const dirClass = (dir: string) => {
  if (dir === 'TX') return 'text-accent border-accent/40'
  if (dir === 'RX') return 'text-accent-2 border-accent-2/40'
  return 'text-mute border-line'
}

const levelDot = (level: string) => {
  if (level === 'error') return 'bg-rose-500'
  if (level === 'warn') return 'bg-amber-500'
  return 'bg-accent'
}

const toggle = (id: number) => {
  const i = expanded.value.indexOf(id)
  if (i >= 0) expanded.value.splice(i, 1)
  else expanded.value.push(id)
}

const toBottom = async () => {
  await nextTick()
  const el = body.value
  if (el) el.scrollTop = el.scrollHeight
}

watch(
  () => props.logs.length,
  () => {
    if (autoScroll.value) void toBottom()
  },
)
</script>

<template>
  <section class="panel flex flex-col h-full min-h-0">
    <header class="flex items-center gap-2 px-3 py-2 border-b border-line">
      <span class="text-[12px] font-semibold">通信日志</span>
      <span class="text-[10px] text-mute">{{ filtered.length }} / {{ props.logs.length }}</span>
      <div class="flex-1" />
      <input
        v-model="keyword"
        placeholder="过滤关键字"
        class="w-36 px-2 py-1 rounded-md bg-white border border-line text-[11px] outline-none focus:border-accent-2/60"
      />
      <label class="flex items-center gap-1 text-[11px] text-mute cursor-pointer select-none">
        <input v-model="autoScroll" type="checkbox" class="accent-accent" />
        滚动
      </label>
    </header>

    <div ref="body" class="flex-1 min-h-0 overflow-y-auto px-2 py-1.5 font-mono text-[11px] leading-[17px]">
      <div
        v-for="e in filtered"
        :key="e.id"
        class="px-1.5 py-0.5 rounded cursor-pointer hover:bg-ink-850"
        :class="e.level === 'error' ? 'text-rose-600' : e.level === 'warn' ? 'text-amber-600' : ''"
        @click="e.hex && toggle(e.id)"
      >
        <div class="flex items-start gap-1.5">
          <span class="mt-[6px] w-1.5 h-1.5 rounded-full shrink-0" :class="levelDot(e.level)" />
          <span class="text-mute shrink-0">{{ e.time }}</span>
          <span class="px-1 rounded border text-[10px] leading-4 shrink-0" :class="dirClass(e.dir)">{{ e.dir }}</span>
          <span v-if="e.pile" class="text-mute shrink-0">{{ e.pile }}<span v-if="e.gun">·{{ e.gun }}</span></span>
          <span v-if="e.cmd" class="text-accent-2 shrink-0">{{ e.cmd }}</span>
          <span class="flex-1 break-all">{{ e.message }}</span>
        </div>
        <pre v-if="e.hex && expanded.includes(e.id)" class="mt-1 mb-1 px-2 py-1.5 rounded bg-ink-850 border border-line/60 whitespace-pre-wrap break-all text-[10px] text-mute">{{ e.hex }}</pre>
      </div>
      <div v-if="!filtered.length" class="px-2 py-6 text-center text-mute">暂无日志</div>
    </div>

    <footer v-if="logDir" class="px-3 py-1.5 border-t border-line text-[10px] text-mute truncate">日志目录：{{ logDir }}</footer>
  </section>
</template>
