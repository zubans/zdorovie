<template>
  <div>
    <div v-if="expired" class="alert alert-warn">
      <b>Срок вашей подписки истёк.</b> Продлите доступ в разделе профиля, чтобы продолжить обучение и скачивание памяток.
      <div style="margin-top:10px;"><router-link to="/profile" class="btn btn-sm">Продлить доступ</router-link></div>
    </div>
    <div v-else-if="errorMsg" class="alert alert-error">{{ errorMsg }}</div>

    <!-- Desktop Split View: Left Tree (4 cols) + Right Active Lesson (8 cols) -->
    <div v-if="!expired && materials.length > 0" class="split-4-8">
      <!-- Left Tree (4 cols) -->
      <div class="avo-card" style="margin:0;padding:18px;">
        <h3 style="font-size:0.95rem;margin:0 0 12px;padding:0 4px;">Программа курса «Авокадо»</h3>

        <div style="display:flex;flex-direction:column;gap:14px;">
          <div v-for="(items, modNum) in grouped" :key="modNum" style="display:flex;flex-direction:column;gap:6px;">
            <div class="muted" style="font-size:0.76rem;font-weight:700;padding:0 6px;">
              Модуль {{ modNum }}
            </div>

            <button
              v-for="m in items"
              :key="m.id"
              type="button"
              style="width:100%;text-align:left;padding:11px 13px;border-radius:16px;font-family:inherit;font-size:0.82rem;cursor:pointer;display:flex;align-items:center;justify-content:space-between;gap:10px;transition:all 0.15s;"
              :style="
                selectedLesson?.id === m.id
                  ? 'background:var(--primary-soft);color:var(--primary-text);border:1px solid rgba(75,112,38,0.28);font-weight:600;'
                  : 'background:var(--surface-soft);color:var(--text);border:1px solid rgba(75,112,38,0.10);'
              "
              @click="selectedId = m.id"
            >
              <span>{{ m.module_number }}.{{ m.lesson_number }} {{ m.title }}</span>
              <span
                v-if="m.completed"
                class="badge badge-green"
                style="padding:2px 8px;font-size:0.72rem;flex-shrink:0;"
              >
                ✓
              </span>
              <span
                v-else-if="selectedLesson?.id === m.id"
                style="font-size:0.74rem;font-weight:700;flex-shrink:0;"
              >
                Сейчас
              </span>
              <span v-else-if="m.duration_minutes" class="muted" style="font-size:0.74rem;flex-shrink:0;">
                {{ m.duration_minutes }} мин
              </span>
            </button>
          </div>
        </div>
      </div>

      <!-- Right Active Lesson (8 cols) -->
      <div v-if="selectedLesson" class="avo-card" style="margin:0;padding:24px 26px;display:flex;flex-direction:column;gap:16px;">
        <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;">
          <span class="badge badge-green">
            Модуль {{ selectedLesson.module_number }} · Урок {{ selectedLesson.lesson_number }}
            <template v-if="selectedLesson.duration_minutes"> · {{ selectedLesson.duration_minutes }} минут</template>
          </span>
          <span class="muted" style="font-size:0.8rem;">
            Статус: {{ selectedLesson.completed ? 'изучен ✓' : 'в изучении' }}
          </span>
        </div>

        <div>
          <h2 style="font-size:1.35rem;margin:0 0 6px;">{{ selectedLesson.title }}</h2>
          <p class="muted" style="margin:0;font-size:0.92rem;">{{ selectedLesson.description }}</p>
        </div>

        <!-- Formatted Lesson Notes instead of raw <pre> -->
        <div v-if="lessonSections.length > 0" class="avo-flesh" style="padding:18px 20px;display:flex;flex-direction:column;gap:12px;">
          <div style="font-size:0.9rem;font-weight:700;">Ключевые тезисы урока:</div>
          <div v-for="(sec, idx) in lessonSections" :key="idx" style="display:flex;flex-direction:column;gap:6px;">
            <div v-if="sec.heading" style="font-size:0.85rem;font-weight:700;color:var(--primary-text);">
              {{ sec.heading }}
            </div>
            <p
              v-for="(p, pIdx) in sec.paragraphs"
              :key="'p-' + pIdx"
              style="margin:0;font-size:0.84rem;line-height:1.55;"
            >
              {{ p }}
            </p>
            <ul v-if="sec.items.length" style="margin:2px 0 0;padding-left:18px;font-size:0.84rem;line-height:1.55;">
              <li v-for="(it, iIdx) in sec.items" :key="'i-' + iIdx" style="margin-bottom:4px;">{{ it }}</li>
            </ul>
          </div>
        </div>

        <div
          style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding-top:12px;border-top:1px solid var(--border);"
        >
          <a
            v-if="selectedLesson.video_url"
            :href="selectedLesson.video_url"
            target="_blank"
            class="btn btn-accent"
          >
            Смотреть видеоурок
          </a>

          <button
            v-if="selectedLesson.file_type === 'pdf' && selectedLesson.client_status === 'ready'"
            type="button"
            class="btn"
            @click="downloadPdf(selectedLesson.id)"
          >
            Скачать именную памятку (PDF)
          </button>
          <span
            v-else-if="selectedLesson.file_type === 'pdf'"
            class="badge badge-peach"
            style="padding:8px 14px;"
          >
            Готовим вашу именную памятку PDF...
          </span>

          <button
            type="button"
            class="btn btn-secondary"
            style="margin-left:auto;"
            @click="toggleComplete(selectedLesson)"
          >
            {{ selectedLesson.completed ? 'Урок пройден ✓ (снять отметку)' : 'Отметить урок пройденным ✓' }}
          </button>
        </div>
      </div>
    </div>

    <div v-else-if="!expired && !errorMsg" class="avo-card muted">
      Материалы курса загружаются...
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { apiFetch } from '../api'
import { parseSummarySections } from '../utils/formatSummary'

const materials = ref([])
const selectedId = ref(null)
const expired = ref(false)
const errorMsg = ref('')
let pollTimer = null

const grouped = computed(() => {
  const map = {}
  for (const m of materials.value) {
    if (!map[m.module_number]) map[m.module_number] = []
    map[m.module_number].push(m)
  }
  return map
})

const selectedLesson = computed(() => {
  if (!materials.value.length) return null
  if (selectedId.value != null) {
    const found = materials.value.find((m) => m.id === selectedId.value)
    if (found) return found
  }
  return materials.value.find((m) => !m.completed) || materials.value[0]
})

const lessonSections = computed(() => {
  return parseSummarySections(selectedLesson.value?.content_markdown || '')
})

async function loadMaterials() {
  try {
    const res = await apiFetch('/api/v1/cabinet/materials')
    materials.value = res.materials || []
    expired.value = false
    if (selectedId.value == null && materials.value.length > 0) {
      const next = materials.value.find((m) => !m.completed) || materials.value[0]
      selectedId.value = next.id
    }
    const hasPending = materials.value.some((m) => m.file_type === 'pdf' && ['pending', 'processing'].includes(m.client_status))
    if (hasPending && !pollTimer) {
      pollTimer = setInterval(loadMaterials, 3500)
    } else if (!hasPending && pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  } catch (e) {
    if (e.code === 'subscription_expired' || e.status === 403) {
      expired.value = true
    } else {
      errorMsg.value = e.message
    }
  }
}

async function toggleComplete(m) {
  try {
    await apiFetch(`/api/v1/cabinet/materials/${m.id}/progress`, {
      method: 'POST',
      body: { completed: !m.completed },
    })
    m.completed = !m.completed
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function downloadPdf(id) {
  try {
    const res = await apiFetch(`/api/v1/cabinet/materials/${id}/download?format=json`)
    if (res.url) window.open(res.url, '_blank')
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(loadMaterials)
onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>
