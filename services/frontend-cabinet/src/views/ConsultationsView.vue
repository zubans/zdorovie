<template>
  <div style="display:flex;flex-direction:column;gap:18px;">
    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <div v-if="state && !state.sub_active" class="alert alert-warn" style="margin:0;">
      <b>Подписка не активна.</b> Продлите доступ в разделе <router-link to="/profile">профиля</router-link> для записи к врачу.
    </div>

    <div v-else-if="state && !state.has_feedback" class="alert alert-warn" style="margin:0;">
      Индивидуальные онлайн-консультации включены в тариф <b>«С обратной связью»</b>.
      Вы можете перейти на него в разделе <router-link to="/profile">Профиль и подписка</router-link>.
    </div>

    <!-- Alternative consultation modes if configured -->
    <div v-if="config && config.mode !== 'builtin_slots'" class="avo-card" style="margin:0;">
      <h3 style="margin-bottom:6px;">Формат связи: {{ modeTitle }}</h3>
      <p v-if="config.mode === 'direct_chat'" class="muted" style="margin:0 0 12px;">
        Врач консультирует в личном чате MAX. Нажмите кнопку ниже, чтобы написать вопрос и прикрепить анализы.
      </p>
      <a v-if="config.mode === 'direct_chat' && config.direct_chat_url" :href="config.direct_chat_url" target="_blank" class="btn">
        Перейти в чат с врачом в MAX
      </a>

      <p v-if="config.mode === 'external_widget'" class="muted" style="margin:0 0 12px;">
        Для выбора удобного времени используйте внешний календарь врача:
      </p>
      <a v-if="config.mode === 'external_widget' && config.external_widget_url" :href="config.external_widget_url" target="_blank" class="btn">
        Открыть календарь записи
      </a>
    </div>

    <!-- Desktop 2-Column Split: Left = My Bookings / Preparation, Right = Available Windows by Day -->
    <div class="split-6-6">
      <!-- LEFT 6 COLS: Booked consultations & preparation -->
      <div style="display:flex;flex-direction:column;gap:16px;">
        <template v-if="myBookedSlots.length > 0">
          <div
            v-for="sl in myBookedSlots"
            :key="sl.id"
            class="avo-card"
            style="margin:0;display:flex;flex-direction:column;gap:14px;"
          >
            <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:10px;flex-wrap:wrap;">
              <div>
                <h3 style="margin:0;">Ваша запись · {{ formatDateTime(sl.slot_start) }}</h3>
                <p class="muted" style="margin:3px 0 0;font-size:0.8rem;">
                  {{ sl.doctor_name || config?.doctor_name || 'Врач-терапевт, нутрициолог' }} · перед звонком можно описать самочувствие и прикрепить анализы
                </p>
              </div>
              <span class="badge" :class="sl.status === 'completed' ? 'badge-green' : 'badge-peach'">
                {{ sl.status === 'booked' ? 'Подтверждена' : sl.status === 'completed' ? 'Завершена' : sl.status }}
              </span>
            </div>

            <div>
              <label style="display:block;font-size:0.8rem;font-weight:600;margin-bottom:6px;">Ваш запрос врачу</label>
              <textarea
                v-model="sl.client_question"
                rows="2"
                class="input input-soft"
                placeholder="Коротко опишите вопросы или показатели анализов..."
              ></textarea>
              <div style="margin-top:8px;">
                <button type="button" class="btn btn-secondary btn-sm" @click="saveQuestion(sl)">
                  Сохранить запрос
                </button>
              </div>
            </div>

            <div class="avo-soft" style="display:flex;flex-direction:column;gap:8px;">
              <div style="font-size:0.8rem;font-weight:600;">Прикреплённые анализы (PDF, JPG, PNG до 20 МБ)</div>
              <div v-if="attachmentsBySlot[sl.id]?.length" style="display:flex;flex-direction:column;gap:6px;">
                <div
                  v-for="a in attachmentsBySlot[sl.id]"
                  :key="a.id"
                  style="background:#ffffff;border-radius:12px;padding:8px 12px;display:flex;justify-content:space-between;align-items:center;font-size:0.8rem;border:1px solid rgba(0,0,0,0.05);"
                >
                  <span>{{ a.file_name }} ({{ Math.round(a.file_size_bytes / 1024) }} КБ)</span>
                  <span class="muted">Загружено ✓</span>
                </div>
              </div>
              <div v-else class="muted" style="font-size:0.78rem;">Файлы пока не прикреплены.</div>

              <div>
                <label
                  class="btn btn-card btn-sm"
                  style="cursor:pointer;display:inline-flex;margin-top:2px;"
                >
                  <span>+ Прикрепить файл</span>
                  <input
                    type="file"
                    accept=".pdf,.jpg,.jpeg,.png,.webp"
                    style="display:none;"
                    @change="uploadAttachment(sl.id, $event)"
                  />
                </label>
              </div>
            </div>

            <div style="display:flex;gap:10px;flex-wrap:wrap;">
              <a
                v-if="sl.meeting_url && sl.status === 'booked'"
                :href="sl.meeting_url"
                target="_blank"
                class="btn"
              >
                Войти на видеовстречу
              </a>
              <button
                v-if="sl.status === 'booked'"
                type="button"
                class="btn btn-secondary"
                @click="cancelSlot(sl.id)"
              >
                Перенести / отменить
              </button>
            </div>
          </div>
        </template>

        <div v-else class="avo-card" style="margin:0;">
          <h3 style="margin-bottom:6px;">Ваша запись к врачу</h3>
          <p class="muted" style="margin:0 0 12px;font-size:0.86rem;">
            У вас пока нет активной брони. Выберите удобный день и время в расписании справа — после записи здесь появится карточка для загрузки анализов и ссылка на видеозвонок.
          </p>
          <div class="avo-soft" style="font-size:0.8rem;">
            <b>Как подготовиться ко встрече:</b>
            <div class="muted" style="margin-top:4px;">
              Сформулируйте 2–3 главных вопроса по самочувствию и при наличии прикрепите свежий ОАК, ферритин или УЗИ.
            </div>
          </div>
        </div>
      </div>

      <!-- RIGHT 6 COLS: Available slots grouped by day -->
      <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:14px;">
        <div>
          <h3 style="margin:0;">{{ myBookedSlots.length > 0 ? 'Выбрать другое окно записи' : 'Свободные окна для записи' }}</h3>
          <p class="muted" style="margin:3px 0 0;font-size:0.8rem;">
            Нажмите на удобное время для мгновенной записи к врачу
          </p>
        </div>

        <div>
          <label style="display:block;font-size:0.78rem;font-weight:600;margin-bottom:5px;">
            Комментарий или тема встречи (по желанию)
          </label>
          <input
            v-model="bookingQuestion"
            type="text"
            class="input input-soft"
            placeholder="Например: разбор ферритина и лёгкая усталость во II триместре"
          />
        </div>

        <div v-if="!state?.sub_active || !state?.has_feedback" class="avo-soft muted" style="font-size:0.82rem;">
          Запись на слоты доступна на активном тарифе «С обратной связью».
        </div>
        <div v-else-if="groupedAvailableSlots.length === 0" class="avo-soft muted" style="font-size:0.82rem;">
          Свободных окон пока нет — врач скоро откроет новые слоты в расписании.
        </div>
        <div v-else style="display:flex;flex-direction:column;gap:10px;">
          <div
            v-for="group in groupedAvailableSlots"
            :key="group.dateKey"
            class="avo-soft"
          >
            <div style="font-size:0.84rem;font-weight:700;margin-bottom:8px;text-transform:capitalize;">
              {{ group.dateLabel }}
            </div>
            <div style="display:flex;gap:8px;flex-wrap:wrap;">
              <button
                v-for="sl in group.slots"
                :key="sl.id"
                type="button"
                class="btn btn-card btn-sm"
                @click="bookSlot(sl.id)"
              >
                <span>{{ formatClock(sl.slot_start) }}</span>
                <span class="muted" style="font-size:0.74rem;">· выбрать</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { apiFetch, formatDateTime } from '../api'

const state = ref(null)
const notice = ref('')
const errorMsg = ref('')
const bookingQuestion = ref('')
const attachmentsBySlot = ref({})

const config = computed(() => state.value?.consultation_config)
const myBookedSlots = computed(() => (state.value?.slots || []).filter((s) => s.client_id && s.status !== 'available'))
const availableSlots = computed(() => (state.value?.slots || []).filter((s) => s.status === 'available'))

const groupedAvailableSlots = computed(() => {
  const groups = new Map()
  for (const sl of availableSlots.value) {
    const dt = new Date(sl.slot_start)
    const dateKey = dt.toISOString().slice(0, 10)
    if (!groups.has(dateKey)) {
      groups.set(dateKey, {
        dateKey,
        dateLabel: dt.toLocaleDateString('ru-RU', { weekday: 'long', day: 'numeric', month: 'long' }),
        slots: [],
      })
    }
    groups.get(dateKey).slots.push(sl)
  }
  return Array.from(groups.values())
})

const modeTitle = computed(() => {
  const m = config.value?.mode
  if (m === 'direct_chat') return 'Прямой чат с врачом в MAX'
  if (m === 'external_widget') return 'Внешний виджет бронирования'
  return 'Встроенное расписание онлайн-консультаций'
})

function formatClock(iso) {
  return new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

async function loadData() {
  try {
    state.value = await apiFetch('/api/v1/cabinet/consultations/slots')
    for (const sl of myBookedSlots.value) {
      const res = await apiFetch(`/api/v1/cabinet/consultations/slots/${sl.id}/attachments`)
      attachmentsBySlot.value[sl.id] = res.attachments || []
    }
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function bookSlot(id) {
  errorMsg.value = ''
  notice.value = ''
  try {
    await apiFetch(`/api/v1/cabinet/consultations/slots/${id}/book`, {
      method: 'POST',
      body: { question: bookingQuestion.value },
    })
    notice.value = 'Вы успешно записались на консультацию!'
    bookingQuestion.value = ''
    await loadData()
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function cancelSlot(id) {
  errorMsg.value = ''
  try {
    await apiFetch(`/api/v1/cabinet/consultations/slots/${id}/cancel`, { method: 'POST' })
    notice.value = 'Запись отменена, окно освобождено.'
    await loadData()
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function saveQuestion(sl) {
  try {
    await apiFetch(`/api/v1/cabinet/consultations/slots/${sl.id}/question`, {
      method: 'PATCH',
      body: { question: sl.client_question },
    })
    notice.value = 'Ваш запрос к консультации сохранён.'
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function uploadAttachment(slotId, ev) {
  const file = ev.target.files?.[0]
  if (!file) return
  const fd = new FormData()
  fd.append('file', file)
  try {
    await apiFetch(`/api/v1/cabinet/consultations/slots/${slotId}/attachments`, { method: 'POST', body: fd })
    notice.value = `Файл ${file.name} прикреплён.`
    await loadData()
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    ev.target.value = ''
  }
}

onMounted(loadData)
</script>
