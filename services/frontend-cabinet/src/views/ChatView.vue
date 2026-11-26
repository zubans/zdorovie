<template>
  <div>
    <div v-if="errorMsg" class="alert alert-error">{{ errorMsg }}</div>

    <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:14px;">
      <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;">
        <div>
          <h2 style="font-size:1.15rem;margin:0;">Личный чат с врачом</h2>
          <div class="muted" style="font-size:0.8rem;margin-top:2px;">
            {{ doctorName || 'Врач-терапевт, нутрициолог' }} · конфиденциально внутри кабинета
          </div>
        </div>
        <div style="display:flex;align-items:center;gap:8px;">
          <span v-if="allClientMessagesRead" class="badge badge-green">✓ Сообщения прочитаны</span>
          <button type="button" class="btn btn-secondary btn-sm" @click="loadMessages(false)">Обновить</button>
        </div>
      </div>

      <div
        ref="scrollBox"
        class="avo-soft"
        style="height:430px;overflow-y:auto;padding:16px;display:flex;flex-direction:column;gap:10px;"
      >
        <div v-if="loading && messages.length === 0" class="muted" style="margin:auto;">
          Загружаем переписку...
        </div>
        <div
          v-else-if="messages.length === 0"
          class="muted"
          style="margin:auto;text-align:center;max-width:420px;font-size:0.85rem;"
        >
          Напишите ваш вопрос по самочувствию, анализам или материалам курса — врач увидит сообщение у себя в карточке и ответит здесь.
        </div>

        <div
          v-for="m in messages"
          :key="m.id"
          style="display:flex;"
          :style="m.sender_role === 'client' ? 'justify-content:flex-end;' : 'justify-content:flex-start;'"
        >
          <div
            style="max-width:76%;padding:11px 15px;border-radius:18px;font-size:0.85rem;line-height:1.48;"
            :style="
              m.sender_role === 'client'
                ? 'background:var(--primary-soft);color:var(--text);border-bottom-right-radius:4px;'
                : 'background:#ffffff;border:1px solid rgba(0,0,0,0.06);border-bottom-left-radius:4px;'
            "
          >
            <div
              style="display:flex;justify-content:space-between;gap:12px;font-size:0.75rem;margin-bottom:3px;"
            >
              <b :style="m.sender_role === 'client' ? 'color:var(--primary-text);' : 'color:var(--primary);'">
                {{ m.sender_role === 'client' ? 'Вы' : (m.sender_name || doctorName || 'Врач') }}
              </b>
              <span class="muted" style="font-size:0.72rem;">{{ formatDateTime(m.created_at) }}</span>
            </div>
            <div style="white-space:pre-wrap;">{{ m.body }}</div>
            <div
              v-if="m.sender_role === 'client'"
              class="muted"
              style="font-size:0.72rem;text-align:right;margin-top:4px;"
            >
              {{ m.read_by_doctor ? '✓ Прочитано врачом' : 'Отправлено' }}
            </div>
          </div>
        </div>
      </div>

      <form @submit.prevent="sendMessage" style="display:flex;gap:10px;align-items:flex-end;">
        <textarea
          v-model="draftBody"
          rows="2"
          class="input input-soft"
          style="flex:1;resize:vertical;"
          placeholder="Напишите ваш вопрос... (Enter — отправить, Shift+Enter — перенос строки)"
          @keydown.enter.exact.prevent="sendMessage"
        ></textarea>
        <button type="submit" class="btn" :disabled="sending || !draftBody.trim()">
          {{ sending ? 'Отправка...' : 'Отправить' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { apiFetch, formatDateTime } from '../api'

const emit = defineEmits(['chat-read'])

const messages = ref([])
const doctorName = ref('')
const draftBody = ref('')
const loading = ref(true)
const sending = ref(false)
const errorMsg = ref('')
const scrollBox = ref(null)
let pollTimer = null

const allClientMessagesRead = computed(() => {
  const clientMsgs = messages.value.filter((m) => m.sender_role === 'client')
  return clientMsgs.length > 0 && clientMsgs.every((m) => m.read_by_doctor)
})

function scrollToBottom() {
  nextTick(() => {
    if (scrollBox.value) {
      scrollBox.value.scrollTop = scrollBox.value.scrollHeight
    }
  })
}

async function loadMessages(silent = false) {
  if (!silent) errorMsg.value = ''
  const prevLen = messages.value.length
  try {
    const res = await apiFetch('/api/v1/cabinet/chat')
    messages.value = res.messages || []
    doctorName.value = res.doctor_name || ''
    emit('chat-read')
    if (messages.value.length !== prevLen || !silent) {
      scrollToBottom()
    }
  } catch (e) {
    if (!silent) errorMsg.value = e.message
  } finally {
    loading.value = false
  }
}

async function sendMessage() {
  const body = draftBody.value.trim()
  if (!body || sending.value) return
  sending.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/cabinet/chat', {
      method: 'POST',
      body: { body },
    })
    if (res?.message) {
      messages.value.push(res.message)
    }
    draftBody.value = ''
    scrollToBottom()
    emit('chat-read')
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    sending.value = false
  }
}

onMounted(() => {
  loadMessages(false)
  pollTimer = setInterval(() => loadMessages(true), 5000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>
