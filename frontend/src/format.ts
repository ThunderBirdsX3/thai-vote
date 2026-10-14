import type { Kind, Option, Poll, Question, Status } from './api'

const tz = 'Asia/Bangkok'

export const fmtDate = (iso: string) =>
  new Date(iso).toLocaleString('th-TH', { timeZone: tz, dateStyle: 'medium', timeStyle: 'short' })

export const statusLabel: Record<Status, string> = {
  draft: 'ร่าง',
  upcoming: 'เร็วๆ นี้',
  open: 'เปิดโหวต',
  closed: 'ปิดแล้ว',
}

export const kindLabel: Record<Kind, string> = {
  single: 'เลือก 1 ข้อ',
  multi: 'เลือกได้หลายข้อ',
  approve: 'เห็นด้วย / ไม่เห็นด้วย',
  comment: 'แสดงความเห็น',
}

// <input type="datetime-local"> uses the browser's local zone
export function toLocalInput(iso: string): string {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

export const fromLocalInput = (v: string) => new Date(v).toISOString()

export function timeLeft(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (ms <= 0) return ''
  const m = Math.floor(ms / 60000)
  if (m < 60) return `อีก ${m} นาที`
  const h = Math.floor(m / 60)
  if (h < 48) return `อีก ${h} ชั่วโมง`
  return `อีก ${Math.floor(h / 24)} วัน`
}

// Thai national id check digit (used by the mock login only)
export function withCheckDigit(first12: string): string {
  let sum = 0
  for (let i = 0; i < 12; i++) sum += Number(first12[i]) * (13 - i)
  return first12 + String((11 - (sum % 11)) % 10)
}

export function randomCitizenId(): string {
  let s = String(1 + Math.floor(Math.random() * 8))
  for (let i = 0; i < 11; i++) s += Math.floor(Math.random() * 10)
  return withCheckDigit(s)
}

// ---- approve-poll results ----

export const YES = 'เห็นด้วย'
export const NO = 'ไม่เห็นด้วย'
export const ABSTAIN = 'งดออกเสียง'

export interface Tally { yes: number; no: number; abstain: number }
export type Verdict = 'เห็นด้วย' | 'ไม่เห็นด้วย' | 'เสมอ' | null

/** Simple majority, abstentions not counted. null when nobody took a side. */
export function verdict(t: Tally): Verdict {
  if (t.yes + t.no === 0) return null
  return t.yes > t.no ? YES : t.no > t.yes ? NO : 'เสมอ'
}

export function tallyOf(options: Option[]): Tally {
  const t = { yes: 0, no: 0, abstain: 0 }
  for (const o of options) {
    if (o.label === YES) t.yes += o.count
    else if (o.label === NO) t.no += o.count
    else if (o.label === ABSTAIN) t.abstain += o.count
  }
  return t
}

/** Per-question tallies plus the overall one (all questions pooled) for an approve poll. */
export function approveSummary(poll: Pick<Poll, 'questions' | 'options'>) {
  const rows = poll.questions.map((q: Question) => ({
    question: q,
    options: poll.options.filter((o) => o.question_id === q.id),
    tally: tallyOf(poll.options.filter((o) => o.question_id === q.id)),
  }))
  const overall = tallyOf(poll.options)
  return { rows, overall, verdict: verdict(overall) }
}

/** Text content of sanitized HTML, for list previews. DOMParser never runs scripts. */
export function plainText(html: string): string {
  return new DOMParser().parseFromString(html, 'text/html').body.textContent?.replace(/\s+/g, ' ').trim() ?? ''
}
