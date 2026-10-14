<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { api, ApiError, type Poll, type Results } from '../api'
import { loadVoter, session } from '../auth'
import { fmtDate, kindLabel } from '../format'
import StatusBadge from '../components/StatusBadge.vue'
import ResultBars from '../components/ResultBars.vue'
import ApproveResults from '../components/ApproveResults.vue'

const props = defineProps<{ id: string }>()
const route = useRoute()

const poll = ref<Poll | null>(null)
const results = ref<Results | null>(null)
const resultsNote = ref('')
const loading = ref(true)
const error = ref('')

const selected = ref<string[]>([])
const answers = ref<Record<string, string>>({}) // approve: question id -> option id
const comment = ref('')
const reviewing = ref(false)
const busy = ref(false)
const voteError = ref('')
const receipt = ref('')
const copied = ref(false)

const isChoice = computed(() => poll.value?.kind !== 'comment')
const isMulti = computed(() => poll.value?.kind === 'multi')
const isApprove = computed(() => poll.value?.kind === 'approve')
const showComment = computed(() => !isChoice.value || !!poll.value?.allow_comment)
const canVote = computed(() => poll.value?.status === 'open' && !poll.value.has_voted && !receipt.value)
const valid = computed(() => {
  if (!poll.value) return false
  if (!isChoice.value) return comment.value.trim().length > 0
  if (isApprove.value) return poll.value.questions.every((q) => answers.value[q.id])
  return selected.value.length >= 1 && selected.value.length <= poll.value.max_choices
})
const optionsOf = (qid: string) => poll.value?.options.filter((o) => o.question_id === qid) ?? []
const chosenLabels = computed(() => {
  if (!poll.value) return []
  if (isApprove.value) {
    return poll.value.questions.map((q) => {
      const o = poll.value!.options.find((x) => x.id === answers.value[q.id])
      return q.label ? `${q.label}: ${o?.label}` : (o?.label ?? '')
    })
  }
  return poll.value.options.filter((o) => selected.value.includes(o.id)).map((o) => o.label)
})

async function loadResults() {
  results.value = null
  resultsNote.value = ''
  try {
    results.value = await api.get<Results>(`/polls/${props.id}/results`)
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) resultsNote.value = 'ผลโหวตจะแสดงเมื่อปิดโหวตแล้ว'
    else if (!(e instanceof ApiError)) throw e
  }
}

async function load() {
  try {
    await loadVoter()
    poll.value = await api.get<Poll>(`/polls/${props.id}`)
    await loadResults()
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 404 ? 'ไม่พบแบบโหวตนี้' : (e as Error).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

function toggle(id: string) {
  if (!poll.value) return
  if (!isMulti.value) {
    selected.value = [id]
  } else if (selected.value.includes(id)) {
    selected.value = selected.value.filter((x) => x !== id)
  } else if (selected.value.length < poll.value.max_choices) {
    selected.value = [...selected.value, id]
  }
}

async function submit() {
  busy.value = true
  voteError.value = ''
  try {
    const r = await api.post<{ receipt: string }>(`/polls/${props.id}/vote`, {
      option_ids: !isChoice.value ? [] : isApprove.value ? poll.value!.questions.map((q) => answers.value[q.id]) : selected.value,
      comment: showComment.value ? comment.value.trim() : '',
    })
    receipt.value = r.receipt
    reviewing.value = false
    poll.value = await api.get<Poll>(`/polls/${props.id}`)
    await loadResults()
  } catch (e) {
    voteError.value = (e as Error).message
    reviewing.value = false
    if (e instanceof ApiError && e.status === 409) poll.value = await api.get<Poll>(`/polls/${props.id}`)
  } finally {
    busy.value = false
  }
}

async function copyReceipt() {
  await navigator.clipboard.writeText(receipt.value).catch(() => {})
  copied.value = true
  setTimeout(() => (copied.value = false), 2000)
}
</script>

<template>
  <p v-if="loading" class="py-16 text-center text-muted">กำลังโหลด…</p>
  <div v-else-if="error" class="py-16 text-center">
    <p class="error-box inline-block">{{ error }}</p>
    <div class="mt-6"><RouterLink to="/" class="btn btn-ghost no-underline">กลับหน้าแรก</RouterLink></div>
  </div>

  <article v-else-if="poll" class="mx-auto max-w-2xl">
    <RouterLink to="/" class="text-sm text-muted hover:text-ink">‹ รายการโหวต</RouterLink>

    <header class="mt-4">
      <div class="flex flex-wrap items-center gap-2">
        <StatusBadge :status="poll.status" />
        <span class="text-sm text-muted">{{ kindLabel[poll.kind] }}<template v-if="isMulti"> (สูงสุด {{ poll.max_choices }})</template><template v-if="isApprove && poll.questions.length > 1"> · {{ poll.questions.length }} ประเด็น</template></span>
      </div>
      <img v-if="poll.image_url" :src="poll.image_url" alt="" class="mt-4 max-h-80 w-full rounded-xl border border-line object-cover" />
      <h1 class="mt-3 text-3xl font-bold">{{ poll.title }}</h1>
      <div v-if="poll.description" class="rich mt-3 text-muted" v-html="poll.description"></div>
      <dl class="mt-5 grid gap-x-8 gap-y-1 text-sm sm:grid-cols-2">
        <div class="flex gap-2"><dt class="text-muted">เริ่ม</dt><dd>{{ fmtDate(poll.starts_at) }}</dd></div>
        <div class="flex gap-2"><dt class="text-muted">สิ้นสุด</dt><dd>{{ fmtDate(poll.closed_at ?? poll.ends_at) }}</dd></div>
        <div class="flex gap-2 sm:col-span-2">
          <dt class="text-muted">ความเป็นส่วนตัว</dt>
          <dd>{{ poll.anonymous ? 'ไม่เปิดเผยว่าใครเลือกอะไร ระบบเก็บเพียงว่าคุณโหวตแล้ว' : 'เปิดเผยชื่อผู้โหวตและตัวเลือกที่เลือก' }}</dd>
        </div>
      </dl>
    </header>

    <!-- receipt stub (shown once, right after voting) -->
    <section v-if="receipt" class="stub mt-8" aria-live="polite">
      <div class="p-6">
        <div class="flex items-center gap-2 font-semibold text-yes">
          <svg viewBox="0 0 24 24" class="h-5 w-5" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.5l4.5 4.5L19 7" /></svg>
          บันทึกเสียงของคุณแล้ว
        </div>
        <p class="mt-2 text-sm text-muted">เก็บรหัสนี้ไว้ใช้ตรวจสอบภายหลังว่าเสียงของคุณอยู่ในระบบ รหัสจะแสดงเพียงครั้งเดียว และไม่เปิดเผยว่าคุณเลือกอะไร</p>
      </div>
      <div class="stub-cut"></div>
      <div class="flex flex-wrap items-center justify-between gap-3 p-6">
        <code class="break-all text-lg font-semibold tracking-wider text-ink">{{ receipt }}</code>
        <button class="btn btn-ghost" @click="copyReceipt">{{ copied ? 'คัดลอกแล้ว' : 'คัดลอกรหัส' }}</button>
      </div>
    </section>

    <section v-else-if="poll.has_voted" class="panel mt-8 flex flex-wrap items-center justify-between gap-3 px-5 py-4">
      <p class="font-medium">คุณโหวตในแบบโหวตนี้แล้ว</p>
      <RouterLink to="/verify" class="text-sm">ตรวจสอบด้วยรหัสใบรับรอง</RouterLink>
    </section>

    <!-- voting form -->
    <section v-if="canVote" class="mt-8">
      <div v-if="!session.voter" class="panel px-6 py-8 text-center">
        <p class="font-medium">เข้าสู่ระบบก่อนจึงจะโหวตได้</p>
        <p class="mt-1 text-sm text-muted">ใช้ ThaiID ยืนยันตัวตนเพื่อป้องกันการโหวตซ้ำ</p>
        <RouterLink :to="{ path: '/login', query: { next: route.fullPath } }" class="btn btn-primary mt-5 no-underline">เข้าสู่ระบบด้วย ThaiID</RouterLink>
      </div>

      <template v-else-if="!reviewing">
        <h2 class="mb-3 text-lg font-bold">{{ !isChoice ? 'ความเห็นของคุณ' : isApprove ? (poll.questions.length > 1 ? 'ตอบทุกประเด็น' : 'ความเห็นของคุณต่อเรื่องนี้') : isMulti ? `เลือกได้ไม่เกิน ${poll.max_choices} ข้อ` : 'เลือก 1 ข้อ' }}</h2>
        <div v-if="isApprove" class="space-y-6">
          <fieldset v-for="(q, i) in poll.questions" :key="q.id">
            <legend v-if="q.label" class="mb-2 font-semibold">{{ i + 1 }}. {{ q.label }}</legend>
            <div class="grid gap-2 sm:grid-cols-3" role="radiogroup" :aria-label="q.label || poll.title">
              <button v-for="o in optionsOf(q.id)" :key="o.id" type="button" class="ballot-option" role="radio" :aria-checked="answers[q.id] === o.id" @click="answers[q.id] = o.id">
                <span class="ballot-box round"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7" /></svg></span>
                <span class="text-[16px]">{{ o.label }}</span>
              </button>
            </div>
          </fieldset>
        </div>
        <div v-else-if="isChoice" class="space-y-2.5" :role="isMulti ? 'group' : 'radiogroup'">
          <button v-for="o in poll.options" :key="o.id" type="button" class="ballot-option"
            :role="isMulti ? 'checkbox' : 'radio'" :aria-checked="selected.includes(o.id)"
            :disabled="isMulti && !selected.includes(o.id) && selected.length >= poll.max_choices"
            @click="toggle(o.id)">
            <span class="ballot-box" :class="isMulti ? 'square' : 'round'">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7" /></svg>
            </span>
            <span class="text-[16px]">{{ o.label }}</span>
          </button>
        </div>
        <div v-if="showComment" :class="isChoice && 'mt-6'">
          <label v-if="isChoice" class="label" for="cm">ความเห็นเพิ่มเติม (ไม่บังคับ)</label>
          <textarea id="cm" v-model="comment" class="field min-h-36" maxlength="2000" :placeholder="isChoice ? 'พิมพ์ความเห็นเพิ่มเติม ถ้ามี' : 'พิมพ์ความเห็นของคุณ'" :aria-label="isChoice ? 'ความเห็นเพิ่มเติม' : 'ความเห็นของคุณ'"></textarea>
          <p class="hint text-right tabular-nums">{{ comment.length }} / 2000</p>
        </div>
        <p v-if="voteError" class="error-box mt-4" role="alert">{{ voteError }}</p>
        <button class="btn btn-primary mt-5 w-full sm:w-auto sm:px-8" :disabled="!valid" @click="reviewing = true; voteError = ''">ตรวจสอบก่อนส่ง</button>
      </template>

      <div v-else class="panel p-6">
        <h2 class="text-lg font-bold">ยืนยันการส่งคะแนน</h2>
        <p class="mt-2 text-sm text-muted">เมื่อส่งแล้วจะแก้ไขหรือเพิกถอนไม่ได้</p>
        <ul v-if="isChoice" class="mt-4 space-y-1.5">
          <li v-for="l in chosenLabels" :key="l" class="flex items-center gap-2 font-medium">
            <svg viewBox="0 0 24 24" class="h-5 w-5 text-brand" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.5l4.5 4.5L19 7" /></svg>{{ l }}
          </li>
        </ul>
        <p v-if="showComment && comment.trim()" class="mt-4 whitespace-pre-line rounded-lg bg-paper px-4 py-3">{{ comment }}</p>
        <div class="mt-6 flex flex-wrap gap-3">
          <button class="btn btn-primary" :disabled="busy" @click="submit">{{ busy ? 'กำลังส่ง…' : 'ยืนยันส่งคะแนน' }}</button>
          <button class="btn btn-ghost" :disabled="busy" @click="reviewing = false">แก้ไข</button>
        </div>
      </div>
    </section>

    <p v-else-if="poll.status === 'upcoming'" class="panel mt-8 px-5 py-4 text-muted">เปิดโหวตวันที่ {{ fmtDate(poll.starts_at) }}</p>
    <p v-else-if="poll.status === 'closed' && !poll.has_voted && !receipt" class="panel mt-8 px-5 py-4 text-muted">แบบโหวตนี้ปิดแล้ว</p>

    <!-- results -->
    <section class="mt-10">
      <h2 class="text-xl font-bold">ผลโหวต</h2>
      <p v-if="resultsNote" class="panel mt-3 px-5 py-4 text-muted">{{ resultsNote }}</p>
      <p v-else-if="poll.status === 'upcoming'" class="panel mt-3 px-5 py-4 text-muted">ยังไม่เริ่มโหวต</p>
      <div v-else-if="results" class="panel mt-3 p-6">
        <p class="mb-5 text-sm text-muted">
          ผู้ใช้สิทธิ์ทั้งหมด <b class="text-ink">{{ results.total.toLocaleString('th-TH') }}</b> คน
          <template v-if="poll.status === 'open'"> · กำลังนับสด</template>
        </p>
        <ApproveResults v-if="isApprove" :poll="{ questions: poll.questions, options: results.options }" />
        <ResultBars v-else-if="isChoice" :options="results.options" :total="results.total" />

        <ul v-if="results.ballots.length" class="mt-6 divide-y divide-line">
          <li v-for="(b, i) in results.ballots" :key="i" class="py-3 text-[15px]">
            <div v-if="b.name" class="font-medium">{{ b.name }}
              <span v-if="isChoice" class="font-normal text-muted"> เลือก {{ b.choices.join(', ') }}</span>
            </div>
            <p v-if="b.comment" class="mt-1 whitespace-pre-line">{{ b.comment }}</p>
            <p v-else-if="b.hidden" class="mt-1 text-sm italic text-muted">ความเห็นถูกซ่อนโดยผู้ดูแลระบบ</p>
          </li>
        </ul>
        <p v-else-if="!isChoice" class="text-muted">ยังไม่มีความเห็น</p>

        <details v-if="results.head_hash" class="mt-6 text-sm text-muted">
          <summary class="cursor-pointer">ลายนิ้วมือตรวจสอบผล</summary>
          <p class="mt-2 break-all font-mono text-xs">{{ results.head_hash }}</p>
          <p class="mt-1">ค่านี้เปลี่ยนทุกครั้งที่มีการเพิ่มหรือแก้ไขคะแนน ใช้เทียบกับผู้ดูแลระบบหรือบุคคลที่สามได้</p>
        </details>
      </div>
    </section>
  </article>
</template>
