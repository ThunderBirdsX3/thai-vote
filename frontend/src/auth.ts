import { reactive } from 'vue'
import { api, ApiError } from './api'

export interface Voter { id: string; name: string; citizen_id: string }
export interface Admin { id: string; username: string; name: string }

export const session = reactive({
  voter: null as Voter | null,
  admin: null as Admin | null,
  voterLoaded: false,
  adminLoaded: false,
})

async function me<T>(path: string): Promise<T | null> {
  try {
    return await api.get<T>(path)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

export async function loadVoter(force = false) {
  if (session.voterLoaded && !force) return session.voter
  session.voter = await me<Voter>('/me')
  session.voterLoaded = true
  return session.voter
}

export async function mockLogin(citizen_id: string, name: string) {
  session.voter = await api.post<Voter>('/auth/mock-thaiid', { citizen_id, name })
  session.voterLoaded = true
}

export async function logout() {
  await api.post('/auth/logout')
  session.voter = null
}

export async function loadAdmin(force = false) {
  if (session.adminLoaded && !force) return session.admin
  session.admin = await me<Admin>('/admin/me')
  session.adminLoaded = true
  return session.admin
}

export async function adminLogin(username: string, password: string) {
  session.admin = await api.post<Admin>('/admin/login', { username, password })
  session.adminLoaded = true
}

export async function adminLogout() {
  await api.post('/admin/logout')
  session.admin = null
}
