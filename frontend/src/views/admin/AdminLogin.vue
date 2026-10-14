<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { adminLogin } from '../../auth'

const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await adminLogin(username.value, password.value)
    router.replace(typeof route.query.next === 'string' && route.query.next.startsWith('/admin') ? route.query.next : '/admin')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-sm">
    <h1 class="text-2xl font-bold">เข้าสู่ระบบผู้ดูแล</h1>
    <p class="mt-2 text-muted">สำหรับผู้ที่ได้รับสิทธิ์สร้างและจัดการแบบโหวตเท่านั้น</p>
    <form class="panel mt-6 space-y-4 p-5 sm:p-6" @submit.prevent="submit">
      <div>
        <label class="label" for="u">ชื่อผู้ใช้</label>
        <input id="u" v-model="username" class="field" autocomplete="username" required />
      </div>
      <div>
        <label class="label" for="p">รหัสผ่าน</label>
        <input id="p" v-model="password" type="password" class="field" autocomplete="current-password" required />
      </div>
      <p v-if="error" class="error-box" role="alert">{{ error }}</p>
      <button class="btn btn-primary w-full" :disabled="busy">{{ busy ? 'กำลังเข้าสู่ระบบ…' : 'เข้าสู่ระบบ' }}</button>
    </form>
  </div>
</template>
