<script setup lang="ts">
import { computed } from 'vue'
import type { OptionResult } from '../api'

const props = defineProps<{ options: OptionResult[]; total: number; approve?: boolean }>()

const top = computed(() => Math.max(0, ...props.options.map((o) => o.count)))
const pct = (n: number) => (props.total ? Math.round((n / props.total) * 1000) / 10 : 0)

function color(label: string, count: number) {
  if (props.approve) {
    if (label === 'เห็นด้วย') return 'var(--color-yes)'
    if (label === 'ไม่เห็นด้วย') return 'var(--color-stamp)'
    return '#8a93ab'
  }
  return count > 0 && count === top.value ? 'var(--color-brand)' : '#8f9ac9'
}
</script>

<template>
  <ul class="space-y-4">
    <li v-for="o in options" :key="o.id">
      <div class="mb-1.5 flex items-baseline justify-between gap-4">
        <span class="font-medium">{{ o.label }}</span>
        <span class="shrink-0 tabular-nums text-sm text-muted">
          <b class="text-base text-ink">{{ pct(o.count) }}%</b> · {{ o.count.toLocaleString('th-TH') }} เสียง
        </span>
      </div>
      <div class="h-3 overflow-hidden rounded-full bg-brand-soft" role="img" :aria-label="`${o.label} ${pct(o.count)} เปอร์เซ็นต์`">
        <div class="bar-fill h-full rounded-full" :style="{ width: pct(o.count) + '%', background: color(o.label, o.count) }"></div>
      </div>
    </li>
  </ul>
</template>
