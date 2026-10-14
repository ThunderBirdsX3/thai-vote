<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ page: number; perPage: number; total: number }>()
const emit = defineEmits<{ 'update:page': [page: number] }>()
const pages = computed(() => Math.max(1, Math.ceil(props.total / props.perPage)))
</script>

<template>
  <nav v-if="pages > 1" class="mt-6 flex items-center justify-center gap-4" aria-label="เปลี่ยนหน้า">
    <button class="btn btn-ghost" :disabled="page <= 1" @click="emit('update:page', page - 1)">ก่อนหน้า</button>
    <span class="text-sm text-muted tabular-nums">หน้า {{ page }} / {{ pages }}</span>
    <button class="btn btn-ghost" :disabled="page >= pages" @click="emit('update:page', page + 1)">ถัดไป</button>
  </nav>
</template>
