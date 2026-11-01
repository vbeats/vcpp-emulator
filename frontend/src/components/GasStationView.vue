<script setup lang="ts">
// 加油站模块（预留）：尚未接入后端协议，当前提供界面骨架与静态示例，
// 后续按 internal/ykc 的方式独立实现加油机协议层，与充电桩模块并行互不干扰。

const PLANS = [
  { title: '加油机模拟', desc: '单进程模拟多台加油机，每机多把油枪，各自维护状态机' },
  { title: '油品与单价', desc: '92# / 95# / 98# / 0# 柴油油价下发与变更确认' },
  { title: '加油流程', desc: '提枪申请 → 授权启机 → 油量金额实时累计 → 挂枪结算' },
  { title: '班结 / 日结', desc: '班结数据、日结流水与加油记录补传' },
  { title: '异常模拟', desc: '脱枪、抬枪停机、掉线与超时重连' },
  { title: '独立协议层', desc: '新增 internal/gas 协议包，不改动机充模块既有链路' },
]
</script>

<template>
  <section class="panel flex flex-col h-full min-h-0">
    <header class="flex items-center gap-2 px-3.5 py-2.5 border-b border-line">
      <span class="text-[13px] font-semibold">加油站模拟器</span>
      <span class="px-2 py-0.5 rounded-full border border-accent-2/40 bg-accent-2/10 text-accent-2 text-[10px]">
        预留模块 · 开发中
      </span>
      <div class="flex-1" />
      <span class="text-[11px] text-mute">与充电桩模块独立并行</span>
    </header>

    <div class="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
      <div class="rounded-xl bg-ink-850 border border-line px-3 py-2.5">
        <div class="text-[12px] font-semibold">模块说明</div>
        <p class="mt-1 text-[11px] text-fg-soft leading-relaxed">
          加油站模块用于模拟加油机（Dispenser）与油枪加油全流程，界面与充电模块通过左侧 tab 切换，
          两侧数据与状态互不影响。当前版本仅占位建立界面骨架，协议适配完成后在此处展示实时加油数据。
        </p>
      </div>

      <div>
        <div class="mb-1.5 text-[12px] font-semibold">规划能力</div>
        <div class="grid grid-cols-2 gap-2">
          <div v-for="p in PLANS" :key="p.title" class="rounded-xl border border-line bg-ink-850 px-3 py-2">
            <div class="text-[12px] font-medium">{{ p.title }}</div>
            <div class="mt-0.5 text-[11px] text-mute leading-relaxed">{{ p.desc }}</div>
          </div>
        </div>
      </div>

      <div>
        <div class="mb-1.5 flex items-center gap-2">
          <span class="text-[12px] font-semibold">界面预览（静态示例）</span>
          <span class="text-[10px] text-mute">数据未接入，不可操作</span>
        </div>
        <div class="grid grid-cols-2 gap-2">
          <div class="rounded-xl border border-line bg-ink-850 p-3 space-y-2 opacity-70">
            <div class="flex items-center gap-2">
              <span class="text-[12px] font-semibold font-mono">01000001</span>
              <span class="px-2 py-0.5 rounded-full border border-line text-[10px] text-mute">未启用</span>
            </div>
            <div class="grid grid-cols-2 gap-1.5 text-[11px]">
              <div class="rounded-lg bg-white border border-line/60 px-2 py-1.5">
                <div class="text-[10px] text-mute">油品</div>
                <div>92# · 7.68 元/L</div>
              </div>
              <div class="rounded-lg bg-white border border-line/60 px-2 py-1.5">
                <div class="text-[10px] text-mute">已加油量</div>
                <div class="font-mono">0.00 L</div>
              </div>
              <div class="rounded-lg bg-white border border-line/60 px-2 py-1.5">
                <div class="text-[10px] text-mute">金额</div>
                <div class="font-mono">0.00 元</div>
              </div>
              <div class="rounded-lg bg-white border border-line/60 px-2 py-1.5">
                <div class="text-[10px] text-mute">枪数</div>
                <div class="font-mono">2</div>
              </div>
            </div>
            <div class="flex items-center gap-1.5">
              <button class="px-2 py-1 rounded-md border border-line text-[11px] text-mute cursor-not-allowed" disabled>挂枪</button>
              <button class="px-2 py-1 rounded-md border border-accent/40 text-[11px] text-accent cursor-not-allowed" disabled>
                开始加油
              </button>
            </div>
          </div>

          <div class="rounded-xl border border-line border-dashed grid place-items-center p-3">
            <div class="text-center space-y-1">
              <div class="text-[12px] text-fg-soft">油枪实时数据</div>
              <div class="text-[11px] text-mute leading-relaxed">
                等待协议适配完成后填充<br />
                油量 / 金额 / 单价 / 状态
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
