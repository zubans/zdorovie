<template>
  <div style="display:flex;flex-direction:column;gap:18px;">
    <div style="display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap;">
      <div>
        <h1 style="margin:0;">Приёмная врача: что требует внимания</h1>
        <p class="muted" style="margin:3px 0 0;">
          Клинические задачи дня — отдельно от финансовой аналитики и технических очередей
        </p>
      </div>

      <div class="tab-pill-group">
        <button
          type="button"
          class="tab-pill-btn"
          :class="{ active: activeTab === 'tasks' }"
          @click="activeTab = 'tasks'"
        >
          Задачи врача
        </button>
        <button
          type="button"
          class="tab-pill-btn"
          :class="{ active: activeTab === 'system' }"
          @click="activeTab = 'system'"
        >
          Финансы и очереди
        </button>
      </div>
    </div>

    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <!-- TAB 1: DOCTOR CLINICAL TASKS (TODAY) -->
    <template v-if="activeTab === 'tasks'">
      <div class="grid-3">
        <div
          class="avo-card"
          style="margin:0;border-left:4px solid #c96f3b;display:flex;flex-direction:column;gap:10px;"
        >
          <div class="muted" style="font-weight:600;font-size:0.8rem;">Непрочитанные чаты</div>
          <div style="font-size:1.7rem;font-weight:700;line-height:1.1;">
            {{ unreadChatCount }} сообщ.
          </div>
          <div class="muted" style="font-size:0.78rem;">
            Личные вопросы пациенток из кабинета
          </div>
          <div style="margin-top:auto;">
            <router-link to="/clients" class="btn btn-peach btn-sm">
              Перейти к пациенткам →
            </router-link>
          </div>
        </div>

        <div
          class="avo-card"
          style="margin:0;border-left:4px solid #4b7026;display:flex;flex-direction:column;gap:10px;"
        >
          <div class="muted" style="font-weight:600;font-size:0.8rem;">Черновики ИИ (HITL)</div>
          <div style="font-size:1.7rem;font-weight:700;line-height:1.1;">
            {{ analytics?.pending_reviews_count ?? 0 }} на проверке
          </div>
          <div class="muted" style="font-size:0.78rem;">
            Ожидают утверждения врачом перед отправкой PDF
          </div>
          <div style="margin-top:auto;">
            <router-link to="/consultations?filter=draft" class="btn btn-sm">
              Проверить и утвердить →
            </router-link>
          </div>
        </div>

        <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:10px;">
          <div class="muted" style="font-weight:600;font-size:0.8rem;">Вопросы в боте MAX</div>
          <div style="font-size:1.7rem;font-weight:700;line-height:1.1;">
            {{ newQuestionsCount }} новых
          </div>
          <div class="muted" style="font-size:0.78rem;">
            ИИ подбирает 3 варианта ответа по карточке
          </div>
          <div style="margin-top:auto;">
            <router-link to="/questions" class="btn btn-secondary btn-sm">
              Подобрать ответы ИИ →
            </router-link>
          </div>
        </div>
      </div>

      <!-- Patients with unread chat right on Today screen -->
      <div v-if="unreadClients.length > 0" class="avo-card" style="margin:0;">
        <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:12px;">
          <h3 style="margin:0;">Ждут ответа в личном чате</h3>
          <router-link to="/clients" style="font-size:0.8rem;font-weight:600;">Все пациентки →</router-link>
        </div>
        <div style="display:flex;flex-direction:column;gap:10px;">
          <div
            v-for="c in unreadClients"
            :key="c.id"
            class="avo-soft"
            style="background:#fff6ef;border-left:4px solid #c96f3b;display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;"
          >
            <div style="display:flex;align-items:center;gap:12px;">
              <span class="avatar-circle">{{ getInitials(c.full_name) }}</span>
              <div>
                <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
                  <span style="font-weight:700;font-size:0.92rem;">{{ c.full_name }}</span>
                  <span class="badge badge-peach">Новое +{{ c.unread_chat_count }}</span>
                  <span class="muted" style="font-size:0.78rem;">{{ c.pregnancy_status || '—' }}</span>
                </div>
                <div class="muted" style="font-size:0.8rem;margin-top:2px;">
                  «{{ c.last_chat_preview }}»
                </div>
              </div>
            </div>
            <router-link :to="`/clients/${c.id}#chat`" class="btn btn-peach btn-sm">
              Открыть чат и профиль →
            </router-link>
          </div>
        </div>
      </div>

      <!-- Summary bar of active clinical load -->
      <div v-if="analytics" class="avo-flesh" style="display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;">
        <div>
          <div style="font-weight:700;font-size:0.95rem;">
            На сопровождении сейчас: {{ analytics.active_clients_count }} активных пациенток
          </div>
          <div class="muted" style="font-size:0.8rem;margin-top:2px;">
            Диалогов в MAX: {{ analytics.bot_sessions_count }} · Оплаченных доступов: {{ analytics.paid_orders_count }}
          </div>
        </div>
        <div style="display:flex;gap:8px;flex-wrap:wrap;">
          <router-link to="/consultations" class="btn btn-secondary btn-sm">Расписание и слоты</router-link>
          <router-link to="/clients" class="btn btn-sm">База пациенток</router-link>
        </div>
      </div>
    </template>

    <!-- TAB 2: FINANCES & QUEUES (SYSTEM) -->
    <template v-else>
      <div v-if="analytics" class="grid-4">
        <div class="avo-card" style="margin:0;">
          <div class="muted">Общая выручка</div>
          <h2 style="margin:6px 0 4px;">{{ formatRub(analytics.total_revenue_kopecks) }}</h2>
          <div class="muted">Оплачено заказов: {{ analytics.paid_orders_count }}</div>
        </div>
        <div class="avo-card" style="margin:0;">
          <div class="muted">Активные пациентки</div>
          <h2 style="margin:6px 0 4px;">{{ analytics.active_clients_count }}</h2>
          <div class="muted">Сессий бота: {{ analytics.bot_sessions_count }}</div>
        </div>
        <div class="avo-card" style="margin:0;">
          <div class="muted">Конверсия MAX → Оплата</div>
          <h2 style="margin:6px 0 4px;">{{ analytics.conversion_percent }}%</h2>
          <div class="muted">Ожидают оплаты: {{ analytics.pending_orders_count }}</div>
        </div>
        <div class="avo-card" style="margin:0;">
          <div class="muted">Черновики ИИ</div>
          <h2 style="margin:6px 0 4px;">{{ analytics.pending_reviews_count }}</h2>
          <router-link to="/consultations?filter=draft" class="btn btn-secondary btn-sm">
            К черновикам
          </router-link>
        </div>
      </div>

      <div v-if="analytics?.by_tariff?.length" class="avo-card" style="margin:0;">
        <h3>Продажи по тарифам</h3>
        <table>
          <thead>
            <tr><th>Код</th><th>Тариф</th><th>Оплаченных заказов</th><th>Выручка</th></tr>
          </thead>
          <tbody>
            <tr v-for="t in analytics.by_tariff" :key="t.code">
              <td><code>{{ t.code }}</code></td>
              <td>{{ t.title }}</td>
              <td>{{ t.orders_count }}</td>
              <td><b>{{ formatRub(t.revenue_kopecks) }}</b></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="queues" class="grid-2">
        <div class="avo-card" style="margin:0;">
          <h3>Очереди Go Worker (Asynq: Watermark / Email)</h3>
          <table>
            <thead>
              <tr><th>Очередь</th><th>Pending</th><th>Active</th><th>Retry</th><th>Archived</th><th>Processed</th></tr>
            </thead>
            <tbody>
              <tr v-for="q in queues.asynq_queues || []" :key="q.name">
                <td><code>{{ q.name }}</code></td>
                <td>{{ q.pending }}</td>
                <td>{{ q.active }}</td>
                <td>{{ q.retry }}</td>
                <td>{{ q.archived }}</td>
                <td>{{ q.processed }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="avo-card" style="margin:0;">
          <h3>Очередь Python ML Worker (STT + LLM)</h3>
          <div class="avo-soft" style="display:grid;gap:8px;font-size:0.85rem;">
            <div>Ожидают транскрибации (<code>audio_transcribe</code>): <b>{{ queues.ml?.transcribe_pending || 0 }}</b></div>
            <div>Ожидают суммаризации (<code>text_summarize</code>): <b>{{ queues.ml?.summarize_pending || 0 }}</b></div>
            <div>Обработано задач: <b>{{ queues.ml?.processed_count || 0 }}</b></div>
            <div>
              Ошибок (<code>ml_failed</code>):
              <span class="badge" :class="queues.ml?.failed_count ? 'badge-red' : 'badge-green'">
                {{ queues.ml?.failed_count || 0 }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { apiFetch, formatRub } from '../api'
import { getInitials } from '../utils/formatSummary'

const activeTab = ref('tasks')
const analytics = ref(null)
const queues = ref(null)
const clients = ref([])
const questions = ref([])
const unreadChatCount = ref(0)
const errorMsg = ref('')

const unreadClients = computed(() =>
  clients.value.filter((c) => (c.unread_chat_count || 0) > 0)
)

const newQuestionsCount = computed(() =>
  questions.value.filter((q) => q.status !== 'answered').length
)

onMounted(async () => {
  try {
    const [aRes, uRes, cRes, qRes] = await Promise.allSettled([
      apiFetch('/api/v1/admin/analytics'),
      apiFetch('/api/v1/admin/chat/unread'),
      apiFetch('/api/v1/admin/clients'),
      apiFetch('/api/v1/admin/questions'),
    ])
    if (aRes.status === 'fulfilled') {
      analytics.value = aRes.value.analytics
      queues.value = aRes.value.queues
    } else {
      errorMsg.value = aRes.reason?.message || 'Ошибка загрузки аналитики'
    }
    if (uRes.status === 'fulfilled') {
      unreadChatCount.value = uRes.value.unread_count || 0
    }
    if (cRes.status === 'fulfilled') {
      clients.value = cRes.value.clients || []
    }
    if (qRes.status === 'fulfilled') {
      questions.value = qRes.value.questions || []
    }
  } catch (e) {
    errorMsg.value = e.message
  }
})
</script>
