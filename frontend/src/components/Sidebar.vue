<script setup lang="ts">
import type { PileView } from '../types'

defineProps<{ piles: PileView[]; selectedId: string }>()
const emit = defineEmits<{
  (e: 'select', id: string): void
  (e: 'add'): void
  (e: 'edit', pile: PileView): void
  (e: 'remove', id: string): void
  (e: 'toggle', pile: PileView): void
}>()

const STATUS: Record<string, { text: string; dot: string }> = {
  offline: { text: '未连接', dot: 'bg-mute' },
  connecting: { text: '连接中', dot: 'bg-amber-500 animate-pulse' },
  online: { text: '在线', dot: 'bg-accent' },
  error: { text: '异常', dot: 'bg-rose-400' },
}

const st = (s: string) => STATUS[s] ?? { text: s || '未知', dot: 'bg-mute' }
const onlineGuns = (p: PileView) => p.guns?.filter((g) => g.status === 'charging').length ?? 0
</script>

<template>
  <aside class="panel flex flex-col h-full min-h-0 w-[248px] shrink-0">
    <header class="flex items-center gap-2 px-3 py-2 border-b border-line">
      <span class="text-[12px] font-semibold">充电桩</span>
      <span class="text-[10px] text-mute">{{ piles.length }}</span>
      <div class="flex-1" />
      <button
        class="px-2 py-0.5 rounded-md border border-accent/40 text-[11px] text-accent hover:bg-accent/10 transition"
        @click="emit('add')"
      >
        + 新增
      </button>
    </header>

    <div class="flex-1 min-h-0 overflow-y-auto p-2 space-y-1.5">
      <div
        v-for="p in piles"
        :key="p.id"
        class="group rounded-lg border px-2.5 py-2 cursor-pointer transition"
        :class="p.id === selectedId ? 'border-accent-2/60 bg-accent-2/10' : 'border-line hover:border-accent-2/40 hover:bg-ink-850'"
        @click="emit('select', p.id)"
      >
        <div class="flex items-center gap-2">
          <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="st(p.status).dot" />
          <span class="font-mono text-[12px] truncate flex-1">{{ p.pileNo }}</span>
          <span class="text-[10px] text-mute">{{ st(p.status).text }}</span>
        </div>
        <div class="mt-1 flex items-center gap-2 text-[10px] text-mute">
          <span>{{ p.versionDesc || p.version }}</span>
          <span>{{ p.gunCount }} 枪</span>
          <span v-if="onlineGuns(p)" class="text-accent">充电 {{ onlineGuns(p) }}</span>
        </div>
        <div v-if="p.lastError" class="mt-1 text-[10px] text-rose-600 truncate" :title="p.lastError">{{ p.lastError }}</div>
        <div class="mt-1.5 flex items-center gap-1.5 opacity-0 group-hover:opacity-100 transition">
          <button class="px-1.5 py-0.5 rounded border border-line text-[10px] hover:text-fg" @click.stop="emit('toggle', p)">
            {{ p.status === 'offline' ? '启动' : '停止' }}
          </button>
          <button class="px-1.5 py-0.5 rounded border border-line text-[10px] hover:text-fg" @click.stop="emit('edit', p)">编辑</button>
          <button class="px-1.5 py-0.5 rounded border border-line text-[10px] text-rose-600 hover:border-rose-400/60" @click.stop="emit('remove', p.id)">
            删除
          </button>
        </div>
      </div>

      <div v-if="!piles.length" class="px-2 py-8 text-center text-[11px] text-mute">暂无充电桩，点击「+ 新增」添加</div>
    </div>
  </aside>
</template>
