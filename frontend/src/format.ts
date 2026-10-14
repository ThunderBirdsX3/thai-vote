import type { Kind, Status } from './api'

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
