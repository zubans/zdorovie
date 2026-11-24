<template>
  <div>
    <header v-if="user" class="topbar">
      <div class="container topbar-inner">
        <router-link class="brand" to="/dashboard">
          <img :src="mascotUrl" alt="Авокадо" class="brand-mascot" />
          <div>
            <div class="brand-title">Авокадо</div>
            <div class="brand-sub">Беременность без тревоги</div>
          </div>
        </router-link>

        <nav class="nav">
          <router-link to="/dashboard">Главная</router-link>
          <router-link to="/materials">Уроки</router-link>
          <router-link to="/consultations">Консультации</router-link>
          <router-link to="/documents">Заключения</router-link>
          <router-link to="/chat">
            <span>Чат с врачом</span>
            <span v-if="chatUnread > 0" class="nav-unread-pill">{{ chatUnread }}</span>
          </router-link>
        </nav>

        <div class="profile-chip-wrap">
          <router-link to="/profile" class="profile-chip" title="Профиль и подписка">
            <span class="avatar-circle">{{ getInitials(user.full_name) }}</span>
            <span>
              <b style="font-weight:600">{{ firstName }}</b>
              <span class="muted"> · {{ user.pregnancy_status || 'Профиль' }}</span>
            </span>
          </router-link>
          <button class="btn btn-secondary btn-sm" title="Выйти из кабинета" @click="logout">
            Выйти
          </button>
        </div>
      </div>
    </header>

    <main class="container page">
      <router-view :user="user" @authenticated="loadSession" @chat-read="chatUnread = 0" />
    </main>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { apiFetch, setToken } from './api'
import { getInitials } from './utils/formatSummary'
import mascotUrl from './assets/avocado-mascot.jpg'

const router = useRouter()
const route = useRoute()
const user = ref(null)
const chatUnread = ref(0)
let unreadTimer = null

const firstName = computed(() => {
  const full = String(user.value?.full_name || '').trim()
  if (!full) return 'Анна'
  const parts = full.split(/\s+/)
  return parts.length > 1 && parts[0].endsWith('а') && parts[1] ? parts[1] : parts[0]
})

async function refreshChatUnread() {
  if (!user.value) {
    chatUnread.value = 0
    return
  }
  try {
    const res = await apiFetch('/api/v1/cabinet/chat/unread')
    chatUnread.value = res.unread_count || 0
  } catch {}
}

async function loadSession() {
  try {
    const res = await apiFetch('/api/v1/auth/me')
    user.value = res.user
    await refreshChatUnread()
  } catch {
    user.value = null
    chatUnread.value = 0
    if (!route.meta?.public) {
      router.replace('/login')
    }
  }
}

async function logout() {
  try {
    await apiFetch('/api/v1/auth/logout', { method: 'POST' })
  } catch {}
  setToken('')
  user.value = null
  chatUnread.value = 0
  router.replace('/login')
}

watch(
  () => route.path,
  () => {
    if (route.path === '/chat') chatUnread.value = 0
    else refreshChatUnread()
  }
)

onMounted(() => {
  if (!route.path.startsWith('/auth/magic')) {
    loadSession()
  }
  unreadTimer = setInterval(refreshChatUnread, 10000)
})

onUnmounted(() => {
  if (unreadTimer) clearInterval(unreadTimer)
})
</script>
