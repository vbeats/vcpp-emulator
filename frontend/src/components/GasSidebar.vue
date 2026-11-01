<script setup lang="ts">
import { ref } from 'vue'

interface Nozzle {
  no: number
  fuel: string
  status: string
}

interface Dispenser {
  id: string
  no: string
  nozzles: Nozzle[]
  price: number
}

// 演示数据：加油站模块尚未接入后端，这里仅用于界面骨架预览
const FUELS = ['92#', '95#', '98#', '0# 柴油']

const dispensers = ref<Dispenser[]>([
  {
    id: 'demo-1',
    no: '01000001',
    price: 7.68,
    nozzles: [
      { no: 1, fuel: '92#', status: '待机' },
      { no: 2, fuel: '95#', status: '待机' },
    ],
  },
  {
    id: 'demo-2',
    no: '01000002',
    price: 8.19,
    nozzles: [
      { no: 1, fuel: '98#', status: '待机' },
      { no: 2, fuel: '0# 柴油', status: '待机' },
    ],
  },
])

const selected = ref('demo-1')
</script>

<template>
  <aside class="panel flex flex-col h-full min-h-0 w-[248px] shrink-0">
    <header class="flex items-center gap-2 px-3 py-2 border-b border-line">
      <span class="text-[12px] font-semibold">加油机</span>
      <span class="px-1.5 py-0.5 rounded border border-line text-[10px] text-mute">预留</span>
      <div class="flex-1" />
      <button class="px-2 py-0.5 rounded-md border border-line text-[11px] text-mute cursor-not-allowed opacity-50" disabled>
        + 新增
      </button>
    </header>

    <div class="flex-1 min-h-0 overflow-y-auto p-2 space-y-1.5">
      <div
        v-for="d in dispensers"
        :key="d.id"
        class="rounded-lg border px-2.5 py-2 cursor-pointer transition"
        :class="d.id === selected ? 'border-accent-2/60 bg-accent-2/10' : 'border-line hover:border-accent-2/40 hover:bg-ink-850'"
        @click="selected = d.id"
      >
        <div class="flex items-center gap-2">
          <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-mute" />
          <span class="font-mono text-[12px] truncate flex-1">{{ d.no }}</span>
          <span class="text-[10px] text-mute">未启用</span>
        </div>
        <div class="mt-1 flex items-center gap-2 text-[10px] text-mute">
          <span>{{ d.nozzles.length }} 枪</span>
          <span v-for="f in d.nozzles.map((n) => n.fuel)" :key="f">{{ f }}</span>
        </div>
      </div>

      <div class="px-2 py-3 text-[10px] text-mute leading-relaxed">
        以上为静态示例数据，用于确认界面布局；<br />
        加油机接入仍在开发中。
      </div>
    </div>

    <footer class="px-3 py-2 border-t border-line text-[10px] text-mute truncate">油品：{{ FUELS.join(' / ') }}</footer>
  </aside>
</template>
