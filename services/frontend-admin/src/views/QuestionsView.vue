<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div>
      <h1 style="margin:0;">Вопросы из бота MAX</h1>
      <p class="muted" style="margin:3px 0 0;">
        ИИ предлагает 3 варианта ответа с учётом карточки пациентки, срока беременности и тарифа
      </p>
    </div>

    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <div v-if="questions.length === 0" class="avo-card muted" style="margin:0;">
      Вопросов из бота пока нет.
    </div>

    <div
      v-for="q in questions"
      :key="q.id"
      class="avo-card"
      style="margin:0;display:flex;flex-direction:column;gap:12px;"
    >
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <b>{{ q.author_name || `MAX #${q.max_user_id}` }}</b>
          <span class="muted" style="margin-left:8px;font-size:0.78rem;">{{ formatDateTime(q.created_at) }}</span>
        </div>
        <div style="display:flex;gap:8px;align-items:center;">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="suggestingId === q.id"
            @click="suggestReplies(q)"
          >
            {{ suggestingId === q.id ? 'Подбираем...' : 'Подобрать ответы ИИ' }}
          </button>
          <span class="badge" :class="q.status === 'answered' ? 'badge-green' : 'badge-peach'">
            {{ q.status === 'answered' ? 'Отвечено ✓' : 'Новый' }}
          </span>
        </div>
      </div>

      <div
        v-if="q.matched_user_id"
        class="avo-soft"
        style="padding:9px 13px;display:flex;gap:12px;flex-wrap:wrap;align-items:center;font-size:0.82rem;"
      >
        <span>
          Пациентка:
          <router-link :to="`/clients/${q.matched_user_id}`"><b>{{ q.client_name }}</b></router-link>
        </span>
        <span>Срок: <b>{{ q.client_pregnancy_status || 'не указан' }}</b></span>
        <span>Тариф: <b>{{ q.tariff_title || 'нет' }}</b></span>
        <span class="badge" :class="q.subscription_active ? 'badge-green' : 'badge-red'">
          {{ q.subscription_active ? 'Подписка активна' : 'Не активна' }}
        </span>
      </div>
      <div v-else class="muted" style="font-size:0.78rem;">
        Гостевой вопрос из бота MAX (карточка в CRM ещё не оформлена).
      </div>

      <div class="avo-soft" style="font-size:0.9rem;line-height:1.5;">
        {{ q.question_text }}
      </div>

      <div v-if="suggestions[q.id]?.length" class="avo-flesh" style="display:flex;flex-direction:column;gap:10px;">
        <div style="font-size:0.86rem;font-weight:700;">
          Варианты ответа ИИ (на основе карточки и вопроса)
        </div>
        <div style="display:grid;gap:8px;">
          <div
            v-for="v in suggestions[q.id]"
            :key="v.id"
            class="avo-card"
            style="margin:0;padding:12px 14px;border-radius:14px;"
          >
            <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:5px;gap:8px;">
              <div>
                <b style="font-size:0.84rem;">{{ v.title }}</b>
                <span class="badge badge-soft" style="margin-left:6px;">{{ v.tone }}</span>
              </div>
              <button type="button" class="btn btn-sm" @click="q.answer_text = v.text">
                Подставить в ответ
              </button>
            </div>
            <div style="font-size:0.84rem;line-height:1.45;">{{ v.text }}</div>
          </div>
        </div>
      </div>

      <div style="display:flex;gap:8px;align-items:flex-end;">
        <textarea
          v-model="q.answer_text"
          rows="2"
          class="input input-soft"
          style="flex:1;"
          placeholder="Ответ (будет доставлен пользовательнице в мессенджер MAX)..."
        ></textarea>
        <button type="button" class="btn" @click="reply(q)">Отправить в MAX</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiFetch, formatDateTime } from '../api'

const questions = ref([])
const suggestions = ref({})
const suggestionModels = ref({})
const suggestingId = ref(null)
const notice = ref('')
const errorMsg = ref('')

async function loadQuestions() {
  try {
    const res = await apiFetch('/api/v1/admin/questions')
    questions.value = res.questions || []
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function suggestReplies(q) {
  suggestingId.value = q.id
  errorMsg.value = ''
  try {
    const res = await apiFetch(`/api/v1/admin/questions/${q.id}/suggest-replies`, { method: 'POST' })
    suggestions.value = { ...suggestions.value, [q.id]: res.variants || [] }
    suggestionModels.value = { ...suggestionModels.value, [q.id]: res.model_name || '' }
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    suggestingId.value = null
  }
}

async function reply(q) {
  try {
    await apiFetch(`/api/v1/admin/questions/${q.id}/reply`, {
      method: 'POST',
      body: { answer: q.answer_text },
    })
    notice.value = 'Ответ отправлен в MAX!'
    await loadQuestions()
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(loadQuestions)
</script>
