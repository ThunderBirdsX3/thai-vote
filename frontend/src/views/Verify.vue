<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, ApiError } from '../api'

const props = defineProps<{ code?: string }>()

const code = ref(props.code ?? '')
const busy = ref(false)
const error = ref('')
const found = ref<{ poll_id: string; poll_title: string; seq: number; hash: string } | null>(null)

async function check() {
  const c = code.value.trim()
  if (!c) return
  busy.value = true
  error.value = ''
  found.value = null
  try {
    found.value = await api.get(`/receipts/${encodeURIComponent(c)}`)
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 404 ? 'ไม่พบรหัสนี้ในระบบ ตรวจสอบตัวอักษรอีกครั้ง' : (e as Error).message
  } finally {
    busy.value = false
  }
}
onMounted(() => props.code && check())
</script>

<template>
  <div class="mx-auto max-w-xl">
    <h1 class="text-2xl font-bold">ตรวจสอบใบรับรองการโหวต</h1>
    <p class="mt-2 text-muted">กรอกรหัสที่ได้รับหลังโหวต เพื่อยืนยันว่าเสียงของคุณถูกบันทึกในระบบ ผลลัพธ์จะไม่บอกว่าคุณเลือกอะไร</p>

    <form class="panel mt-6 p-5 sm:p-6" @submit.prevent="check">
      <label class="label" for="code">รหัสใบรับรอง</label>
      <input id="code" v-model="code" class="field font-mono uppercase tracking-wider" placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXX-X" autocomplete="off" spellcheck="false" />
      <button class="btn btn-primary mt-4" :disabled="busy || !code.trim()">{{ busy ? 'กำลังตรวจสอบ…' : 'ตรวจสอบ' }}</button>
    </form>

    <p v-if="error" class="error-box mt-5" role="alert">{{ error }}</p>
    <section v-if="found" class="panel mt-5 border-yes/40 p-6" aria-live="polite">
      <div class="flex items-center gap-2 font-semibold text-yes">
        <svg viewBox="0 0 24 24" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.5l4.5 4.5L19 7" /></svg>
        พบเสียงของคุณในระบบ
      </div>
      <p class="mt-3">แบบโหวต: <RouterLink :to="`/polls/${found.poll_id}`">{{ found.poll_title }}</RouterLink></p>
      <p class="text-sm text-muted">ลำดับบัตรที่ {{ found.seq.toLocaleString('th-TH') }} · ลายนิ้วมือ</p>
      <p class="break-all font-mono text-xs text-muted">{{ found.hash }}</p>
    </section>
  </div>
</template>
