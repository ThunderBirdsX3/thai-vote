<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api, type Results, type Verify } from '../../api'
import { fmtDate, kindLabel } from '../../format'
import StatusBadge from '../../components/StatusBadge.vue'
import ResultBars from '../../components/ResultBars.vue'
import ApproveResults from '../../components/ApproveResults.vue'

const props = defineProps<{ id: string }>()
const router = useRouter()

const res = ref<Results | null>(null)
const verify = ref<Verify | null>(null)
const error = ref('')
const busy = ref(false)
const hideReason = ref<Record<string, string>>({})
const hiding = ref('')

const poll = computed(() => res.value?.poll)
const isChoice = computed(() => poll.value?.kind !== 'comment')

async function load() {
  try {
    res.value = await api.get<Results>(`/admin/polls/${props.id}/results`)
  } catch (e) {
    error.value = (e as Error).message
  }
}
onMounted(load)

async function act(label: string, fn: () => Promise<unknown>, ask?: string) {
  if (ask && !confirm(ask)) return
  busy.value = true
  error.value = ''
  try {
    await fn()
    await load()
  } catch (e) {
    error.value = `${label}ไม่สำเร็จ: ${(e as Error).message}`
  } finally {
    busy.value = false
  }
}

const publish = () => act('เผยแพร่', () => api.post(`/admin/polls/${props.id}/publish`), 'เผยแพร่แบบโหวตนี้?\nหลังเผยแพร่จะไม่สามารถลบได้ และแก้ไขได้เฉพาะก่อนเริ่มโหวต')
const close = () => act('ปิดโหวต', () => api.post(`/admin/polls/${props.id}/close`), 'ปิดโหวตทันที?\nการปิดก่อนกำหนดย้อนกลับไม่ได้')
const runVerify = () => act('ตรวจสอบ', async () => { verify.value = await api.get<Verify>(`/admin/polls/${props.id}/verify`) })
async function remove() {
  if (!confirm('ลบแบบร่างนี้ถาวร?')) return
  try {
    await api.del(`/admin/polls/${props.id}`)
    router.push('/admin')
  } catch (e) {
    error.value = (e as Error).message
  }
}
const hide = (ballotId: string) =>
  act('ซ่อนความเห็น', async () => {
    await api.post(`/admin/ballots/${ballotId}/hide`, { reason: hideReason.value[ballotId] ?? '' })
    hiding.value = ''
  })
</script>

<template>
  <RouterLink to="/admin" class="text-sm text-muted hover:text-ink">‹ แบบโหวตทั้งหมด</RouterLink>
  <p v-if="error" class="error-box mt-4" role="alert">{{ error }}</p>

  <template v-if="poll && res">
    <header class="mt-4 flex flex-wrap items-start justify-between gap-4">
      <div class="min-w-0">
        <div class="flex items-center gap-2"><StatusBadge :status="poll.status" /><span class="text-sm text-muted">{{ kindLabel[poll.kind] }}</span></div>
        <h1 class="mt-2 text-2xl font-bold">{{ poll.title }}</h1>
        <p class="mt-1 text-sm text-muted">
          {{ fmtDate(poll.starts_at) }} – {{ fmtDate(poll.closed_at ?? poll.ends_at) }} ·
          {{ poll.anonymous ? 'ไม่ระบุตัวตน' : 'เปิดเผยชื่อ' }} ·
          {{ poll.results_visibility === 'live' ? 'แสดงผลสด' : 'แสดงผลเมื่อปิด' }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <RouterLink v-if="poll.status === 'draft' || poll.status === 'upcoming'" :to="`/admin/polls/${id}/edit`" class="btn btn-ghost no-underline">แก้ไข</RouterLink>
        <button v-if="poll.status === 'draft'" class="btn btn-primary" :disabled="busy" @click="publish">เผยแพร่</button>
        <button v-if="poll.status === 'draft'" class="btn btn-danger" :disabled="busy" @click="remove">ลบ</button>
        <RouterLink v-if="poll.status !== 'draft'" :to="`/polls/${id}`" class="btn btn-ghost no-underline">ดูหน้าผู้โหวต</RouterLink>
        <button v-if="poll.status === 'open'" class="btn btn-danger" :disabled="busy" @click="close">ปิดโหวตทันที</button>
      </div>
    </header>

    <img v-if="poll.image_url" :src="poll.image_url" alt="รูปโปรโมต" class="mt-4 max-h-64 rounded-xl border border-line object-cover" />
    <div v-if="poll.description" class="rich mt-4 max-w-2xl text-muted" v-html="poll.description"></div>

    <section v-if="poll.status !== 'draft'" class="panel mt-8 p-6">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-lg font-bold">ผลโหวต · {{ res.total.toLocaleString('th-TH') }} เสียง</h2>
        <button class="btn btn-ghost" :disabled="busy" @click="runVerify">ตรวจสอบความถูกต้องของบัตร</button>
      </div>

      <div v-if="verify" class="mt-4 rounded-lg border px-4 py-3 text-sm" :class="verify.ok ? 'border-yes/40 bg-yes-soft text-yes' : 'border-stamp/40 bg-stamp-soft text-stamp'" role="status">
        <p class="font-semibold">{{ verify.ok ? 'ผ่าน: ห่วงโซ่บัตรสมบูรณ์ ไม่พบการแก้ไข' : 'พบความผิดปกติ' }}</p>
        <p class="mt-1 opacity-90">บัตร {{ verify.ballots }} ใบ · ผู้ใช้สิทธิ์ {{ verify.participations }} คน</p>
        <ul v-if="verify.problems.length" class="mt-2 list-disc pl-5"><li v-for="p in verify.problems" :key="p">{{ p }}</li></ul>
        <p class="mt-2 break-all font-mono text-xs opacity-80">{{ verify.head_hash }}</p>
      </div>

      <div class="mt-6">
        <ApproveResults v-if="poll.kind === 'approve'" :poll="{ questions: poll.questions, options: res.options }" />
        <ResultBars v-else-if="isChoice" :options="res.options" :total="res.total" />
        <ul v-if="res.ballots.length" class="mt-6 divide-y divide-line">
          <li v-for="b in res.ballots" :key="b.id" class="py-3 text-[15px]">
            <div v-if="b.name" class="font-medium">{{ b.name }}<span v-if="isChoice" class="font-normal text-muted"> เลือก {{ b.choices.join(', ') }}</span></div>
            <template v-if="b.comment">
              <p class="mt-1 whitespace-pre-line" :class="b.hidden && 'text-muted line-through'">{{ b.comment }}</p>
              <p v-if="b.hidden" class="text-sm text-stamp">ซ่อนแล้ว{{ b.hide_reason ? `: ${b.hide_reason}` : '' }}</p>
              <div v-else-if="b.id" class="mt-2">
                <button v-if="hiding !== b.id" class="text-sm text-stamp underline" @click="hiding = b.id!">ซ่อนความเห็นนี้</button>
                <form v-else class="flex gap-2" @submit.prevent="hide(b.id!)">
                  <input v-model="hideReason[b.id]" class="field" maxlength="500" placeholder="เหตุผล (ไม่บังคับ)" aria-label="เหตุผลที่ซ่อน" />
                  <button class="btn btn-danger" :disabled="busy">ซ่อน</button>
                  <button type="button" class="btn btn-ghost" @click="hiding = ''">ยกเลิก</button>
                </form>
              </div>
            </template>
          </li>
        </ul>
      </div>
    </section>
  </template>
  <p v-else-if="!error" class="py-12 text-center text-muted">กำลังโหลด…</p>
</template>
