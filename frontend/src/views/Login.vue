<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { mockLogin } from '../auth'
import { randomCitizenId, withCheckDigit } from '../format'

const route = useRoute()
const router = useRouter()

const personas = [
  { name: 'สมชาย ใจดี', id: withCheckDigit('110170020345') },
  { name: 'สมหญิง รักไทย', id: withCheckDigit('350990412876') },
  { name: 'ธนา วงศ์สกุล', id: withCheckDigit('510040098123') },
]

const citizenId = ref(personas[0].id)
const name = ref(personas[0].name)
const busy = ref(false)
const error = ref('')

function pick(p: { name: string; id: string }) {
  citizenId.value = p.id
  name.value = p.name
}
function randomPerson() {
  citizenId.value = randomCitizenId()
  name.value = 'ผู้ใช้ทดลอง ' + citizenId.value.slice(-4)
}

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await mockLogin(citizenId.value.replace(/\D/g, ''), name.value)
    router.replace(typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-md">
    <h1 class="text-2xl font-bold">เข้าสู่ระบบด้วย ThaiID</h1>
    <p class="mt-2 text-muted">ใช้ยืนยันว่าคุณเป็นผู้ใช้สิทธิ์ตัวจริง หนึ่งบัตรประชาชนโหวตได้หนึ่งเสียงต่อแบบโหวต</p>

    <div class="mt-5 rounded-lg border border-amber/30 bg-amber-soft px-4 py-3 text-sm text-amber">
      <b>โหมดทดลอง</b> ยังไม่ได้เชื่อมต่อ ThaiID จริง เลือกหรือกรอกตัวตนใดก็ได้เพื่อเข้าใช้งาน
    </div>

    <form class="panel mt-5 space-y-5 p-5 sm:p-6" @submit.prevent="submit">
      <div>
        <span class="label">เลือกบุคคลตัวอย่าง</span>
        <div class="flex flex-wrap gap-2">
          <button v-for="p in personas" :key="p.id" type="button" class="btn btn-ghost !px-3 !py-1.5 text-sm"
            :class="citizenId === p.id && '!border-brand !bg-brand-soft'" @click="pick(p)">{{ p.name }}</button>
          <button type="button" class="btn btn-ghost !px-3 !py-1.5 text-sm" @click="randomPerson">สุ่มคนใหม่</button>
        </div>
      </div>
      <div>
        <label class="label" for="cid">เลขบัตรประชาชน 13 หลัก</label>
        <input id="cid" v-model="citizenId" class="field tabular-nums" inputmode="numeric" maxlength="17" autocomplete="off" required />
        <p class="hint">ต้องเป็นเลขที่ผ่านการตรวจหลักตรวจสอบ ระบบเก็บเฉพาะค่าเข้ารหัส ไม่เก็บเลขจริง</p>
      </div>
      <div>
        <label class="label" for="nm">ชื่อที่แสดง</label>
        <input id="nm" v-model="name" class="field" maxlength="100" required />
      </div>
      <p v-if="error" class="error-box" role="alert">{{ error }}</p>
      <button class="btn btn-primary w-full" :disabled="busy">{{ busy ? 'กำลังเข้าสู่ระบบ…' : 'เข้าสู่ระบบ' }}</button>
    </form>
  </div>
</template>
