<script setup lang="ts">
import { computed, ref } from 'vue'
import NozzleCard from './NozzleCard.vue'
import type { DispenserView, FuelOption, FuelOrder, PushMessage } from '../types'

const props = defineProps<{
  dispenser: DispenserView | null
  fuels: FuelOption[]
  orders: FuelOrder[]
  pushes: PushMessage[]
  pushTarget: string
}>()
const emit = defineEmits<{
  (e: 'edit', d: DispenserView): void
  (e: 'start', nozzleNo: string): void
  (e: 'stop', nozzleNo: string): void
  (e: 'lift', nozzleNo: string, lifted: boolean): void
  (e: 'fuel', nozzleNo: string, code: string): void
}>()

const SHOW_PUSH = 20
const SHOW_ORDER = 8

const expanded = ref<string[]>([])

const recentOrders = computed(() => (props.orders ?? []).slice(0, SHOW_ORDER))
const recentPushes = computed(() => (props.pushes ?? []).slice(-SHOW_PUSH).slice().reverse())

const STATUS: Record<string, string> = {
  offline: 'text-mute border-line',
  idle: 'text-accent border-accent/40 bg-accent/10',
  fueling: 'text-accent border-accent/40 bg-accent/10',
  fault: 'text-rose-600 border-rose-400/40 bg-rose-400/10',
}

const st = computed(() => STATUS[props.dispenser?.status ?? ''] ?? 'text-mute border-line')

const num = (v: number | null | undefined, d = 2) =>
  Number.isFinite(v as number) ? (v as number).toFixed(d) : (0).toFixed(d)

const channelText = (c: string) =>
  c === 'mqtt' ? 'MQTT' : c === 'http' ? 'HTTP' : '未启用'

const kindText = (k: string) => (k === 'order' ? '加油订单' : '加油进度')

const toggle = (id: string) => {
  const i = expanded.value.indexOf(id)
  if (i >= 0) expanded.value.splice(i, 1)
  else expanded.value.push(id)
}
</script>

<template>
  <section class="panel flex flex-col h-full min-h-0">
    <header v-if="dispenser" class="flex items-center gap-2 px-3.5 py-2.5 border-b border-line">
      <span class="font-mono text-[13px]">{{ dispenser.dispenserNo }}</span>
      <span class="px-2 py-0.5 rounded-full border text-[10px]" :class="st">
        {{ dispenser.statusText || dispenser.status }}
      </span>
      <span class="text-[11px] text-mute">{{ dispenser.nozzleCount }} 把油枪</span>
      <div class="flex-1" />
      <button class="px-2.5 py-1 rounded-md border border-line text-[11px] hover:text-fg transition" @click="emit('edit', dispenser)">
        编辑
      </button>
    </header>

    <div v-if="!dispenser" class="flex-1 grid place-items-center text-[12px] text-mute">
      选择左侧加油机查看实时数据
    </div>

    <div v-else class="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <!-- 油站概览（加油量 / 金额按枪展示，见下方油枪卡片） -->
      <div class="grid grid-cols-2 gap-1.5">
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2.5 py-1.5">
          <div class="text-[10px] text-mute">加油机</div>
          <div class="text-[12px] truncate">{{ dispenser.dispenserNo }}</div>
        </div>
        <div class="rounded-lg bg-ink-850 border border-line/60 px-2.5 py-1.5">
          <div class="text-[10px] text-mute">油枪数</div>
          <div class="text-[12px] truncate">{{ dispenser.nozzleCount }}</div>
        </div>
      </div>

      <div class="rounded-xl bg-ink-850 border border-line px-3 py-2.5">
        <div class="text-[12px] font-semibold">统一油价</div>
        <div class="mt-1.5 flex flex-wrap gap-1.5">
          <span
            v-for="f in fuels"
            :key="f.code"
            class="px-2 py-0.5 rounded-full border border-line text-[10px]"
            :style="{ color: f.color }"
          >
            {{ f.code }} · {{ f.category }}
          </span>
        </div>
        <div class="mt-1.5 text-[10px] text-mute">数据推送（全站公共）：{{ pushTarget }}</div>
        <div class="mt-0.5 text-[10px] text-mute">油价与推送通道均为油站公共配置，请在左侧「推送配置」中修改。</div>
      </div>

      <!-- 油枪卡片 -->
      <div class="grid gap-3" :class="dispenser.nozzleCount > 1 ? 'grid-cols-2' : 'grid-cols-1'">
        <NozzleCard
          v-for="n in dispenser.nozzles ?? []"
          :key="n.no"
          :nozzle="n"
          :fuels="fuels"
          @start="emit('start', $event)"
          @stop="emit('stop', $event)"
          @lift="(no, lifted) => emit('lift', no, lifted)"
          @fuel="(no, code) => emit('fuel', no, code)"
        />
      </div>

      <!-- 最近订单 -->
      <div>
        <div class="mb-1.5 flex items-center gap-2">
          <span class="text-[12px] font-semibold">最近加油订单</span>
          <span class="text-[10px] text-mute">{{ recentOrders.length }} 条</span>
        </div>
        <div v-if="!recentOrders.length" class="rounded-xl border border-line border-dashed px-3 py-6 text-center text-[11px] text-mute">
          暂无订单，发起加油并完成挂枪后在此展示
        </div>
        <div v-else class="rounded-xl border border-line overflow-hidden">
          <table class="w-full text-[11px]">
            <thead class="bg-ink-850 text-mute">
              <tr>
                <th class="px-2.5 py-1.5 text-left font-medium">订单号</th>
                <th class="px-2.5 py-1.5 text-left font-medium">机 / 枪</th>
                <th class="px-2.5 py-1.5 text-left font-medium">油品</th>
                <th class="px-2.5 py-1.5 text-right font-medium">升数</th>
                <th class="px-2.5 py-1.5 text-right font-medium">金额</th>
                <th class="px-2.5 py-1.5 text-right font-medium">单价</th>
                <th class="px-2.5 py-1.5 text-right font-medium">泵码</th>
                <th class="px-2.5 py-1.5 text-left font-medium">停止原因</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="o in recentOrders" :key="o.orderNo" class="border-t border-line/70">
                <td class="px-2.5 py-1.5 font-mono">{{ o.orderNo }}</td>
                <td class="px-2.5 py-1.5 font-mono">{{ o.dispenserNo }} / {{ o.nozzleNo }}</td>
                <td class="px-2.5 py-1.5" :style="{ color: o.fuelColor || undefined }">{{ o.fuelCode }}</td>
                <td class="px-2.5 py-1.5 text-right font-mono">{{ num(o.volume, 4) }}</td>
                <td class="px-2.5 py-1.5 text-right font-mono">{{ num(o.amount) }}</td>
                <td class="px-2.5 py-1.5 text-right font-mono">{{ num(o.price) }}</td>
                <td class="px-2.5 py-1.5 text-right font-mono">{{ num(o.pumpStart, 4) }} → {{ num(o.pumpEnd, 4) }}</td>
                <td class="px-2.5 py-1.5 text-mute">{{ o.stopReason || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 推送记录 -->
      <div>
        <div class="mb-1.5 flex items-center gap-2">
          <span class="text-[12px] font-semibold">数据推送记录</span>
          <span class="text-[10px] text-mute">最近 {{ recentPushes.length }} 条 · 预留通道，暂不实际外发</span>
        </div>
        <div v-if="!recentPushes.length" class="rounded-xl border border-line border-dashed px-3 py-6 text-center text-[11px] text-mute">
          暂无推送记录
        </div>
        <div v-else class="space-y-1">
          <div
            v-for="p in recentPushes"
            :key="p.id"
            class="rounded-lg border border-line bg-ink-850 px-2.5 py-1.5 cursor-pointer hover:border-accent-2/40"
            @click="toggle(String(p.id))"
          >
            <div class="flex items-center gap-2 text-[11px]">
              <span class="text-mute font-mono">{{ p.time }}</span>
              <span class="px-1 rounded border border-line text-[10px] text-accent-2">{{ channelText(p.channel) }}</span>
              <span class="px-1 rounded border border-line text-[10px] text-mute">{{ kindText(p.kind) }}</span>
              <span class="font-mono text-mute truncate flex-1">{{ p.target }}</span>
            </div>
            <pre
              v-if="expanded.includes(String(p.id))"
              class="mt-1 px-2 py-1.5 rounded bg-white border border-line/60 whitespace-pre-wrap break-all text-[10px] text-mute"
            >{{ p.payload }}</pre>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
