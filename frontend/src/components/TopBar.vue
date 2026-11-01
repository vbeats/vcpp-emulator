<script setup lang="ts">
import { ref, watch } from 'vue'
import type { AppConfig, VersionOption } from '../types'
import type { ModuleKey } from '../modules'

const props = defineProps<{
  config: AppConfig | null
  versions: VersionOption[]
  online: number
  total: number
  module: ModuleKey
  pushTarget?: string
}>()
const emit = defineEmits<{
  (e: 'save', cfg: AppConfig): void
  (e: 'startAll'): void
  (e: 'stopAll'): void
  (e: 'openGasConfig'): void
}>()

const host = ref('')
const port = ref(9847)
const version = ref('V15')

watch(
  () => props.config,
  (cfg) => {
    if (!cfg) return
    host.value = cfg.server.host
    port.value = cfg.server.port
    version.value = cfg.server.version
  },
  { immediate: true, deep: true },
)

// 切换协议版本时自动带出该版本对应的默认端口
const onVersionChange = () => {
  const v = props.versions.find((it) => it.code === version.value)
  if (v) port.value = v.port
}

const dirty = () => {
  const c = props.config
  if (!c) return false
  return host.value !== c.server.host || port.value !== c.server.port || version.value !== c.server.version
}

const save = () => {
  if (!props.config) return
  emit('save', { ...props.config, server: { host: host.value, port: Number(port.value), version: version.value } })
}
</script>

<template>
  <header class="panel flex items-center gap-3 px-3.5 py-2.5 mb-3 shrink-0">
    <div class="flex items-center gap-2">
      <div class="w-7 h-7 rounded-lg bg-gradient-to-br from-accent to-accent-2 grid place-items-center text-white font-bold text-[13px]">
        {{ props.module === 'charge' ? '充' : '油' }}
      </div>
      <div class="leading-tight">
        <div class="text-[13px] font-semibold tracking-wide">
          设备模拟器
        </div>
        <div class="text-[10px] text-mute">
          {{ props.module === 'charge' ? '充电桩 · 云快充 V1.5 ~ V2.1' : '加油机 · 加油流程与数据推送' }}
        </div>
      </div>
    </div>

    <div class="w-px h-7 bg-line mx-1" />

    <!-- 充电桩：服务端连接 -->
    <div v-if="props.module === 'charge'" class="flex items-center gap-2 text-[11px]">
      <label class="text-mute">服务端</label>
      <input
        v-model="host"
        class="w-32 px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono"
        placeholder="IP / 域名"
      />
      <span class="text-mute">:</span>
      <input
        v-model.number="port"
        type="number"
        class="w-20 px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60 font-mono"
      />
      <select
        v-model="version"
        class="px-2 py-1 rounded-md bg-white border border-line outline-none focus:border-accent-2/60"
        @change="onVersionChange"
      >
        <option v-for="v in versions" :key="v.code" :value="v.code">{{ v.desc }} · {{ v.port }}</option>
      </select>
      <button
        class="px-2.5 py-1 rounded-md border text-[11px] transition"
        :class="dirty() ? 'border-accent/50 text-accent hover:bg-accent/10' : 'border-line text-mute'"
        @click="save"
      >
        保存
      </button>
    </div>

    <!-- 加油站：数据推送形式 -->
    <div v-else class="flex items-center gap-2 text-[11px]">
      <label class="text-mute">数据推送</label>
      <span class="px-2 py-1 rounded-md bg-ink-850 border border-line font-mono max-w-[420px] truncate">
        {{ props.pushTarget || '未启用数据推送' }}
      </span>
      <button
        class="px-2.5 py-1 rounded-md border border-line text-[11px] text-mute hover:text-fg transition"
        @click="emit('openGasConfig')"
      >
        配置
      </button>
    </div>

    <div class="flex-1" />

    <template v-if="props.module === 'charge'">
      <div class="flex items-center gap-1.5 text-[11px] text-mute">
        <span class="w-1.5 h-1.5 rounded-full" :class="online ? 'bg-accent' : 'bg-mute'" />
        在线 {{ online }} / {{ total }}
      </div>
      <button
        class="px-2.5 py-1 rounded-md border border-accent/40 text-[11px] text-accent hover:bg-accent/10 transition"
        title="手动连接全部充电桩"
        @click="emit('startAll')"
      >
        全部连接
      </button>
      <button
        class="px-2.5 py-1 rounded-md border border-line text-[11px] text-mute hover:text-fg transition"
        title="断开全部充电桩连接"
        @click="emit('stopAll')"
      >
        全部断开
      </button>
    </template>
    <template v-else>
      <div class="flex items-center gap-1.5 text-[11px] text-mute">
        <span class="w-1.5 h-1.5 rounded-full" :class="online ? 'bg-accent animate-pulse' : 'bg-mute'" />
        加油机 {{ total }} 台 · 加油中 {{ online }} 枪
      </div>
    </template>
  </header>
</template>
