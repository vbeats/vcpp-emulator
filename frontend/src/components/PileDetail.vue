<script setup lang="ts">
import { computed } from 'vue'
import GunCard from './GunCard.vue'
import type { GunView, PileView } from '../types'

const props = defineProps<{ pile: PileView | null }>()
const emit = defineEmits<{
  (e: 'edit', pile: PileView): void
  (e: 'toggle', pile: PileView): void
  (e: 'start', gun: number): void
  (e: 'stop', gun: number): void
  (e: 'plug', gun: number, plugged: boolean): void
}>()

const offline = computed(() => !props.pile || props.pile.status !== 'online')

const STATUS: Record<string, string> = {
  offline: '未连接',
  connecting: '连接中',
  online: '在线',
  error: '异常',
}

const item = (label: string, value: string | number) => ({ label, value })
const items = computed(() => {
  const p = props.pile
  if (!p) return []
  return [
    item('桩号', p.pileNo),
    item('协议版本', `${p.versionDesc || p.version} (${p.version})`),
    item('服务端', p.addr),
    item('桩类型', p.pileType),
    item('额定功率', `${p.ratedPower} kW`),
    item('程序版本', p.programVer),
    item('费率', p.billing || '—'),
    item('帧计数', `${p.sentFrames} ↑ / ${p.recvFrames} ↓`),
    item('连接时间', p.connectedAt || '—'),
  ]
})
</script>

<template>
  <section class="panel flex flex-col h-full min-h-0">
    <header v-if="pile" class="flex items-center gap-2 px-3.5 py-2.5 border-b border-line">
      <span class="font-mono text-[13px]">{{ pile.pileNo }}</span>
      <span
        class="px-2 py-0.5 rounded-full border text-[10px]"
        :class="pile.status === 'online' ? 'text-accent border-accent/40 bg-accent/10' : 'text-mute border-line'"
      >
        {{ STATUS[pile.status] ?? pile.status }}
      </span>
      <span class="text-[11px] text-mute">{{ pile.versionDesc }} · {{ pile.gunCount }} 枪</span>
      <div class="flex-1" />
      <button class="px-2.5 py-1 rounded-md border border-line text-[11px] hover:text-fg transition" @click="emit('edit', pile)">
        编辑
      </button>
      <button
        class="px-2.5 py-1 rounded-md border text-[11px] transition"
        :class="pile.status === 'offline' ? 'border-accent/40 text-accent hover:bg-accent/10' : 'border-line text-mute hover:text-fg'"
        @click="emit('toggle', pile)"
      >
        {{ pile.status === 'offline' ? '启动连接' : '断开连接' }}
      </button>
    </header>

    <div v-if="!pile" class="flex-1 grid place-items-center text-[12px] text-mute">选择左侧充电桩查看实时数据</div>

    <div v-else class="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <div class="grid grid-cols-3 gap-1.5">
        <div v-for="it in items" :key="it.label" class="rounded-lg bg-ink-850 border border-line/60 px-2.5 py-1.5">
          <div class="text-[10px] text-mute">{{ it.label }}</div>
          <div class="text-[12px] truncate" :title="String(it.value)">{{ it.value }}</div>
        </div>
      </div>

      <div v-if="pile.lastError" class="rounded-lg border border-rose-400/40 bg-rose-400/10 px-2.5 py-1.5 text-[11px] text-rose-600">
        {{ pile.lastError }}
      </div>

      <div class="grid gap-3" :class="pile.gunCount > 1 ? 'grid-cols-2' : 'grid-cols-1'">
        <GunCard
          v-for="g in pile.guns ?? []"
          :key="g.no"
          :gun="g as GunView"
          :offline="offline"
          @start="emit('start', $event)"
          @stop="emit('stop', $event)"
          @plug="(no, plugged) => emit('plug', no, plugged)"
        />
      </div>
    </div>
  </section>
</template>
