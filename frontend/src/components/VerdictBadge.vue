<script setup lang="ts">
import type { Verdict } from '../format'

defineProps<{ verdict: Verdict }>()

const style: Record<string, string> = {
  เห็นด้วย: 'bg-yes-soft text-yes',
  ไม่เห็นด้วย: 'bg-stamp-soft text-stamp',
  เสมอ: 'bg-brand-soft text-muted',
}
</script>

<template>
  <span v-if="verdict" class="inline-block rounded-full px-2.5 py-0.5 text-xs font-semibold" :class="style[verdict]">
    {{ verdict === 'เสมอ' ? 'เสียงเสมอกัน' : `ข้างมาก${verdict}` }}
  </span>
  <span v-else class="inline-block rounded-full bg-brand-soft px-2.5 py-0.5 text-xs font-medium text-muted">ยังไม่มีผู้ออกเสียง</span>
</template>
