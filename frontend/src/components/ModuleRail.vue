<script setup lang="ts">
import { MODULES, type ModuleKey } from '../modules'

defineProps<{ active: ModuleKey }>()
const emit = defineEmits<{ (e: 'change', key: ModuleKey): void }>()
</script>

<template>
  <nav class="panel flex flex-col w-[64px] shrink-0 py-2 gap-1.5">
    <button
      v-for="m in MODULES"
      :key="m.key"
      class="mx-2 rounded-xl px-1 py-2 flex flex-col items-center gap-1 transition"
      :class="
        m.key === active
          ? 'bg-accent-2/10 text-accent-2 border border-accent-2/40'
          : 'border border-transparent text-mute hover:text-fg hover:bg-ink-850'
      "
      :title="m.desc"
      @click="emit('change', m.key)"
    >
      <span
        class="w-7 h-7 rounded-lg grid place-items-center text-[13px] font-semibold"
        :class="m.key === active ? 'bg-gradient-to-br from-accent to-accent-2 text-white' : 'bg-ink-800 text-fg-soft'"
      >
        {{ m.short }}
      </span>
      <span class="text-[11px] leading-none">{{ m.label }}</span>
    </button>

    <div class="flex-1" />
    <div class="mx-2 text-center text-[10px] text-mute leading-tight">vcpp<br />emulator</div>
  </nav>
</template>
