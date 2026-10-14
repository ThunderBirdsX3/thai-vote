export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error ?? 'เกิดข้อผิดพลาด');
  return data as T
}

export const api = {
  get: <T>(p: string) => req<T>('GET', p),
  post: <T>(p: string, b?: unknown) => req<T>('POST', p, b ?? {}),
  put: <T>(p: string, b: unknown) => req<T>('PUT', p, b),
  del: <T>(p: string) => req<T>('DELETE', p),
  // raw body upload (promo image); the server sniffs the real type
  upload: async (p: string, file: Blob) => {
    const res = await fetch('/api' + p, { method: 'PUT', credentials: 'same-origin', body: file })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new ApiError(res.status, data.error ?? 'เกิดข้อผิดพลาด')
  },
}

export type Kind = 'single' | 'multi' | 'approve' | 'comment'
export type Status = 'draft' | 'upcoming' | 'open' | 'closed'

export interface Option { id: string; label: string; question_id?: string; count: number }
export interface Question { id: string; label: string }
export interface Poll {
  id: string
  title: string
  description: string
  kind: Kind
  max_choices: number
  anonymous: boolean
  results_visibility: 'live' | 'after_close'
  starts_at: string
  ends_at: string
  published_at: string | null
  closed_at: string | null
  status: Status
  allow_comment: boolean
  questions: Question[]
  options: Option[]
  image_url: string
  results_visible: boolean
  has_voted: boolean
  ballot_count: number
}
export interface PollPage {
  items: Poll[]
  total: number
  page: number
  per_page: number
  counts: Record<Status, number>
}
export type OptionResult = Option
export interface BallotOut {
  id?: string
  name: string | null
  choices: string[]
  comment: string | null
  hidden: boolean
  hide_reason?: string
}
export interface Results {
  poll: Poll
  total: number
  options: OptionResult[]
  ballots: BallotOut[]
  head_hash: string
}
export interface Verify {
  ok: boolean
  ballots: number
  participations: number
  head_hash: string
  problems: string[]
  checked_at: string
}
export interface PollInput {
  title: string
  description: string
  kind: Kind
  max_choices: number
  anonymous: boolean
  results_visibility: 'live' | 'after_close'
  starts_at: string
  ends_at: string
  allow_comment: boolean
  questions: string[]
  options: string[]
}
