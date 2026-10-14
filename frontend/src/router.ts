import { createRouter, createWebHistory } from 'vue-router'
import { loadAdmin } from './auth'

export const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    { path: '/', component: () => import('./views/Home.vue') },
    { path: '/login', component: () => import('./views/Login.vue') },
    { path: '/polls/:id', component: () => import('./views/PollView.vue'), props: true },
    { path: '/verify/:code?', component: () => import('./views/Verify.vue'), props: true },
    { path: '/admin/login', component: () => import('./views/admin/AdminLogin.vue') },
    { path: '/admin', component: () => import('./views/admin/Dashboard.vue'), meta: { admin: true } },
    { path: '/admin/polls/new', component: () => import('./views/admin/PollForm.vue'), meta: { admin: true } },
    { path: '/admin/polls/:id/edit', component: () => import('./views/admin/PollForm.vue'), props: true, meta: { admin: true } },
    { path: '/admin/polls/:id', component: () => import('./views/admin/PollAdmin.vue'), props: true, meta: { admin: true } },
    { path: '/admin/audit', component: () => import('./views/admin/Audit.vue'), meta: { admin: true } },
    { path: '/:pathMatch(.*)*', component: () => import('./views/NotFound.vue') },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.admin && !(await loadAdmin())) {
    return { path: '/admin/login', query: { next: to.fullPath } }
  }
})
