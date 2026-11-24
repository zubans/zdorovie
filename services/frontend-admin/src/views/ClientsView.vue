<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:10px;">
      <div>
        <h1 style="margin:0;">Пациентки и входящие сообщения</h1>
        <p class="muted" style="margin:3px 0 0;">
          Переход в сплит-карточку (чат + ИИ-резюме и история) в один клик
        </p>
      </div>
      <span class="badge badge-peach">Непрочитанные подняты наверх</span>
    </div>

    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <!-- Filter bar -->
    <div class="avo-card" style="margin:0;padding:16px 20px;display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;">
      <div class="field" style="margin:0;min-width:180px;">
        <label>Статус подписки</label>
        <select v-model="statusFilter" class="input input-soft" @change="loadClients">
          <option value="">Все пациентки</option>
          <option value="active">Активна</option>
          <option value="expired">Истекла</option>
        </select>
      </div>
      <div class="field" style="margin:0;min-width:200px;">
        <label>Код тарифа</label>
        <input v-model="tariffFilter" class="input input-soft" placeholder="например feedback_3m" @keyup.enter="loadClients" />
      </div>
      <button
        type="button"
        class="btn btn-sm"
        :class="onlyUnread ? 'btn-peach' : 'btn-secondary'"
        style="height:38px;"
        @click="onlyUnread = !onlyUnread"
      >
        {{ onlyUnread ? 'Показаны только с новыми сообщениями' : 'Только с новыми сообщениями' }}
        <span v-if="totalUnreadMessages > 0">({{ totalUnreadMessages }})</span>
      </button>
      <button type="button" class="btn btn-secondary btn-sm" style="height:38px;margin-left:auto;" @click="loadClients">
        Обновить
      </button>
    </div>

    <!-- Patient Cards List (matches prototype #docScreenClients + full actions) -->
    <div style="display:flex;flex-direction:column;gap:10px;">
      <div v-if="displayedClients.length === 0" class="avo-card muted" style="margin:0;text-align:center;">
        Пациенток по выбранному фильтру не найдено.
      </div>

      <div
        v-for="c in displayedClients"
        :key="c.id"
        class="avo-card"
        style="margin:0;padding:16px 20px;display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;"
        :style="c.unread_chat_count > 0 ? 'background:#fff6ef;border-left:4px solid #c96f3b;' : ''"
      >
        <div style="display:flex;align-items:center;gap:14px;flex:1;min-width:260px;">
          <span class="avatar-circle" :class="c.unread_chat_count > 0 ? '' : 'olive'">
            {{ getInitials(c.full_name) }}
          </span>
          <div style="display:flex;flex-direction:column;gap:3px;">
            <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
              <router-link :to="`/clients/${c.id}`" style="font-size:0.98rem;font-weight:700;color:var(--text);">
                {{ c.full_name }}
              </router-link>
              <span v-if="c.unread_chat_count > 0" class="badge badge-peach">
                Новое +{{ c.unread_chat_count }}
              </span>
              <span class="muted" style="font-size:0.8rem;">· {{ c.pregnancy_status || 'срок не указан' }}</span>
              <span class="badge" :class="c.subscription_active ? 'badge-green' : 'badge-red'">
                {{ c.tariff_title || 'Без тарифа' }}
                <template v-if="c.expires_at"> · до {{ formatDate(c.expires_at) }}</template>
              </span>
            </div>

            <div v-if="c.unread_chat_count > 0 && c.last_chat_preview" style="font-size:0.84rem;color:var(--accent-text);font-weight:500;">
              «{{ c.last_chat_preview }}»
            </div>
            <div class="muted" style="font-size:0.78rem;">
              {{ c.email }} · {{ c.phone || '—' }} · Уроков (PDF): {{ c.materials_ready }}/{{ c.materials_total }} ·
              Встреч: {{ c.booked_consultations + c.completed_consultations }} ·
              Сообщений в чате: {{ c.total_chat_count || 0 }}
            </div>
          </div>
        </div>

        <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
          <router-link
            :to="`/clients/${c.id}#chat`"
            class="btn btn-sm"
            :class="c.unread_chat_count > 0 ? 'btn-peach' : ''"
          >
            {{ c.unread_chat_count > 0 ? 'Открыть чат и профиль →' : 'Карточка и чат' }}
          </router-link>
          <button type="button" class="btn btn-secondary btn-sm" @click="openEdit(c)">
            Тариф / Срок
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="resendMagic(c)">
            Magic Link
          </button>
        </div>
      </div>
    </div>

    <!-- Subscription Editor Drawer -->
    <div v-if="editing" class="avo-card" style="margin:0;border-color:var(--primary);">
      <h3>Редактировать подписку: {{ editing.full_name }}</h3>
      <div class="grid-2">
        <div class="field">
          <label>Тариф</label>
          <select v-model="editForm.tariff_id">
            <option v-for="t in tariffs" :key="t.id" :value="t.id">{{ t.title }}</option>
          </select>
        </div>
        <div class="field">
          <label>Дата окончания (ГГГГ-ММ-ДД)</label>
          <input v-model="editForm.expires_at" type="date" class="input" />
        </div>
      </div>
      <div style="display:flex;gap:8px;">
        <button class="btn" @click="saveSub">Сохранить</button>
        <button class="btn btn-secondary" @click="editing = null">Отмена</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { apiFetch, formatDate } from '../api'
import { getInitials } from '../utils/formatSummary'

const emit = defineEmits(['chat-updated'])

const clients = ref([])
const tariffs = ref([])
const statusFilter = ref('')
const tariffFilter = ref('')
const onlyUnread = ref(false)
const editing = ref(null)
const editForm = ref({ tariff_id: 1, expires_at: '' })
const notice = ref('')
const errorMsg = ref('')

let pollTimer = null

const totalUnreadMessages = computed(() =>
  clients.value.reduce((sum, c) => sum + (c.unread_chat_count || 0), 0)
)

const displayedClients = computed(() => {
  if (!onlyUnread.value) return clients.value
  return clients.value.filter((c) => (c.unread_chat_count || 0) > 0)
})

async function loadClients() {
  try {
    const q = new URLSearchParams()
    if (statusFilter.value) q.set('status', statusFilter.value)
    if (tariffFilter.value.trim()) q.set('tariff', tariffFilter.value.trim())
    const res = await apiFetch(`/api/v1/admin/clients?${q.toString()}`)
    clients.value = res.clients || []
    emit('chat-updated')
  } catch (e) {
    errorMsg.value = e.message
  }
}

function openEdit(c) {
  editing.value = c
  const match = tariffs.value.find((t) => t.code === c.tariff_code)
  editForm.value = {
    tariff_id: match ? match.id : (tariffs.value[0]?.id || 1),
    expires_at: c.expires_at ? c.expires_at.slice(0, 10) : new Date(Date.now() + 90 * 86400000).toISOString().slice(0, 10),
  }
}

async function saveSub() {
  try {
    await apiFetch(`/api/v1/admin/clients/${editing.value.id}/subscription`, {
      method: 'PATCH',
      body: editForm.value,
    })
    notice.value = 'Подписка обновлена.'
    editing.value = null
    await loadClients()
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function resendMagic(c) {
  try {
    const res = await apiFetch(`/api/v1/admin/clients/${c.id}/magic-link`, { method: 'POST' })
    notice.value = res.magic_url ? `Magic Link отправлен и скопирован: ${res.magic_url}` : `Magic Link переотправлен на ${c.email}`
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(async () => {
  await loadClients()
  try {
    const tRes = await apiFetch('/api/v1/admin/tariffs')
    tariffs.value = tRes.tariffs || []
  } catch {}
  pollTimer = setInterval(loadClients, 10000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>
