<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../api'
import { fmtDate } from '../../format'

interface Entry { id: number; action: string; target: string; detail: Record<string, unknown>; created_at: string; admin: string }
const rows = ref<Entry[]>([])
const error = ref('')
onMounted(async () => {
  try {
    rows.value = await api.get<Entry[]>('/admin/audit-log')
  } catch (e) {
    error.value = (e as Error).message
  }
})
</script>

<template>
  <h1 class="text-2xl font-bold">บันทึกการใช้งานของผู้ดูแล</h1>
  <p class="mt-2 text-muted">แสดง 200 รายการล่าสุด แก้ไขหรือลบไม่ได้จากหน้าเว็บ</p>
  <p v-if="error" class="error-box mt-6">{{ error }}</p>
  <div v-else class="panel mt-6 overflow-x-auto">
    <table class="w-full min-w-[640px] text-left text-sm">
      <thead class="border-b border-line text-muted">
        <tr><th class="px-5 py-3 font-medium">เวลา</th><th class="px-3 py-3 font-medium">ผู้ดูแล</th><th class="px-3 py-3 font-medium">การกระทำ</th><th class="px-5 py-3 font-medium">รายละเอียด</th></tr>
      </thead>
      <tbody class="divide-y divide-line">
        <tr v-for="r in rows" :key="r.id">
          <td class="whitespace-nowrap px-5 py-3 text-muted">{{ fmtDate(r.created_at) }}</td>
          <td class="px-3 py-3">{{ r.admin || '—' }}</td>
          <td class="px-3 py-3 font-mono text-xs">{{ r.action }}</td>
          <td class="break-all px-5 py-3 text-muted">{{ r.target }} <span v-if="Object.keys(r.detail).length">{{ JSON.stringify(r.detail) }}</span></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
