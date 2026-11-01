<script setup lang="ts">
import { ref, watch } from 'vue'
import type { FuelOption, GasConfig } from '../types'

const props = defineProps<{ open: boolean; gas: GasConfig | null; fuels: FuelOption[] }>()
const emit = defineEmits<{
  (e: 'submit', gas: GasConfig): void
  (e: 'close'): void
}>()

const DEFAULTS: GasConfig = {
  pushMode: 'mqtt',
  mqtt: {
    broker: '10.88.0.81',
    port: 1883,
    clientId: 'vcpp-emulator-gas',
    username: 'admin',
    password: 'admin123456',
    topicProgress: 'vcpp/gas/{dispenser}/nozzle/{nozzle}/progress',
    topicOrder: 'vcpp/gas/{dispenser}/nozzle/{nozzle}/order',
    qos: 1,
  },
  http: {
    baseUrl: 'http://10.88.0.81:18083',
    pathProgress: '/api/gas/fuel/progress',
    pathOrder: '/api/gas/fuel/order',
    token: '',
    timeoutSec: 10,
  },
  prices: [],
  dispensers: [],
  pushIntervalSec: 5,
  tankVolumeL: 60,
  lastStartFuel: { target: { mode: 'amount', amount: 200, volume: 30 }, cardNo: '88888888', plateNo: '京A12345' },
}

const form = ref<GasConfig>({ ...DEFAULTS })

const PUSH_MODES = [
  { value: 'none', label: '不推送' },
  { value: 'mqtt', label: 'MQTT' },
  { value: 'http', label: 'HTTP' },
]

// 每次打开表单按当前持久化配置回填
watch(
  () => [props.open, props.gas] as const,
  ([open, gas]) => {
    if (!open) return
    const base = gas ?? DEFAULTS
    const saved = new Map((base.prices ?? []).map((p) => [p.code, p.price]))
    form.value = {
      ...DEFAULTS,
      ...base,
      mqtt: { ...DEFAULTS.mqtt, ...(base.mqtt ?? {}) },
      http: { ...DEFAULTS.http, ...(base.http ?? {}) },
      prices: props.fuels.map((f) => ({ code: f.code, price: saved.get(f.code) ?? 0 })),
      dispensers: (base.dispensers ?? []).map((d) => ({ ...d, nozzles: (d.nozzles ?? []).map((n) => ({ ...n })) })),
      lastStartFuel: { ...DEFAULTS.lastStartFuel, ...(base.lastStartFuel ?? {}) },
    }
  },
  { immediate: true },
)

const priceOf = (code: string) => (form.value.prices ?? []).find((p) => p.code === code)?.price ?? 0

const setPrice = (code: string, v: string) => {
  const p = (form.value.prices ?? []).find((it) => it.code === code)
  if (p) p.price = Number(v) || 0
}
</script>

<template>
  <div v-if="open" class="fixed inset-0 z-50 grid place-items-center bg-black/25 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel glow w-[620px] max-h-[86vh] overflow-y-auto p-4 space-y-4">
      <div class="text-[13px] font-semibold">数据推送配置（全站公共）</div>
      <div class="-mt-2 text-[10px] text-mute">推送通道与油价属于油站公共资源，所有油机共用，不随单台油机分别配置。</div>

      <!-- 数据推送形式 -->
      <section class="space-y-2">
        <div class="text-[12px] font-medium">数据推送形式</div>
        <div class="flex items-center gap-2">
          <select
            v-model="form.pushMode"
            class="px-2 py-1.5 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
          >
            <option v-for="m in PUSH_MODES" :key="m.value" :value="m.value">{{ m.label }}</option>
          </select>
          <span class="text-[10px] text-mute">加油过程进度与加油完成订单数据均按此形式推送</span>
        </div>

        <!-- MQTT -->
        <div v-if="form.pushMode === 'mqtt'" class="rounded-lg border border-line bg-ink-850 p-2.5 space-y-2">
          <div class="text-[11px] text-mute">MQTT 服务器（18083 为 EMQX 控制台端口，MQTT 连接默认 1883）</div>
          <div class="grid grid-cols-3 gap-2">
            <label class="block col-span-2">
              <span class="text-[10px] text-mute">服务器地址</span>
              <input
                v-model="form.mqtt.broker"
                placeholder="10.88.0.81"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">端口</span>
              <input
                v-model.number="form.mqtt.port"
                type="number"
                min="1"
                max="65535"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">客户端标识</span>
              <input
                v-model="form.mqtt.clientId"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">用户名</span>
              <input
                v-model="form.mqtt.username"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">口令</span>
              <input
                v-model="form.mqtt.password"
                type="password"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
          </div>
          <label class="block">
            <span class="text-[10px] text-mute">进度推送 topic（{dispenser} / {nozzle} 为占位符）</span>
            <input
              v-model="form.mqtt.topicProgress"
              class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
            />
          </label>
          <div class="grid grid-cols-3 gap-2">
            <label class="block col-span-2">
              <span class="text-[10px] text-mute">订单推送 topic</span>
              <input
                v-model="form.mqtt.topicOrder"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">QoS</span>
              <select
                v-model.number="form.mqtt.qos"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
              >
                <option :value="0">0</option>
                <option :value="1">1</option>
                <option :value="2">2</option>
              </select>
            </label>
          </div>
        </div>

        <!-- HTTP -->
        <div v-else-if="form.pushMode === 'http'" class="rounded-lg border border-line bg-ink-850 p-2.5 space-y-2">
          <div class="text-[11px] text-mute">HTTP 接口（以 JSON 形式 POST 推送）</div>
          <label class="block">
            <span class="text-[10px] text-mute">接口基址</span>
            <input
              v-model="form.http.baseUrl"
              placeholder="http://10.88.0.81:18083"
              class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
            />
          </label>
          <div class="grid grid-cols-2 gap-2">
            <label class="block">
              <span class="text-[10px] text-mute">进度推送路径</span>
            <input
                v-model="form.http.pathProgress"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">订单推送路径</span>
              <input
                v-model="form.http.pathOrder"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
          </div>
          <div class="grid grid-cols-2 gap-2">
            <label class="block">
              <span class="text-[10px] text-mute">鉴权令牌（可选）</span>
              <input
                v-model="form.http.token"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              />
            </label>
            <label class="block">
              <span class="text-[10px] text-mute">超时（秒）</span>
              <input
                v-model.number="form.http.timeoutSec"
                type="number"
                min="1"
                max="60"
                class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 text-[12px]"
              />
            </label>
          </div>
        </div>
      </section>

      <!-- 统一油价 -->
      <section class="space-y-2">
        <div class="text-[12px] font-medium">油价（元 / 升，两位小数）</div>
        <div class="grid grid-cols-3 gap-2">
          <label v-for="f in props.fuels" :key="f.code" class="block rounded-lg border border-line bg-ink-850 px-2 py-1.5">
            <span class="flex items-center gap-1.5 text-[11px]">
              <span class="w-1.5 h-1.5 rounded-full shrink-0" :style="{ background: f.color }"></span>
              <span class="truncate">{{ f.code }}</span>
              <span class="text-[10px] text-mute">{{ f.category }}</span>
            </span>
            <input
              :value="priceOf(f.code)"
              type="number"
              step="0.01"
              min="0"
              class="mt-1 w-full px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono text-[12px]"
              @input="setPrice(f.code, ($event.target as HTMLInputElement).value)"
            />
          </label>
        </div>
      </section>

      <div class="flex justify-end gap-2 pt-1 border-t border-line">
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
