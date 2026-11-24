<template>
  <div style="display:flex;flex-direction:column;gap:18px;">
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <div v-if="docs.length === 0 && !errorMsg" class="avo-card" style="margin:0;">
      <h3 style="margin-bottom:6px;">Медицинские заключения и выписки</h3>
      <p class="muted" style="margin:0;font-size:0.88rem;">
        Пока нет опубликованных итогов консультаций. После приёма и утверждения рекомендаций врачом здесь появится персональный лист выписки и официальный PDF-документ.
      </p>
    </div>

    <div
      v-for="doc in docs"
      :key="doc.id"
      class="avo-card"
      style="margin:0;padding:24px 26px;display:flex;flex-direction:column;gap:18px;"
    >
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:14px;flex-wrap:wrap;">
        <div>
          <span class="badge badge-green">
            Утверждено врачом · {{ formatDate(doc.published_at || doc.created_at) }}
          </span>
          <h2 style="font-size:1.3rem;margin:8px 0 4px;">{{ doc.title }}</h2>
          <div v-if="doc.doctor_name" class="muted" style="font-size:0.82rem;">
            Врач: {{ doc.doctor_name }}
          </div>
        </div>
        <button type="button" class="btn" @click="downloadDoc(doc.id)">
          Скачать официальный PDF
        </button>
      </div>

      <div class="grid-2" style="gap:14px;">
        <div v-if="doc.recommendations" class="avo-flesh" style="padding:16px 18px;">
          <div style="font-size:0.9rem;font-weight:700;margin-bottom:6px;">Индивидуальные рекомендации</div>
          <div class="pre" style="font-size:0.84rem;line-height:1.55;">{{ doc.recommendations }}</div>
        </div>

        <div style="display:flex;flex-direction:column;gap:12px;">
          <div v-if="doc.diagnostics_plan || doc.follow_up" class="avo-soft">
            <div style="font-size:0.88rem;font-weight:700;margin-bottom:5px;">Контроль и анализы</div>
            <div v-if="doc.diagnostics_plan" class="pre" style="font-size:0.82rem;line-height:1.5;margin-bottom:6px;">
              {{ doc.diagnostics_plan }}
            </div>
            <div v-if="doc.follow_up" class="pre muted" style="font-size:0.8rem;line-height:1.5;">
              {{ doc.follow_up }}
            </div>
          </div>

          <div v-if="doc.complaints || doc.anamnesis" class="avo-soft">
            <div style="font-size:0.86rem;font-weight:700;margin-bottom:5px;">Запрос и клиническая оценка</div>
            <div v-if="doc.complaints" style="font-size:0.81rem;margin-bottom:4px;">
              <b>Запрос:</b> {{ doc.complaints }}
            </div>
            <div v-if="doc.anamnesis" class="muted" style="font-size:0.8rem;line-height:1.48;">
              {{ doc.anamnesis }}
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiFetch, formatDate } from '../api'

const docs = ref([])
const errorMsg = ref('')

async function downloadDoc(id) {
  try {
    const res = await apiFetch(`/api/v1/cabinet/documents/${id}/download?format=json`)
    if (res.url) window.open(res.url, '_blank')
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(async () => {
  try {
    const res = await apiFetch('/api/v1/cabinet/documents')
    docs.value = res.documents || []
  } catch (e) {
    errorMsg.value = e.message
  }
})
</script>
