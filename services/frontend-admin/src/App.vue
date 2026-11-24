<template>
  <div v-if="user" class="layout">
    <aside class="sidebar">
      <div>
        <div class="sidebar-brand">
          <img :src="mascotUrl" alt="Авокадо" class="sidebar-mascot" />
          <div>
            <div class="sidebar-title">Авокадо · Врач</div>
            <div class="sidebar-sub">Клиника и забота</div>
          </div>
        </div>

        <nav class="sidebar-nav" style="margin-top:10px;">
          <div class="sidebar-section-label">ПРИЁМ И ПАЦИЕНТКИ</div>
          <router-link to="/analytics">
            <span>Сегодня (Приёмная)</span>
          </router-link>
          <router-link to="/clients">
            <span>Пациентки и чат</span>
            <span v-if="unreadChatTotal > 0" class="sidebar-badge-peach">{{ unreadChatTotal }}</span>
          </router-link>
          <router-link to="/consultations">
            <span>Консультации и ИИ</span>
          </router-link>
          <router-link to="/questions">
            <span>Вопросы в боте MAX</span>
          </router-link>

          <div class="sidebar-section-label" style="margin-top:8px;">КУРС И ОПЛАТЫ</div>
          <router-link to="/materials">
            <span>Курс и тарифы</span>
          </router-link>
          <router-link to="/orders">
            <span>Заказы и канал MAX</span>
          </router-link>
        </nav>
      </div>

      <div class="avo-card" style="margin:0;padding:13px 14px;border-radius:16px;">
        <div style="font-size:0.84rem;font-weight:700;">{{ user.full_name || 'Врач курса' }}</div>
        <div class="muted" style="font-size:0.75rem;margin-top:2px;">Терапевт · нутрициолог</div>
        <button class="btn btn-secondary btn-sm" style="width:100%;margin-top:10px;" @click="logout">
          Выйти
        </button>
      </div>
    </aside>

    <main class="main">
      <router-view @authenticated="loadSession" @chat-updated="refreshUnreadChat" />
    </main>
  </div>

  <main v-else class="main" style="margin:0 auto;">
    <router-view @authenticated="loadSession" />
  </main>
</template>

<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { apiFetch, setToken } from './api'
import mascotUrl from './assets/avocado-mascot.jpg'

const router = useRouter()
const route = useRoute()
const user = ref(null)
const unreadChatTotal = ref(0)
let chatPoll = null

async function refreshUnreadChat() {
  if (!user.value) {
    unreadChatTotal.value = 0
    return
  }
  try {
    const res = await apiFetch('/api/v1/admin/chat/unread')
    unreadChatTotal.value = res.unread_count || 0
  } catch {}
}

async function loadSession() {
  try {
    const res = await apiFetch('/api/v1/auth/me')
    if (res.user?.role !== 'admin' && res.user?.role !== 'doctor') {
      throw new Error('Требуется роль врача или администратора')
    }
    user.value = res.user
    await refreshUnreadChat()
  } catch {
    user.value = null
    unreadChatTotal.value = 0
    if (!route.meta?.public) {
      router.replace('/login')
    }
  }
}

async function logout() {
  try { await apiFetch('/api/v1/auth/logout', { method: 'POST' }) } catch {}
  setToken('')
  user.value = null
  unreadChatTotal.value = 0
  router.replace('/login')
}

watch(
  () => route.fullPath,
  () => refreshUnreadChat()
)

onMounted(() => {
  loadSession()
  chatPoll = setInterval(refreshUnreadChat, 10000)
})

onUnmounted(() => {
  if (chatPoll) clearInterval(chatPoll)
})
</script>
