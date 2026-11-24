import { createRouter, createWebHistory } from 'vue-router'
import LoginView from './views/LoginView.vue'
import DashboardView from './views/DashboardView.vue'
import MaterialsView from './views/MaterialsView.vue'
import ConsultationsView from './views/ConsultationsView.vue'
import ChatView from './views/ChatView.vue'
import DocumentsView from './views/DocumentsView.vue'
import ProfileView from './views/ProfileView.vue'

const routes = [
  { path: '/', redirect: '/dashboard' },
  { path: '/login', component: LoginView, meta: { public: true } },
  { path: '/auth/magic', component: LoginView, meta: { public: true } },
  { path: '/dashboard', component: DashboardView },
  { path: '/materials', component: MaterialsView },
  { path: '/consultations', component: ConsultationsView },
  { path: '/chat', component: ChatView },
  { path: '/documents', component: DocumentsView },
  { path: '/profile', component: ProfileView },
]

export const router = createRouter({
  history: createWebHistory('/cabinet/'),
  routes,
})
