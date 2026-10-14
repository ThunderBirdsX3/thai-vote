<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, type Poll } from '../../api'
import { fmtDate, kindLabel } from '../../format'
import StatusBadge from '../../components/StatusBadge.vue'

const polls = ref<Poll[]>([])
const loading = ref(true)
const error = ref('')

onMounted(async () => {
  try {
    polls.value = await api.get<Poll[]>('/admin/polls')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="flex flex-wrap items-center justify-between gap-3">
    <h1 class="text-2xl font-bold">แบบโหวตทั้งหมด</h1>
    <RouterLink to="/admin/polls/new" class="btn btn-primary no-underline">สร้างแบบโหวตใหม่</RouterLink>
  </div>

  <p v-if="loading" class="py-12 text-center text-muted">กำลังโหลด…</p>
  <p v-else-if="error" class="error-box mt-6">{{ error }}</p>
  <div v-else-if="!polls.length" class="panel mt-6 px-6 py-14 text-center">
    <p class="font-medium">ยังไม่มีแบบโหวต</p>
    <p class="mt-1 text-sm text-muted">สร้างแบบโหวตแรก กำหนดวันเริ่มและวันสิ้นสุด แล้วเผยแพร่</p>
  </div>
  <div v-else class="panel mt-6 overflow-x-auto">
    <table class="w-full min-w-[640px] text-left text-[15px]">
      <thead class="border-b border-line text-sm text-muted">
        <tr>
          <th class="px-5 py-3 font-medium">ชื่อ</th>
          <th class="px-3 py-3 font-medium">สถานะ</th>
          <th class="px-3 py-3 font-medium">ช่วงเวลา</th>
          <th class="px-5 py-3 text-right font-medium">เสียง</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-line">
        <tr v-for="p in polls" :key="p.id" class="hover:bg-paper">
          <td class="px-5 py-3.5">
            <RouterLink :to="`/admin/polls/${p.id}`" class="font-medium text-ink no-underline hover:underline">{{ p.title }}</RouterLink>
            <div class="text-sm text-muted">{{ kindLabel[p.kind] }} · {{ p.anonymous ? 'ไม่ระบุตัวตน' : 'เปิดเผยชื่อ' }}</div>
          </td>
          <td class="px-3 py-3.5"><StatusBadge :status="p.status" /></td>
          <td class="px-3 py-3.5 text-sm text-muted">{{ fmtDate(p.starts_at) }}<br />ถึง {{ fmtDate(p.closed_at ?? p.ends_at) }}</td>
          <td class="px-5 py-3.5 text-right tabular-nums">{{ p.ballot_count.toLocaleString('th-TH') }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
