<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:12px;">
      <div>
        <h1 style="margin:0;">Курс «Авокадо» и тарифы</h1>
        <p class="muted" style="margin:3px 0 0;">
          Уроки и мастер-PDF отдельно от настройки тарифной сетки
        </p>
      </div>

      <div class="tab-pill-group">
        <button
          type="button"
          class="tab-pill-btn"
          :class="{ active: activeTab === 'lessons' }"
          @click="activeTab = 'lessons'"
        >
          Уроки и методички ({{ materials.length }})
        </button>
        <button
          type="button"
          class="tab-pill-btn"
          :class="{ active: activeTab === 'tariffs' }"
          @click="activeTab = 'tariffs'"
        >
          Тарифы ({{ tariffs.length }})
        </button>
      </div>
    </div>

    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <!-- TAB 1: LESSONS & MASTER PDFs -->
    <template v-if="activeTab === 'lessons'">
      <div class="avo-card" style="margin:0;">
        <h3 style="margin-bottom:12px;">Добавить урок и мастер-PDF (для именных водяных знаков)</h3>
        <form @submit.prevent="createMaterial" class="grid-2">
          <div class="field">
            <label>Модуль</label>
            <input v-model.number="newMat.module_number" type="number" min="1" class="input input-soft" required />
          </div>
          <div class="field">
            <label>Урок</label>
            <input v-model.number="newMat.lesson_number" type="number" min="1" class="input input-soft" required />
          </div>
          <div class="field">
            <label>Название урока / памятки</label>
            <input v-model="newMat.title" class="input input-soft" required placeholder="Например: Ферритин и мягкая энергия" />
          </div>
          <div class="field">
            <label>Видеоурок (URL, опционально)</label>
            <input v-model="newMat.video_url" class="input input-soft" placeholder="https://kinescope.io/..." />
          </div>
          <div class="field" style="grid-column:1 / -1;">
            <label>Краткое описание</label>
            <textarea v-model="newMat.description" rows="2" class="input input-soft"></textarea>
          </div>
          <div class="field">
            <label>Мастер-PDF</label>
            <input ref="fileRef" type="file" accept=".pdf" />
          </div>
          <div style="display:flex;align-items:flex-end;padding-bottom:12px;">
            <button type="submit" class="btn">Создать и загрузить PDF</button>
          </div>
        </form>
      </div>

      <div class="avo-card" style="margin:0;overflow-x:auto;">
        <h3 style="margin-bottom:10px;">Программа уроков</h3>
        <table>
          <thead>
            <tr>
              <th>Модуль / Урок</th>
              <th>Название и описание</th>
              <th>Формат</th>
              <th>Ключ в S3</th>
              <th>Статус</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in materials" :key="m.id">
              <td><b>М{{ m.module_number }} · У{{ m.lesson_number }}</b></td>
              <td>
                <b>{{ m.title }}</b>
                <div class="muted" style="font-size:0.78rem;">{{ m.description }}</div>
              </td>
              <td><span class="badge badge-soft">{{ m.file_type }}</span></td>
              <td><code style="font-size:0.78rem;">{{ m.s3_master_key }}</code></td>
              <td>
                <span class="badge" :class="m.is_published ? 'badge-green' : 'badge-soft'">
                  {{ m.is_published ? 'Опубликован' : 'Скрыт' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- TAB 2: TARIFFS -->
    <template v-else>
      <div class="avo-card" style="margin:0;overflow-x:auto;">
        <h3 style="margin-bottom:10px;">Управление тарифами</h3>
        <table>
          <thead>
            <tr>
              <th>Код</th>
              <th>Название</th>
              <th>Цена (₽)</th>
              <th>Дней</th>
              <th>ОС врача</th>
              <th>Активен</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in tariffs" :key="t.id">
              <td><code>{{ t.code }}</code></td>
              <td><input v-model="t.title" class="input input-soft" /></td>
              <td><input v-model.number="t.price_rub" type="number" class="input input-soft" style="width:110px;" /></td>
              <td><input v-model.number="t.duration_days" type="number" class="input input-soft" style="width:90px;" /></td>
              <td><input v-model="t.has_feedback" type="checkbox" style="width:auto;" /></td>
              <td><input v-model="t.is_active" type="checkbox" style="width:auto;" /></td>
              <td><button class="btn btn-sm" @click="saveTariff(t)">Сохранить</button></td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiFetch } from '../api'

const activeTab = ref('lessons')
const materials = ref([])
const tariffs = ref([])
const fileRef = ref(null)
const newMat = ref({ module_number: 1, lesson_number: 2, title: '', description: '', video_url: '' })
const notice = ref('')
const errorMsg = ref('')

async function loadAll() {
  try {
    const [mRes, tRes] = await Promise.all([apiFetch('/api/v1/admin/materials'), apiFetch('/api/v1/admin/tariffs')])
    materials.value = mRes.materials || []
    tariffs.value = (tRes.tariffs || []).map((t) => ({ ...t, price_rub: Math.round(t.price_kopecks / 100) }))
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function createMaterial() {
  errorMsg.value = ''
  const fd = new FormData()
  fd.append('module_number', String(newMat.value.module_number))
  fd.append('lesson_number', String(newMat.value.lesson_number))
  fd.append('title', newMat.value.title)
  fd.append('description', newMat.value.description)
  fd.append('video_url', newMat.value.video_url)
  fd.append('is_published', 'true')
  if (fileRef.value?.files?.[0]) {
    fd.append('master_pdf', fileRef.value.files[0])
  }
  try {
    await apiFetch('/api/v1/admin/materials', { method: 'POST', body: fd })
    notice.value = 'Материал добавлен!'
    newMat.value.title = ''
    newMat.value.description = ''
    if (fileRef.value) fileRef.value.value = ''
    await loadAll()
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function saveTariff(t) {
  try {
    await apiFetch(`/api/v1/admin/tariffs/${t.id}`, {
      method: 'PATCH',
      body: {
        title: t.title,
        description: t.description,
        price_kopecks: Math.round(t.price_rub * 100),
        duration_days: t.duration_days,
        has_feedback: t.has_feedback,
        is_active: t.is_active,
      },
    })
    notice.value = `Тариф «${t.code}» обновлён.`
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(loadAll)
</script>
