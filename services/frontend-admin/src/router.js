import { createRouter, createWebHistory } from 'vue-router'
import LoginView from './views/LoginView.vue'
import AnalyticsView from './views/AnalyticsView.vue'
import ClientsView from './views/ClientsView.vue'
import ClientProfileView from './views/ClientProfileView.vue'
import OrdersView from './views/OrdersView.vue'
import MaterialsAdminView from './views/MaterialsAdminView.vue'
import QuestionsView from './views/QuestionsView.vue'
import ConsultationsAdminView from './views/ConsultationsAdminView.vue'
import ConsultationWorkspaceView from './views/ConsultationWorkspaceView.vue'

const routes = [
  { path: '/', redirect: '/analytics' },
  { path: '/login', component: LoginView, meta: { public: true } },
  { path: '/analytics', component: AnalyticsView },
  { path: '/clients', component: ClientsView },
  { path: '/clients/:id', component: ClientProfileView },
  { path: '/orders', component: OrdersView },
  { path: '/materials', component: MaterialsAdminView },
  { path: '/questions', component: QuestionsView },
  { path: '/consultations', component: ConsultationsAdminView },
  { path: '/consultations/:id', component: ConsultationWorkspaceView },
]

export const router = createRouter({
  history: createWebHistory('/admin/'),
  routes,
})
