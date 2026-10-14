<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { adminLogout, loadVoter, logout, session } from './auth'

const route = useRoute()
const router = useRouter()
const inAdmin = computed(() => route.path.startsWith('/admin') && route.path !== '/admin/login')

onMounted(() => loadVoter().catch(() => {}))

async function doLogout() {
  await logout()
  router.push('/')
}
async function doAdminLogout() {
  await adminLogout()
  router.push('/admin/login')
}
</script>

<template>
  <header class="sticky top-0 z-20 border-b border-line bg-surface/95 backdrop-blur">
    <div class="mx-auto flex max-w-5xl items-center gap-4 px-4 py-3 sm:px-6">
      <RouterLink :to="inAdmin ? '/admin' : '/'" class="flex items-center gap-2.5 no-underline">
        <span class="grid h-8 w-8 place-items-center rounded-lg bg-brand">
          <svg viewBox="0 0 32 32" class="h-5 w-5" fill="none" stroke="#fff" stroke-width="3.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M8 17l5 5 11-12" /></svg>
        </span>
        <span class="font-serif text-lg font-bold text-ink">โหวตไทย</span>
        <span v-if="inAdmin" class="rounded-md bg-stamp-soft px-2 py-0.5 text-xs font-medium text-stamp">ผู้ดูแลระบบ</span>
      </RouterLink>

      <nav v-if="!inAdmin" class="ml-2 hidden gap-1 text-[15px] sm:flex">
        <RouterLink to="/" class="rounded-lg px-3 py-1.5 text-muted hover:bg-brand-soft hover:text-ink" active-class="!text-ink font-medium" exact-active-class="!text-ink font-medium">รายการโหวต</RouterLink>
        <RouterLink to="/verify" class="rounded-lg px-3 py-1.5 text-muted hover:bg-brand-soft hover:text-ink" active-class="!text-ink font-medium">ตรวจสอบใบรับรอง</RouterLink>
      </nav>
      <nav v-else class="ml-2 flex gap-1 text-[15px]">
        <RouterLink to="/admin" class="rounded-lg px-3 py-1.5 text-muted hover:bg-brand-soft hover:text-ink" exact-active-class="!text-ink font-medium">แบบโหวต</RouterLink>
        <RouterLink to="/admin/audit" class="rounded-lg px-3 py-1.5 text-muted hover:bg-brand-soft hover:text-ink" active-class="!text-ink font-medium">บันทึกการใช้งาน</RouterLink>
      </nav>

      <div class="ml-auto flex items-center gap-3 text-sm">
        <template v-if="inAdmin">
          <span class="hidden text-muted sm:inline">{{ session.admin?.name }}</span>
          <button class="btn btn-ghost !py-1.5" @click="doAdminLogout">ออกจากระบบ</button>
        </template>
        <template v-else-if="session.voter">
          <span class="hidden text-muted sm:inline">{{ session.voter.name }}</span>
          <button class="btn btn-ghost !py-1.5" @click="doLogout">ออกจากระบบ</button>
        </template>
        <RouterLink v-else-if="session.voterLoaded" to="/login" class="btn btn-primary !py-1.5 no-underline">เข้าสู่ระบบ</RouterLink>
      </div>
    </div>
  </header>

  <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-10">
    <RouterView />
  </main>

  <footer class="mx-auto max-w-5xl px-4 pb-10 pt-4 text-sm text-muted sm:px-6">
    <RouterLink v-if="!inAdmin" to="/admin/login" class="text-muted hover:text-ink">สำหรับผู้ดูแลระบบ</RouterLink>
  </footer>
</template>
