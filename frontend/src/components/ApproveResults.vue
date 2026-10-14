<script setup lang="ts">
import { computed } from 'vue'
import type { Poll } from '../api'
import { approveSummary, verdict, type Tally } from '../format'
import ResultBars from './ResultBars.vue'
import VerdictBadge from './VerdictBadge.vue'

// compact: one stacked bar for the whole poll (list cards). Otherwise overall + each question.
const props = defineProps<{ poll: Pick<Poll, 'questions' | 'options'>; compact?: boolean }>()

const sum = computed(() => approveSummary(props.poll))
const multi = computed(() => sum.value.rows.length > 1)
const showOverall = computed(() => props.compact || multi.value)
const total = (t: Tally) => t.yes + t.no + t.abstain
const pct = (n: number, t: Tally) => (total(t) ? (n / total(t)) * 100 : 0)
const segments = (t: Tally) => [
  { key: 'yes', label: 'เห็นด้วย', n: t.yes, bg: 'var(--color-yes)' },
  { key: 'no', label: 'ไม่เห็นด้วย', n: t.no, bg: 'var(--color-stamp)' },
  { key: 'abs', label: 'งดออกเสียง', n: t.abstain, bg: '#8a93ab' },
]
</script>

<template>
  <div>
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
      <VerdictBadge :verdict="sum.verdict" />
      <span v-if="showOverall && total(sum.overall)" class="text-sm text-muted">
        <template v-for="s in segments(sum.overall)" :key="s.key">
          <span class="mr-3 whitespace-nowrap"><i class="mr-1 inline-block h-2 w-2 rounded-full align-middle" :style="{ background: s.bg }"></i>{{ s.label }} {{ s.n.toLocaleString('th-TH') }}</span>
        </template>
      </span>
    </div>
    <div v-if="showOverall && total(sum.overall)" class="mt-2 flex h-2.5 overflow-hidden rounded-full bg-brand-soft" role="img"
      :aria-label="`เห็นด้วย ${sum.overall.yes} ไม่เห็นด้วย ${sum.overall.no} งดออกเสียง ${sum.overall.abstain}`">
      <div v-for="s in segments(sum.overall)" :key="s.key" :style="{ width: pct(s.n, sum.overall) + '%', background: s.bg }"></div>
    </div>
    <p v-if="multi && !compact" class="mt-1 text-xs text-muted">รวมเสียงทุกประเด็น ({{ sum.rows.length }} ประเด็น)</p>

    <ol v-if="!compact" class="mt-6 space-y-7">
      <li v-for="(r, i) in sum.rows" :key="r.question.id">
        <div v-if="r.question.label" class="mb-2 flex flex-wrap items-center gap-2">
          <h3 class="font-bold">{{ i + 1 }}. {{ r.question.label }}</h3>
          <VerdictBadge :verdict="verdict(r.tally)" />
        </div>
        <ResultBars :options="r.options" :total="total(r.tally)" approve />
      </li>
    </ol>
  </div>
</template>
