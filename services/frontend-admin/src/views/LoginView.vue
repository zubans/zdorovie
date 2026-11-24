<template>
  <div style="max-width:440px;margin:52px auto;">
    <div class="avo-flesh" style="display:flex;align-items:center;gap:14px;margin-bottom:16px;">
      <img
        :src="mascotUrl"
        alt="Авокадо"
        style="width:56px;height:56px;border-radius:16px;object-fit:cover;border:1px solid rgba(75,112,38,0.18);flex-shrink:0;"
      />
      <div>
        <div style="font-size:1.15rem;font-weight:700;">Авокадо · Врач</div>
        <div class="muted" style="font-size:0.8rem;">Клиническая панель и CRM сопровождения беременных</div>
      </div>
    </div>

    <div class="avo-card" style="margin:0;">
      <h2 style="margin-bottom:4px;">Вход для врача</h2>
      <p class="muted" style="margin:0 0 16px;">Приёмная, расписание, чаты пациенток и проверка ИИ-заключений</p>

      <div v-if="errorMsg" class="alert alert-error">{{ errorMsg }}</div>

      <form @submit.prevent="login">
        <div class="field">
          <label>E-mail врача</label>
          <input v-model="email" type="email" class="input input-soft" required placeholder="doctor@zdorovie.local" />
        </div>
        <div class="field">
          <label>Пароль</label>
          <input v-model="password" type="password" class="input input-soft" required />
        </div>
        <button type="submit" class="btn" style="width:100%;" :disabled="loading">
          {{ loading ? 'Вход...' : 'Войти в панель врача' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { apiFetch, setToken } from '../api'
import mascotUrl from '../assets/avocado-mascot.jpg'

const emit = defineEmits(['authenticated'])
const router = useRouter()

const email = ref('doctor@zdorovie.local')
const password = ref('zdorovie_doctor_pass')
const errorMsg = ref('')
const loading = ref(false)

async function login() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/auth/login', {
      method: 'POST',
      body: { email: email.value, password: password.value },
    })
    if (res.access_token) setToken(res.access_token)
    emit('authenticated')
    router.replace('/analytics')
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    loading.value = false
  }
}
</script>
