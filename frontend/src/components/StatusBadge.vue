<script setup lang="ts">
import type { Status } from '../api'
import { statusLabel } from '../format'
defineProps<{ status: Status }>()
const cls: Record<Status, string> = {
  open: 'bg-yes-soft text-yes',
  upcoming: 'bg-amber-soft text-amber',
  closed: 'bg-line/70 text-muted',
  draft: 'bg-brand-soft text-brand',
}
</script>

<template>
  <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium" :class="cls[status]">
    <span v-if="status === 'open'" class="h-1.5 w-1.5 rounded-full bg-yes"></span>
    {{ statusLabel[status] }}
  </span>
</template>
