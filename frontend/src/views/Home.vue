<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, type Poll, type Status } from '../api'
import { fmtDate, kindLabel, timeLeft } from '../format'
import StatusBadge from '../components/StatusBadge.vue'

const polls = ref<Poll[]>([])
const loading = ref(true)
const error = ref('')
const tab = ref<Status>('open')

onMounted(async () => {
  try {
    polls.value = await api.get<Poll[]>('/polls')
    if (!polls.value.some((p) => p.status === 'open')) {
      tab.value = polls.value.some((p) => p.status === 'upcoming') ? 'upcoming' : 'closed'
    }
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
})

const tabs: { key: Status; label: string }[] = [
  { key: 'open', label: 'เปิดอยู่' },
  { key: 'upcoming', label: 'เร็วๆ นี้' },
  { key: 'closed', label: 'ปิดแล้ว' },
]
const count = (s: Status) => polls.value.filter((p) => p.status === s).length
const shown = computed(() => polls.value.filter((p) => p.status === tab.value))
const stripe: Record<string, string> = {
  open: 'var(--color-yes)',
  upcoming: 'var(--color-amber)',
  closed: '#c3c9d6',
}
</script>

<template>
  <section>
    <h1 class="text-3xl font-bold sm:text-4xl">เสียงของคุณ นับทุกเสียง</h1>
    <p class="mt-3 max-w-xl text-muted">
      ยืนยันตัวตนด้วย ThaiID แล้วร่วมโหวตหรือแสดงความเห็น หนึ่งคนหนึ่งเสียง
      และตรวจสอบได้ภายหลังว่าเสียงของคุณถูกนับ
    </p>
  </section>

  <div class="mt-8 flex gap-1 border-b border-line" role="tablist">
    <button
      v-for="t in tabs"
      :key="t.key"
      role="tab"
      :aria-selected="tab === t.key"
      class="-mb-px cursor-pointer border-b-2 px-4 py-2.5 text-[15px] transition-colors"
      :class="tab === t.key ? 'border-brand font-semibold text-ink' : 'border-transparent text-muted hover:text-ink'"
      @click="tab = t.key"
    >
      {{ t.label }}
      <span class="ml-1 rounded-full bg-brand-soft px-2 py-0.5 text-xs text-brand">{{ count(t.key) }}</span>
    </button>
  </div>

  <p v-if="loading" class="py-12 text-center text-muted">กำลังโหลด…</p>
  <p v-else-if="error" class="error-box mt-6">{{ error }}</p>
  <div v-else-if="!shown.length" class="panel mt-6 px-6 py-14 text-center">
    <p class="font-medium">ยังไม่มีรายการในหมวดนี้</p>
    <p class="mt-1 text-sm text-muted">เมื่อผู้ดูแลระบบเปิดโหวตใหม่ รายการจะปรากฏที่นี่</p>
  </div>

  <ul v-else class="panel mt-6 divide-y divide-line overflow-hidden">
    <li v-for="p in shown" :key="p.id">
      <RouterLink
        :to="`/polls/${p.id}`"
        class="flex gap-4 border-l-4 px-5 py-5 no-underline transition-colors hover:bg-paper sm:px-6"
        :style="{ borderLeftColor: stripe[p.status] }"
      >
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <StatusBadge :status="p.status" />
            <span v-if="p.has_voted" class="rounded-full bg-brand-soft px-2.5 py-0.5 text-xs font-medium text-brand">คุณโหวตแล้ว</span>
          </div>
          <h2 class="mt-2 text-lg font-bold text-ink">{{ p.title }}</h2>
          <p class="mt-1 line-clamp-2 text-[15px] text-muted">{{ p.description }}</p>
          <p class="mt-3 text-sm text-muted">
            {{ kindLabel[p.kind] }} · {{ p.anonymous ? 'ไม่เปิดเผยชื่อผู้โหวต' : 'เปิดเผยชื่อผู้โหวต' }}
          </p>
        </div>
        <div class="hidden shrink-0 text-right text-sm text-muted sm:block">
          <template v-if="p.status === 'open'">
            <div class="font-medium text-ink">{{ timeLeft(p.ends_at) }}</div>
            <div>ปิด {{ fmtDate(p.ends_at) }}</div>
          </template>
          <template v-else-if="p.status === 'upcoming'">
            <div class="font-medium text-ink">เริ่ม</div>
            <div>{{ fmtDate(p.starts_at) }}</div>
          </template>
          <template v-else>
            <div class="font-medium text-ink">{{ p.ballot_count.toLocaleString('th-TH') }} เสียง</div>
            <div>ปิด {{ fmtDate(p.closed_at ?? p.ends_at) }}</div>
          </template>
        </div>
      </RouterLink>
    </li>
  </ul>
</template>
