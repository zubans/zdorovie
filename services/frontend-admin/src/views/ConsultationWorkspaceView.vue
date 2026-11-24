<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
      <div>
        <router-link to="/consultations" class="muted" style="font-weight:600;">← Назад к расписанию</router-link>
        <h1 style="margin:4px 0 0;">Рабочее место врача · Консультация #{{ slotId }}</h1>
      </div>
      <div v-if="workspace?.summary">
        <span class="badge" :class="workspace.summary.status === 'approved' ? 'badge-green' : 'badge-peach'">
          {{ workspace.summary.status === 'approved' ? 'Утверждено и опубликовано ✓' : 'Черновик ИИ (на проверке)' }}
        </span>
      </div>
    </div>

    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <!-- Pre-Consultation AI Summary Card (Auto-built 30 minutes before slot + manual trigger) -->
    <div v-if="workspace && workspace.slot?.client_id" class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:10px;">
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px;flex-wrap:wrap;">
        <div>
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <h3 style="margin:0;">Предконсультационное ИИ-саммари профиля пациентки</h3>
            <span v-if="workspace.pre_consultation_summary" class="badge badge-green">
              Сформировано к приёму ✓
            </span>
            <span v-else class="badge badge-soft">
              Автозапуск за 30 мин до слота
            </span>
            <span v-if="workspace.unprocessed_events_count > 0" class="badge badge-peach">
              +{{ workspace.unprocessed_events_count }} новых материалов
            </span>
          </div>
          <p class="muted" style="margin:3px 0 0;font-size:0.78rem;">
            За 30 минут до начала встречи система автоматически обновляет саммари на основе предыдущего саммари и новых необработанных материалов пациентки.
          </p>
        </div>

        <div style="display:flex;gap:8px;flex-wrap:wrap;">
          <button
            type="button"
            class="btn btn-peach btn-sm"
            :disabled="preSumLoading"
            @click="triggerPreConsultSummary('pre_consultation')"
          >
            {{ preSumLoading ? 'Обновляем...' : 'Собрать к приёму (предыдущее + новые)' }}
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="preSumLoading"
            @click="triggerPreConsultSummary('full')"
          >
            Полный пересчёт с нуля
          </button>
          <button
            v-if="activePatientSummary"
            type="button"
            class="btn btn-secondary btn-sm"
            @click="showPreSummaryBody = !showPreSummaryBody"
          >
            {{ showPreSummaryBody ? 'Свернуть' : 'Развернуть сводку' }}
          </button>
        </div>
      </div>

      <div
        v-if="activePatientSummary && showPreSummaryBody"
        class="avo-flesh"
        style="padding:14px 18px;display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:10px;font-size:0.81rem;"
      >
        <div
          v-for="(sec, idx) in parsedPatientSummary"
          :key="idx"
          class="avo-card"
          style="margin:0;padding:12px 14px;border-radius:14px;"
        >
          <div v-if="sec.heading" style="font-weight:700;color:var(--primary-text);margin-bottom:4px;font-size:0.84rem;">
            {{ sec.heading }}
          </div>
          <p v-for="(p, pIdx) in sec.paragraphs" :key="'p-' + pIdx" style="margin:0 0 4px;">
            {{ p }}
          </p>
          <ul v-if="sec.items.length" style="margin:2px 0 0;padding-left:16px;">
            <li v-for="(it, iIdx) in sec.items" :key="'i-' + iIdx" style="margin-bottom:2px;">{{ it }}</li>
          </ul>
        </div>
      </div>
    </div>

    <div v-if="workspace" class="split-5-7">
      <!-- Left column (5 cols): Patient context, analyses, audio upload & Whisper transcript -->
      <div style="display:flex;flex-direction:column;gap:16px;">
        <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:10px;">
          <h3 style="margin:0;">Пациентка и контекст приёма</h3>
          <div class="avo-soft" style="display:grid;gap:6px;font-size:0.84rem;">
            <div>
              <b>Пациентка:</b>
              <router-link
                v-if="workspace.slot.client_id"
                :to="`/clients/${workspace.slot.client_id}`"
                style="font-weight:700;margin-left:4px;"
              >
                {{ workspace.slot.client_name }} (карточка и чат →)
              </router-link>
              <span v-else style="margin-left:4px;">{{ workspace.slot.client_name || 'Слот свободен' }}</span>
            </div>
            <div><b>Срок:</b> {{ workspace.slot.client_pregnancy_status || '—' }}</div>
            <div><b>Время:</b> {{ formatDateTime(workspace.slot.slot_start) }}</div>
            <div><b>Запрос до приёма:</b> {{ workspace.slot.client_question || 'Не указан' }}</div>
          </div>

          <div class="field" style="margin:0;">
            <label>Ссылка на видеовстречу (Телемост / VK)</label>
            <div style="display:flex;gap:8px;">
              <input v-model="workspace.slot.meeting_url" class="input input-soft" />
              <button class="btn btn-secondary btn-sm" @click="saveSlotMeta">Сохранить</button>
            </div>
          </div>

          <div class="field" style="margin:0;">
            <label>Приватные заметки врача (152-ФЗ, пациентка не видит)</label>
            <textarea
              v-model="workspace.slot.doctor_notes"
              rows="2"
              class="input input-soft"
              placeholder="Внутренние клинические пометки..."
            ></textarea>
            <button class="btn btn-secondary btn-sm" style="margin-top:6px;" @click="saveSlotMeta">
              Сохранить заметки
            </button>
          </div>

          <div v-if="workspace.attachments?.length">
            <div style="font-size:0.8rem;font-weight:700;margin-bottom:6px;">Прикреплённые анализы пациентки:</div>
            <div style="display:flex;gap:6px;flex-wrap:wrap;">
              <button
                v-for="att in workspace.attachments"
                :key="att.id"
                type="button"
                class="btn btn-secondary btn-sm"
                @click="downloadAttachment(att.id)"
              >
                {{ att.file_name }}
              </button>
            </div>
          </div>
        </div>

        <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:10px;">
          <h3 style="margin:0;">Аудиозапись приёма (Whisper STT)</h3>
          <p class="muted" style="margin:0;font-size:0.78rem;">
            MP3, M4A, WAV, AAC, OGG, WEBM до 500 МБ (потоковая загрузка чанками).
          </p>
          <input
            type="file"
            accept=".mp3,.m4a,.wav,.aac,.ogg,.webm,audio/*"
            :disabled="uploading"
            @change="uploadAudioChunked"
          />

          <div v-if="uploadProgress > 0">
            <div class="muted" style="font-size:0.78rem;">Загрузка: {{ uploadProgress }}%</div>
            <div style="height:8px;background:var(--surface-soft);border-radius:99px;overflow:hidden;margin-top:4px;">
              <div :style="{ width: uploadProgress + '%', height: '100%', background: 'var(--primary)' }"></div>
            </div>
          </div>

          <div v-if="latestRecord" class="avo-soft" style="display:flex;flex-direction:column;gap:8px;">
            <div style="display:flex;justify-content:space-between;align-items:center;font-size:0.82rem;">
              <b>Запись #{{ latestRecord.id }} ({{ latestRecord.audio_format }})</b>
              <span class="badge" :class="latestRecord.status === 'completed' ? 'badge-green' : 'badge-peach'">
                {{ latestRecord.status }}
              </span>
            </div>
            <div style="display:flex;gap:6px;flex-wrap:wrap;">
              <button class="btn btn-secondary btn-sm" @click="loadAudioPlayer">▶ Плеер (TTL 5 мин)</button>
              <button class="btn btn-secondary btn-sm" @click="retranscribe">↻ Перетранскрибировать</button>
            </div>
            <div v-if="audioUrl">
              <audio ref="audioEl" :src="audioUrl" controls style="width:100%;" @timeupdate="onAudioTime"></audio>
              <div style="display:flex;gap:6px;margin-top:6px;align-items:center;">
                <span class="muted" style="font-size:0.76rem;">Скорость:</span>
                <button v-for="rate in [1, 1.25, 1.5, 2]" :key="rate" class="btn btn-secondary btn-sm" @click="setRate(rate)">
                  {{ rate }}x
                </button>
              </div>
            </div>
          </div>
        </div>

        <div v-if="workspace.transcript?.length" class="avo-card" style="margin:0;">
          <h3 style="margin-bottom:10px;">Стенограмма Whisper (клик для перемотки)</h3>
          <div style="max-height:320px;overflow-y:auto;">
            <div
              v-for="seg in workspace.transcript"
              :key="seg.id"
              class="segment"
              :class="{ active: currentTime >= seg.start_time_sec && currentTime <= seg.end_time_sec }"
              @click="seekTo(seg.start_time_sec)"
            >
              <div class="muted" style="font-size:0.74rem;">
                <b>{{ formatSec(seg.start_time_sec) }}</b> · {{ seg.speaker_label }}
              </div>
              <div style="font-size:0.83rem;">{{ seg.text }}</div>
            </div>
          </div>
        </div>
      </div>

      <!-- Right column (7 cols): AI Template, Doctor Thesis Synthesis & HITL Approval -->
      <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:14px;">
        <div>
          <h3 style="margin:0;">Итоговое заключение и рекомендации (Human-in-the-Loop)</h3>
          <p class="muted" style="margin:3px 0 0;font-size:0.8rem;">
            Выберите подходящий клинический шаблон или обработайте тезисы приёма с помощью ИИ — пациентка увидит выписку только после вашего утверждения.
          </p>
        </div>

        <div v-if="!workspace.slot.client_id" class="alert alert-warn" style="margin:0;">
          Слот пока свободен (пациентка не записана). Публикация PDF станет доступна после бронирования слота.
        </div>

        <!-- AI Recommended Template Banner -->
        <div
          v-if="recommendedTemplateObj"
          class="avo-flesh"
          style="padding:12px 16px;display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap;"
        >
          <div style="font-size:0.84rem;">
            <b>ИИ рекомендует шаблон:</b> «{{ recommendedTemplateObj.title }}»
            <div v-if="workspace.recommended_reason" class="muted" style="font-size:0.76rem;margin-top:2px;">
              {{ workspace.recommended_reason }}
            </div>
          </div>
          <button
            v-if="draft.template_code !== recommendedTemplateObj.code"
            type="button"
            class="btn btn-sm"
            @click="selectTemplate(recommendedTemplateObj.code, true)"
          >
            Применить
          </button>
          <span v-else class="badge badge-green">Выбран ✓</span>
        </div>

        <div class="field" style="margin:0;">
          <label>Клинический шаблон заключения</label>
          <div style="display:flex;gap:8px;flex-wrap:wrap;">
            <select v-model="draft.template_code" class="input input-soft" style="flex:1;min-width:220px;">
              <option value="">— Без шаблона (индивидуально) —</option>
              <option
                v-for="tpl in workspace.templates || []"
                :key="tpl.code"
                :value="tpl.code"
              >
                {{ tpl.code === workspace.recommended_template_code ? '★ ' : '' }}{{ tpl.title }} ({{ tpl.category }})
              </option>
            </select>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="!draft.template_code"
              @click="applyTemplateFields(draft.template_code, true)"
            >
              Подставить в поля
            </button>
          </div>
        </div>

        <!-- Doctor's free-form description + AI synthesis button -->
        <div class="avo-soft" style="display:flex;flex-direction:column;gap:8px;">
          <div style="display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap;">
            <label style="font-size:0.8rem;font-weight:700;margin:0;">
              Описание от врача (свободные тезисы приёма для ИИ)
            </label>
            <button
              type="button"
              class="btn btn-peach btn-sm"
              :disabled="aiDraftLoading || !workspace.slot.client_id"
              @click="generateWithAI"
            >
              {{ aiDraftLoading ? 'Обработка ИИ...' : 'Обработать ИИ на основе описания' }}
            </button>
          </div>
          <textarea
            v-model="draft.doctor_description"
            rows="3"
            class="input"
            placeholder="Кратко запишите жалобы, показатели (ферритин, АД, ТТГ) и назначения — нажмите кнопку справа, и ИИ разложит их по разделам выписки..."
          ></textarea>
        </div>

        <div class="grid-2" style="gap:12px;">
          <div class="field" style="margin:0;">
            <label>Жалобы и запрос</label>
            <textarea v-model="draft.complaints" rows="2"></textarea>
          </div>
          <div class="field" style="margin:0;">
            <label>Анамнез и клиническая оценка</label>
            <textarea v-model="draft.anamnesis" rows="2"></textarea>
          </div>
        </div>

        <div class="field" style="margin:0;">
          <label>Индивидуальные рекомендации (основной блок выписки)</label>
          <textarea v-model="draft.recommendations" rows="5"></textarea>
        </div>

        <div class="grid-2" style="gap:12px;">
          <div class="field" style="margin:0;">
            <label>План дообследования и анализов</label>
            <textarea v-model="draft.diagnostics_plan" rows="2"></textarea>
          </div>
          <div class="field" style="margin:0;">
            <label>Контроль и дальнейшие шаги</label>
            <textarea v-model="draft.follow_up" rows="2"></textarea>
          </div>
        </div>

        <div class="field" style="margin:0;">
          <label>Комментарий врача к проверке (внутренний HITL)</label>
          <input v-model="draft.doctor_feedback" class="input input-soft" placeholder="Пометка для внутренней истории" />
        </div>

        <div style="display:flex;gap:10px;flex-wrap:wrap;padding-top:6px;border-top:1px solid var(--border);">
          <button class="btn btn-secondary" :disabled="saving || !workspace.slot.client_id" @click="saveDraft">
            Сохранить черновик
          </button>
          <button class="btn" :disabled="saving || !workspace.slot.client_id" @click="approveSummary">
            Утвердить и опубликовать PDF пациентке ✓
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { apiFetch, formatDateTime } from '../api'
import { parseSummarySections } from '../utils/formatSummary'

const route = useRoute()
const slotId = route.params.id

const workspace = ref(null)
const draft = ref({
  template_code: '',
  doctor_description: '',
  complaints: '',
  anamnesis: '',
  recommendations: '',
  diagnostics_plan: '',
  follow_up: '',
  doctor_feedback: '',
})
const uploadProgress = ref(0)
const uploading = ref(false)
const aiDraftLoading = ref(false)
const preSumLoading = ref(false)
const showPreSummaryBody = ref(true)
const saving = ref(false)
const audioUrl = ref('')
const audioEl = ref(null)
const currentTime = ref(0)
const notice = ref('')
const errorMsg = ref('')
let pollTimer = null

const latestRecord = computed(() => workspace.value?.records?.[0] || null)
const recommendedTemplateObj = computed(() => {
  const code = workspace.value?.recommended_template_code
  if (!code || !workspace.value?.templates) return null
  return workspace.value.templates.find((t) => t.code === code) || null
})

const activePatientSummary = computed(
  () => workspace.value?.pre_consultation_summary || workspace.value?.patient_latest_summary || null
)

const parsedPatientSummary = computed(() =>
  parseSummarySections(activePatientSummary.value?.summary_text || '')
)

async function triggerPreConsultSummary(mode) {
  const clientId = workspace.value?.slot?.client_id
  if (!clientId) return
  preSumLoading.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch(`/api/v1/admin/clients/${clientId}/summary`, {
      method: 'POST',
      body: {
        mode,
        slot_id: Number(slotId),
        use_previous_summary: mode !== 'full',
      },
    })
    if (res.saved_summary) {
      workspace.value.pre_consultation_summary = res.saved_summary
      workspace.value.patient_latest_summary = res.saved_summary
    }
    workspace.value.unprocessed_events_count = res.unprocessed_events_count ?? 0
    showPreSummaryBody.value = true
    notice.value =
      mode === 'full'
        ? 'Выполнен полный пересчёт саммари пациентки с нуля по всем материалам.'
        : 'Предконсультационное саммари обновлено (предыдущее саммари + новые материалы).'
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    preSumLoading.value = false
  }
}

function syncDraftFromWorkspace(ws) {
  const sum = ws?.summary
  if (sum) {
    draft.value = {
      template_code: sum.template_code || ws.recommended_template_code || '',
      doctor_description: sum.doctor_description || '',
      complaints: sum.complaints || '',
      anamnesis: sum.anamnesis || '',
      recommendations: sum.recommendations || '',
      diagnostics_plan: sum.diagnostics_plan || '',
      follow_up: sum.follow_up || '',
      doctor_feedback: sum.doctor_feedback || '',
    }
  } else {
    draft.value = {
      template_code: ws?.recommended_template_code || '',
      doctor_description: draft.value.doctor_description || '',
      complaints: draft.value.complaints || '',
      anamnesis: draft.value.anamnesis || '',
      recommendations: draft.value.recommendations || '',
      diagnostics_plan: draft.value.diagnostics_plan || '',
      follow_up: draft.value.follow_up || '',
      doctor_feedback: draft.value.doctor_feedback || '',
    }
  }
}

function selectTemplate(code, fillEmptyOnly = true) {
  draft.value.template_code = code
  applyTemplateFields(code, !fillEmptyOnly)
}

function applyTemplateFields(code, overwrite = true) {
  const tpl = (workspace.value?.templates || []).find((t) => t.code === code)
  if (!tpl) return
  if (overwrite || !draft.value.complaints) draft.value.complaints = tpl.complaints_tpl || ''
  if (overwrite || !draft.value.anamnesis) draft.value.anamnesis = tpl.anamnesis_tpl || ''
  if (overwrite || !draft.value.recommendations) draft.value.recommendations = tpl.recommendations_tpl || ''
  if (overwrite || !draft.value.diagnostics_plan) draft.value.diagnostics_plan = tpl.diagnostics_tpl || ''
  if (overwrite || !draft.value.follow_up) draft.value.follow_up = tpl.follow_up_tpl || ''
  notice.value = `Шаблон «${tpl.title}» подставлен в поля заключения.`
}

async function generateWithAI() {
  aiDraftLoading.value = true
  errorMsg.value = ''
  notice.value = ''
  try {
    const res = await apiFetch(`/api/v1/consultations/slots/${slotId}/summary/ai-draft`, {
      method: 'POST',
      body: {
        template_code: draft.value.template_code,
        doctor_description: draft.value.doctor_description,
        doctor_feedback: draft.value.doctor_feedback,
      },
    })
    workspace.value.summary = res.summary
    syncDraftFromWorkspace(workspace.value)
    notice.value = 'ИИ-черновик сформирован по вашему описанию. Проверьте разделы и нажмите «Утвердить».'
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    aiDraftLoading.value = false
  }
}

async function loadWorkspace() {
  try {
    const wasEmpty = !workspace.value
    const hadSummary = Boolean(workspace.value?.summary?.id)
    const ws = await apiFetch(`/api/v1/consultations/slots/${slotId}/workspace`)
    workspace.value = ws
    if (wasEmpty || (!hadSummary && ws?.summary?.id)) {
      syncDraftFromWorkspace(ws)
    }
    const st = ws?.records?.[0]?.status
    const processing = st && ['uploaded', 'transcribing', 'summarizing'].includes(st)
    if (processing && !pollTimer) {
      pollTimer = setInterval(loadWorkspace, 3000)
    } else if (!processing && pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function saveSlotMeta() {
  try {
    await apiFetch(`/api/v1/admin/consultations/slots/${slotId}`, {
      method: 'PATCH',
      body: {
        meeting_url: workspace.value.slot.meeting_url,
        doctor_notes: workspace.value.slot.doctor_notes,
      },
    })
    notice.value = 'Данные слота сохранены.'
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function downloadAttachment(id) {
  try {
    const res = await apiFetch(`/api/v1/admin/consultations/attachments/${id}/download?format=json`)
    if (res.url) window.open(res.url, '_blank')
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function uploadAudioChunked(ev) {
  const file = ev.target.files?.[0]
  if (!file) return
  uploading.value = true
  uploadProgress.value = 1
  errorMsg.value = ''
  notice.value = ''

  try {
    const ext = file.name.split('.').pop()?.toLowerCase() || 'wav'
    const init = await apiFetch('/api/v1/consultations/uploads/init', {
      method: 'POST',
      body: { slot_id: Number(slotId), file_name: file.name, audio_format: ext, source_type: 'manual_upload' },
    })

    const chunkSize = 5 * 1024 * 1024
    const totalChunks = Math.max(1, Math.ceil(file.size / chunkSize))
    for (let i = 0; i < totalChunks; i++) {
      const slice = file.slice(i * chunkSize, (i + 1) * chunkSize)
      await apiFetch(`/api/v1/consultations/uploads/${init.upload_id}/chunk/${i}`, {
        method: 'PUT',
        body: slice,
        headers: { 'Content-Type': 'application/octet-stream' },
      })
      uploadProgress.value = Math.round(((i + 1) / totalChunks) * 95)
    }

    await apiFetch(`/api/v1/consultations/uploads/${init.upload_id}/complete`, { method: 'POST' })
    uploadProgress.value = 100
    notice.value = 'Аудиозапись загружена и отправлена в ИИ-обработку!'
    await loadWorkspace()
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    uploading.value = false
    ev.target.value = ''
  }
}

async function loadAudioPlayer() {
  if (!latestRecord.value) return
  try {
    const res = await apiFetch(`/api/v1/consultations/records/${latestRecord.value.id}/audio-url`)
    audioUrl.value = res.url
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function retranscribe() {
  if (!latestRecord.value) return
  try {
    await apiFetch(`/api/v1/consultations/records/${latestRecord.value.id}/retranscribe`, { method: 'POST' })
    notice.value = 'Перезапуск транскрибации запущен.'
    await loadWorkspace()
  } catch (e) {
    errorMsg.value = e.message
  }
}

function onAudioTime() {
  if (audioEl.value) currentTime.value = audioEl.value.currentTime
}

function seekTo(sec) {
  if (audioEl.value) {
    audioEl.value.currentTime = sec
    audioEl.value.play().catch(() => {})
  }
}

function setRate(rate) {
  if (audioEl.value) audioEl.value.playbackRate = rate
}

function formatSec(sec) {
  const s = Math.floor(sec || 0)
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
}

async function saveDraft() {
  saving.value = true
  errorMsg.value = ''
  notice.value = ''
  try {
    let res
    if (workspace.value?.summary?.id) {
      res = await apiFetch(`/api/v1/consultations/summaries/${workspace.value.summary.id}`, {
        method: 'PUT',
        body: draft.value,
      })
    } else {
      res = await apiFetch(`/api/v1/consultations/slots/${slotId}/summary`, {
        method: 'PUT',
        body: draft.value,
      })
    }
    if (res?.summary) {
      workspace.value.summary = res.summary
    }
    notice.value = 'Черновик заключения сохранён.'
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    saving.value = false
  }
}

async function approveSummary() {
  saving.value = true
  errorMsg.value = ''
  notice.value = ''
  try {
    let summaryId = workspace.value?.summary?.id
    if (!summaryId) {
      const created = await apiFetch(`/api/v1/consultations/slots/${slotId}/summary`, {
        method: 'PUT',
        body: draft.value,
      })
      workspace.value.summary = created.summary
      summaryId = created.summary?.id
    }
    await apiFetch(`/api/v1/consultations/summaries/${summaryId}/approve`, {
      method: 'POST',
      body: draft.value,
    })
    notice.value = 'Заключение утверждено, PDF сформирован и опубликован пациентке!'
    await loadWorkspace()
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    saving.value = false
  }
}

onMounted(loadWorkspace)
onUnmounted(() => { if (pollTimer) clearInterval(pollTimer) })
</script>
