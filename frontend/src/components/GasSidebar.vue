<script setup lang="ts">
import { ref } from 'vue'
import type { DispenserView } from '../types'

const props = defineProps<{ dispensers: DispenserView[]; selectedId: string }>()
const emit = defineEmits<{
  (e: 'select', id: string): void
  (e: 'add'): void
  (e: 'edit', d: DispenserView): void
  (e: 'remove', id: string): void
  (e: 'config'): void
  (e: 'generate', count: number, nozzleCount: number): void
}>()

const STATUS: Record<string, { text: string; dot: string }> = {
  offline: { text: '已停止', dot: 'bg-mute' },
  idle: { text: '待机', dot: 'bg-accent' },
  fueling: { text: '加油中', dot: 'bg-accent animate-pulse' },
  fault: { text: '故障', dot: 'bg-rose-400' },
}

const st = (s: string) => STATUS[s] ?? { text: s || '未知', dot: 'bg-mute' }
const fuelingCount = (d: DispenserView) => d.nozzles?.filter((n) => n.status === 'fueling').length ?? 0

// 批量生成参数仅在界面使用，生成结果落在 gas.dispensers，不写入配置文件
const genCount = ref(props.dispensers.length || 1)
const genNozzles = ref(props.dispensers[0]?.nozzleCount || 2)
</script>

<template>
  <aside class="panel flex flex-col h-full min-h-0 w-[248px] shrink-0">
    <header class="flex items-center gap-2 px-3 py-2 border-b border-line">
      <span class="text-[12px] font-semibold">加油机</span>
      <span class="text-[10px] text-mute">{{ props.dispensers.length }}</span>
      <div class="flex-1" />
      <button
        class="px-2 py-0.5 rounded-md border border-line text-[11px] text-mute hover:text-fg transition"
        title="数据推送配置（全站公共）"
        @click="emit('config')"
      >
        推送配置
      </button>
      <button
        class="px-2 py-0.5 rounded-md border border-accent/40 text-[11px] text-accent hover:bg-accent/10 transition"
        @click="emit('add')"
      >
        + 新增
      </button>
    </header>

    <div class="flex-1 min-h-0 overflow-y-auto p-2 space-y-1.5">
      <div
        v-for="d in props.dispensers"
        :key="d.id"
        class="group rounded-lg border px-2.5 py-2 cursor-pointer transition"
        :class="d.id === props.selectedId ? 'border-accent-2/60 bg-accent-2/10' : 'border-line hover:border-accent-2/40 hover:bg-ink-850'"
        @click="emit('select', d.id)"
      >
        <div class="flex items-center gap-2">
          <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="st(d.status).dot" />
          <span class="font-mono text-[12px] truncate flex-1">{{ d.dispenserNo }}</span>
          <span class="text-[10px] text-mute">{{ st(d.status).text }}</span>
        </div>
        <div class="mt-1 flex items-center gap-1.5 text-[10px] text-mute">
          <span>{{ d.nozzleCount }} 枪</span>
          <span
            v-for="n in d.nozzles ?? []"
            :key="n.no"
            class="px-1 rounded border border-line"
            :style="{ color: n.fuelColor || '#6f7b93' }"
          >
            {{ n.fuelCode }}
          </span>
        </div>
        <div v-if="fuelingCount(d)" class="mt-1 text-[10px] text-accent">加油 {{ fuelingCount(d) }}</div>
        <div class="mt-1.5 flex items-center gap-1.5 opacity-0 group-hover:opacity-100 transition">
          <button class="px-1.5 py-0.5 rounded border border-line text-[10px] hover:text-fg" @click.stop="emit('edit', d)">编辑</button>
          <button class="px-1.5 py-0.5 rounded border border-line text-[10px] text-rose-600 hover:border-rose-400/60" @click.stop="emit('remove', d.id)">
            删除
          </button>
        </div>
      </div>

      <div v-if="!props.dispensers.length" class="px-2 py-8 text-center text-[11px] text-mute">
        暂无加油机，点击「+ 新增」添加
      </div>
    </div>

    <!-- 批量生成油机：台数 × 每台枪数，枪号自动按 1 ~ x 排列 -->
    <footer class="border-t border-line px-2.5 py-2 space-y-1.5">
      <div class="flex items-center gap-2 text-[11px] text-mute">
        <span>台数</span>
        <input
          v-model.number="genCount"
          type="number"
          min="1"
          max="64"
          class="w-16 px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60"
        />
        <span>×</span>
        <input
          v-model.number="genNozzles"
          type="number"
          min="1"
          max="8"
          class="w-16 px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60"
        />
        <span>枪</span>
      </div>
      <button
        class="w-full px-2.5 py-1 rounded-md border border-accent/50 text-[11px] text-accent hover:bg-accent/10 transition"
        title="按台数与每台枪数重新生成油机列表（覆盖现有列表）"
        @click="emit('generate', genCount, genNozzles)"
      >
        批量生成油机
      </button>
    </footer>
  </aside>
</template>
