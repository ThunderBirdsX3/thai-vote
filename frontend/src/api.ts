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
}

export type Kind = 'single' | 'multi' | 'approve' | 'comment'
export type Status = 'draft' | 'upcoming' | 'open' | 'closed'

export interface Option { id: string; label: string }
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
  options: Option[]
  has_voted: boolean
  ballot_count: number
}
export interface OptionResult extends Option { count: number }
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
  options: string[]
}
