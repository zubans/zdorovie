<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:12px;">
      <div>
        <h1 style="margin:0;">Консультации и расписание</h1>
        <p class="muted" style="margin:3px 0 0;">
          Управление слотами приёма и проверка ИИ-заключений (Human-in-the-Loop)
        </p>
      </div>

      <div class="tab-pill-group">
        <button
          type="button"
          class="tab-pill-btn"
          :class="{ active: activeTab === 'schedule' }"
          @click="activeTab = 'schedule'"
        >
          Приёмы и слоты
        </button>
        <button
          type="button"
          class="tab-pill-btn"
          :class="{ active: activeTab === 'settings' }"
          @click="activeTab = 'settings'"
        >
          Настройки видео и MAX
        </button>
      </div>
    </div>

    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <!-- TAB 1: SCHEDULE, CALENDAR & HITL DRAFTS -->
    <template v-if="activeTab === 'schedule'">
      <!-- Highlight Banner for AI Drafts awaiting doctor review -->
      <div v-if="draftSlots.length > 0" class="avo-flesh" style="display:flex;flex-direction:column;gap:10px;">
        <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;">
          <div style="font-weight:700;font-size:0.95rem;">
            На проверке у врача (Черновики ИИ · {{ draftSlots.length }})
          </div>
          <span class="badge badge-green">Требуется утверждение (HITL)</span>
        </div>

        <div style="display:flex;flex-direction:column;gap:8px;">
          <div
            v-for="ds in draftSlots"
            :key="'draft-' + ds.id"
            class="avo-card"
            style="margin:0;padding:12px 16px;border-radius:16px;display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;"
          >
            <div style="font-size:0.85rem;">
              <b>{{ formatDateTime(ds.slot_start) }} · {{ ds.client_name || 'Пациентка' }}</b>
              <span class="muted"> — ИИ подготовил проект заключения по материалам приёма</span>
            </div>
            <router-link :to="`/consultations/${ds.id}`" class="btn btn-sm">
              Проверить и утвердить →
            </router-link>
          </div>
        </div>
      </div>

      <!-- Desktop Split: Left (5 cols) = Calendar Slot Creator, Right (7 cols) = Slots Table -->
      <div class="split-5-7">
        <!-- LEFT 5 COLS: Interactive Calendar & Conflict-Protected Slot Creator -->
        <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:12px;">
          <h3 style="margin:0;">Добавить слот в расписание врача</h3>

          <div style="display:flex;justify-content:space-between;align-items:center;">
            <button type="button" class="btn btn-secondary btn-sm" @click="shiftMonth(-1)">◀</button>
            <b style="text-transform:capitalize;font-size:0.9rem;">{{ calendarMonthLabel }}</b>
            <button type="button" class="btn btn-secondary btn-sm" @click="shiftMonth(1)">▶</button>
          </div>

          <div style="display:grid;grid-template-columns:repeat(7,1fr);gap:4px;text-align:center;">
            <div v-for="wd in ['Пн','Вт','Ср','Чт','Пт','Сб','Вс']" :key="wd" class="muted" style="font-size:0.73rem;font-weight:700;">
              {{ wd }}
            </div>
            <button
              v-for="cell in calendarCells"
              :key="cell.key"
              type="button"
              :disabled="!cell.inMonth"
              @click="selectCalendarDate(cell.dateStr)"
              :style="{
                padding: '6px 2px',
                borderRadius: '10px',
                border: cell.dateStr === newSlot.dateStr ? '2px solid var(--primary)' : '1px solid var(--border)',
                background: !cell.inMonth ? 'transparent' : cell.dateStr === newSlot.dateStr ? 'var(--primary-soft)' : '#ffffff',
                opacity: cell.inMonth ? 1 : 0.25,
                cursor: cell.inMonth ? 'pointer' : 'default',
                fontSize: '0.8rem',
                fontFamily: 'inherit',
              }"
            >
              <div style="font-weight:600;">{{ cell.day }}</div>
              <div v-if="cell.slotCount > 0" style="font-size:0.66rem;color:var(--primary);font-weight:700;">
                • {{ cell.slotCount }}
              </div>
            </button>
          </div>

          <!-- Busy timeline of selected day -->
          <div class="avo-soft" style="padding:10px 12px;">
            <div style="font-size:0.78rem;font-weight:700;margin-bottom:4px;">
              Занятость на {{ newSlot.dateStr }}:
            </div>
            <div v-if="slotsForSelectedDate.length === 0" class="muted" style="font-size:0.78rem;">
              Слотов на эту дату ещё нет — день полностью свободен.
            </div>
            <div v-else style="display:flex;gap:6px;flex-wrap:wrap;">
              <span
                v-for="ex in slotsForSelectedDate"
                :key="ex.id"
                class="badge"
                :class="ex.status === 'booked' ? 'badge-peach' : ex.status === 'completed' ? 'badge-green' : 'badge-soft'"
              >
                {{ formatTimeRange(ex.slot_start, ex.slot_end) }} ({{ ex.client_name || 'Свободен' }})
              </span>
            </div>
          </div>

          <div class="field" style="margin:0;">
            <label>Быстрый выбор времени ({{ newSlot.dateStr }})</label>
            <div style="display:flex;gap:5px;flex-wrap:wrap;margin-bottom:8px;">
              <button
                v-for="tOpt in quickTimeOptions"
                :key="tOpt.time"
                type="button"
                class="btn btn-sm"
                :class="tOpt.conflict ? 'btn-secondary' : newSlot.timeStr === tOpt.time ? '' : 'btn-secondary'"
                :disabled="Boolean(tOpt.conflict)"
                :style="tOpt.conflict ? 'opacity:0.4;text-decoration:line-through;' : ''"
                :title="tOpt.conflict ? `Пересекается со слотом #${tOpt.conflict.id}` : 'Выбрать время'"
                @click="newSlot.timeStr = tOpt.time"
              >
                {{ tOpt.time }}
              </button>
            </div>

            <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;">
              <input v-model="newSlot.dateStr" type="date" class="input input-soft" style="max-width:145px;" />
              <input v-model="newSlot.timeStr" type="time" class="input input-soft" style="max-width:115px;" />
              <div style="display:flex;gap:4px;">
                <button
                  v-for="dur in [30, 45, 60, 90]"
                  :key="dur"
                  type="button"
                  class="btn btn-sm"
                  :class="newSlot.durationMin === dur ? '' : 'btn-secondary'"
                  @click="newSlot.durationMin = dur"
                >
                  {{ dur }}м
                </button>
              </div>
            </div>
          </div>

          <div v-if="slotConflict" class="alert alert-error" style="margin:0;padding:8px 12px;font-size:0.8rem;">
            Пересечение с существующим слотом #{{ slotConflict.id }} ({{ formatTimeRange(slotConflict.slot_start, slotConflict.slot_end) }}). Выберите другое время.
          </div>

          <div class="field" style="margin:0;">
            <label>Ссылка на видеовстречу (Телемост / VK Звонки)</label>
            <input v-model="newSlot.meeting_url" class="input input-soft" placeholder="https://telemost.yandex.ru/j/..." />
          </div>

          <button class="btn" :disabled="Boolean(slotConflict) || !newSlot.dateStr || !newSlot.timeStr" @click="createSlot">
            Открыть слот для записи
          </button>
        </div>

        <!-- RIGHT 7 COLS: Filtered Slots List -->
        <div class="avo-card" style="margin:0;overflow-x:auto;">
          <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;margin-bottom:14px;">
            <h3 style="margin:0;">Расписание слотов</h3>
            <div style="display:flex;gap:6px;flex-wrap:wrap;">
              <button class="btn btn-sm" :class="slotFilter === 'all' ? '' : 'btn-secondary'" @click="setFilter('all')">
                Все ({{ slots.length }})
              </button>
              <button class="btn btn-sm" :class="slotFilter === 'draft' ? '' : 'btn-secondary'" @click="setFilter('draft')">
                Черновики на проверку ({{ draftSlotsCount }})
              </button>
              <button class="btn btn-sm" :class="slotFilter === 'booked' ? '' : 'btn-secondary'" @click="setFilter('booked')">
                Запланированы
              </button>
              <button class="btn btn-sm" :class="slotFilter === 'available' ? '' : 'btn-secondary'" @click="setFilter('available')">
                Свободные
              </button>
            </div>
          </div>

          <table>
            <thead>
              <tr>
                <th>Дата и время</th>
                <th>Пациентка</th>
                <th>Статус</th>
                <th>Аудио / ИИ</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="filteredSlots.length === 0">
                <td colspan="5" class="muted" style="text-align:center;padding:18px;">
                  По выбранному фильтру слотов нет.
                </td>
              </tr>
              <tr v-for="sl in filteredSlots" :key="sl.id">
                <td>
                  <b>{{ formatDateTime(sl.slot_start) }}</b>
                  <div class="muted" style="font-size:0.75rem;">до {{ formatClock(sl.slot_end) }}</div>
                </td>
                <td>
                  <template v-if="sl.client_name">
                    <router-link
                      v-if="sl.client_id"
                      :to="`/clients/${sl.client_id}`"
                      style="font-weight:700;color:var(--text);"
                    >
                      {{ sl.client_name }}
                    </router-link>
                    <b v-else>{{ sl.client_name }}</b>
                    <div class="muted" style="font-size:0.75rem;">{{ sl.client_pregnancy_status }}</div>
                  </template>
                  <span v-else class="muted">Свободное окно</span>
                </td>
                <td>
                  <span
                    class="badge"
                    :class="sl.status === 'booked' ? 'badge-peach' : sl.status === 'completed' ? 'badge-green' : 'badge-soft'"
                  >
                    {{ sl.status === 'booked' ? 'Запись' : sl.status === 'completed' ? 'Завершён' : 'Свободен' }}
                  </span>
                </td>
                <td>
                  <span
                    class="badge"
                    :class="sl.summary_status === 'approved' ? 'badge-green' : sl.summary_status === 'draft' ? 'badge-peach' : 'badge-soft'"
                  >
                    {{
                      sl.summary_status === 'draft'
                        ? 'Черновик (ждет врача)'
                        : sl.summary_status === 'approved'
                        ? 'Утверждено ✓'
                        : sl.record_status || '—'
                    }}
                  </span>
                </td>
                <td>
                  <div style="display:flex;gap:6px;justify-content:flex-end;">
                    <router-link
                      :to="`/consultations/${sl.id}`"
                      class="btn btn-sm"
                      :class="sl.summary_status === 'draft' ? 'btn-peach' : ''"
                    >
                      {{ sl.summary_status === 'draft' ? 'Проверить →' : 'Рабочее место' }}
                    </router-link>
                    <button
                      v-if="sl.status === 'available'"
                      type="button"
                      class="btn btn-secondary btn-sm"
                      @click="deleteSlot(sl.id)"
                    >
                      Удалить
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- TAB 2: CONSULTATION & MAX CHANNEL SETTINGS -->
    <template v-else>
      <div class="avo-card" style="margin:0;max-width:680px;">
        <h3 style="margin-bottom:12px;">Настройки модуля консультаций и ссылок MAX</h3>
        <div class="field">
          <label>Режим записи пациенток</label>
          <select v-model="settings.mode" class="input input-soft">
            <option value="builtin_slots">Встроенное расписание и слоты (Телемост / VK)</option>
            <option value="direct_chat">Личный чат врача в MAX</option>
            <option value="external_widget">Внешняя ссылка / виджет записи</option>
          </select>
        </div>
        <div class="field">
          <label>Имя и специализация врача</label>
          <input v-model="settings.doctor_name" class="input input-soft" />
        </div>
        <div class="field">
          <label>Ссылка на личный чат в MAX</label>
          <input v-model="settings.direct_chat_url" class="input input-soft" />
        </div>
        <div class="field">
          <label>Ссылка на внешний календарь (если выбран внешний виджет)</label>
          <input v-model="settings.external_widget_url" class="input input-soft" />
        </div>
        <div class="field">
          <label>Invite-ссылка закрытого канала курса в MAX</label>
          <input v-model="settings.max_channel_invite_url" class="input input-soft" />
        </div>
        <button class="btn" @click="saveSettings">Сохранить настройки</button>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiFetch, formatDateTime } from '../api'

const route = useRoute()
const router = useRouter()

function toLocalDateStr(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const tomorrow = new Date(Date.now() + 86400000)

const activeTab = ref('schedule')
const settings = ref({
  mode: 'builtin_slots',
  doctor_name: '',
  direct_chat_url: '',
  external_widget_url: '',
  max_channel_invite_url: '',
})
const slots = ref([])
const slotFilter = ref(route.query.filter === 'draft' ? 'draft' : 'all')

const calendarMonth = ref(new Date(tomorrow.getFullYear(), tomorrow.getMonth(), 1))
const newSlot = ref({
  dateStr: toLocalDateStr(tomorrow),
  timeStr: '10:00',
  durationMin: 45,
  meeting_url: 'https://telemost.yandex.ru/j/zdorovie-room',
})
const notice = ref('')
const errorMsg = ref('')

function setFilter(val) {
  slotFilter.value = val
  router.replace({ query: val === 'all' ? {} : { filter: val } })
}

watch(
  () => route.query.filter,
  (v) => {
    slotFilter.value = v || 'all'
  }
)

const draftSlots = computed(() => slots.value.filter((s) => s.summary_status === 'draft'))
const draftSlotsCount = computed(() => draftSlots.value.length)
const filteredSlots = computed(() => {
  if (slotFilter.value === 'draft') return draftSlots.value
  if (slotFilter.value === 'booked') return slots.value.filter((s) => s.status === 'booked')
  if (slotFilter.value === 'available') return slots.value.filter((s) => s.status === 'available')
  return slots.value
})

const calendarMonthLabel = computed(() =>
  calendarMonth.value.toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' })
)

function shiftMonth(delta) {
  const d = calendarMonth.value
  calendarMonth.value = new Date(d.getFullYear(), d.getMonth() + delta, 1)
}

function selectCalendarDate(dateStr) {
  newSlot.value.dateStr = dateStr
}

const slotsByDate = computed(() => {
  const map = {}
  for (const sl of slots.value) {
    if (sl.status === 'canceled') continue
    const key = toLocalDateStr(new Date(sl.slot_start))
    if (!map[key]) map[key] = []
    map[key].push(sl)
  }
  return map
})

const calendarCells = computed(() => {
  const year = calendarMonth.value.getFullYear()
  const month = calendarMonth.value.getMonth()
  const firstDay = new Date(year, month, 1)
  const startOffset = (firstDay.getDay() + 6) % 7
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells = []
  for (let i = 0; i < startOffset; i++) {
    cells.push({ key: `pad-${i}`, day: '', inMonth: false, dateStr: '', slotCount: 0 })
  }
  for (let d = 1; d <= daysInMonth; d++) {
    const dt = new Date(year, month, d)
    const dateStr = toLocalDateStr(dt)
    cells.push({
      key: dateStr,
      day: d,
      inMonth: true,
      dateStr,
      slotCount: (slotsByDate.value[dateStr] || []).length,
    })
  }
  return cells
})

const slotsForSelectedDate = computed(() => slotsByDate.value[newSlot.value.dateStr] || [])

function findConflict(dateStr, timeStr, durationMin) {
  if (!dateStr || !timeStr) return null
  const start = new Date(`${dateStr}T${timeStr}:00`)
  if (Number.isNaN(start.getTime())) return null
  const end = new Date(start.getTime() + durationMin * 60000)
  for (const sl of slots.value) {
    if (sl.status === 'canceled') continue
    const s = new Date(sl.slot_start)
    const e = new Date(sl.slot_end)
    if (s < end && e > start) {
      return sl
    }
  }
  return null
}

const slotConflict = computed(() => findConflict(newSlot.value.dateStr, newSlot.value.timeStr, newSlot.value.durationMin))

const quickTimeOptions = computed(() => {
  const times = ['09:00', '10:00', '11:00', '12:00', '13:00', '14:00', '15:00', '16:00', '17:00', '18:00', '19:00']
  return times.map((time) => ({
    time,
    conflict: findConflict(newSlot.value.dateStr, time, newSlot.value.durationMin),
  }))
})

function formatClock(iso) {
  return new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

function formatTimeRange(startIso, endIso) {
  return `${formatClock(startIso)}–${formatClock(endIso)}`
}

async function loadAll() {
  try {
    const [cfg, sRes] = await Promise.all([
      apiFetch('/api/v1/admin/consultations/settings'),
      apiFetch('/api/v1/admin/consultations/slots'),
    ])
    settings.value = cfg
    slots.value = sRes.slots || []
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function saveSettings() {
  try {
    await apiFetch('/api/v1/admin/consultations/settings', { method: 'PUT', body: settings.value })
    notice.value = 'Настройки консультаций сохранены.'
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function createSlot() {
  errorMsg.value = ''
  notice.value = ''
  if (slotConflict.value) {
    errorMsg.value = `Невозможно добавить слот: пересечение со слотом #${slotConflict.value.id} (${formatTimeRange(slotConflict.value.slot_start, slotConflict.value.slot_end)}).`
    return
  }
  try {
    const start = new Date(`${newSlot.value.dateStr}T${newSlot.value.timeStr}:00`)
    const end = new Date(start.getTime() + newSlot.value.durationMin * 60000)
    await apiFetch('/api/v1/admin/consultations/slots', {
      method: 'POST',
      body: {
        slot_start: start.toISOString(),
        slot_end: end.toISOString(),
        meeting_url: newSlot.value.meeting_url,
      },
    })
    notice.value = 'Новый слот добавлен в расписание.'
    await loadAll()
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function deleteSlot(id) {
  try {
    await apiFetch(`/api/v1/admin/consultations/slots/${id}`, { method: 'DELETE' })
    await loadAll()
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(loadAll)
</script>
