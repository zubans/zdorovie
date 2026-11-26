<template>
  <div style="max-width:520px;margin:28px auto;">
    <div class="avo-flesh" style="display:flex;align-items:center;gap:16px;margin-bottom:18px;">
      <img
        :src="mascotUrl"
        alt="Авокадо"
        style="width:62px;height:62px;border-radius:18px;object-fit:cover;border:1px solid rgba(75,112,38,0.18);flex-shrink:0;"
      />
      <div>
        <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:4px;">
          <span style="font-size:1.25rem;font-weight:700;">Авокадо</span>
          <span class="badge badge-green">Беременность без тревоги</span>
        </div>
        <p class="muted" style="margin:0;font-size:0.83rem;">
          Спокойный личный кабинет будущей мамы: уроки по триместрам, именные памятки и прямая связь с врачом
        </p>
      </div>
    </div>

    <div v-if="statusMsg" class="alert alert-ok">{{ statusMsg }}</div>
    <div v-if="errorMsg" class="alert alert-error">{{ errorMsg }}</div>
    <div v-if="route.query.paid" class="alert alert-ok">
      Оплата заказа <b>{{ route.query.paid }}</b> подтверждена! Введите e-mail ниже, чтобы получить код входа (или перейдите по Magic Link из письма / бота MAX).
    </div>

    <div class="card">
      <div class="avo-soft" style="display:flex;gap:6px;padding:5px;border-radius:14px;margin-bottom:20px;">
        <button
          type="button"
          class="btn btn-sm"
          :class="tab === 'otp' ? '' : 'btn-secondary'"
          style="flex:1;border:none;"
          @click="tab = 'otp'"
        >
          Вход по Email и коду
        </button>
        <button
          type="button"
          class="btn btn-sm"
          :class="tab === 'buy' ? '' : 'btn-secondary'"
          style="flex:1;border:none;"
          @click="switchToBuy"
        >
          Оформить доступ
        </button>
      </div>

      <div v-if="magicLoading" style="text-align:center;padding:24px;">
        <p class="muted">Проверяем одноразовую ссылку входа...</p>
      </div>

      <div v-else-if="tab === 'otp'">
        <form @submit.prevent="otpSent ? verifyOtp() : requestOtp()">
          <div class="field">
            <label>Ваш E-mail</label>
            <input v-model="email" type="email" class="input" required placeholder="anna@example.ru" :disabled="otpSent" />
            <p v-if="email && !otpEmailCheck.valid" style="color:var(--danger);font-size:0.8rem;margin:5px 0 0;">
              {{ otpEmailCheck.reason }}
            </p>
          </div>

          <div v-if="otpSent" class="field">
            <label>6-значный код из письма</label>
            <input v-model="otpCode" type="text" class="input" maxlength="6" required placeholder="123456" />
            <p v-if="devCodeHint" class="muted" style="margin-top:6px;">
              Код для входа на стенде: <b>{{ devCodeHint }}</b>
            </p>
          </div>

          <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap;">
            <button type="submit" class="btn" :disabled="loading">
              {{ otpSent ? 'Войти в кабинет' : 'Получить код на E-mail' }}
            </button>
            <button v-if="otpSent" type="button" class="btn btn-secondary btn-sm" @click="otpSent = false; otpCode = ''">
              Изменить e-mail
            </button>
          </div>
        </form>
      </div>

      <div v-else>
        <form @submit.prevent="submitCheckout">
          <div class="field">
            <label>Выберите формат участия</label>
            <select v-model="checkout.tariff_code" required>
              <option v-for="t in tariffs" :key="t.code" :value="t.code">
                {{ t.title }} — {{ Math.round(t.price_kopecks / 100) }} ₽
              </option>
            </select>
          </div>
          <div class="field">
            <label>Имя и фамилия</label>
            <input v-model="checkout.full_name" class="input" required placeholder="Смирнова Анна" />
          </div>
          <div class="field">
            <label>Телефон</label>
            <input
              :value="checkout.phone"
              type="tel"
              inputmode="tel"
              autocomplete="tel"
              maxlength="18"
              class="input"
              required
              placeholder="+7 (___) ___-__-__"
              @focus="onPhoneFocus"
              @input="onPhoneInput"
            />
            <p v-if="checkout.phone && !phoneValid" style="color:var(--danger);font-size:0.8rem;margin:5px 0 0;">
              Введите все 10 цифр номера в формате +7 (999) 123-45-67
            </p>
            <p v-else-if="phoneValid" style="color:var(--primary);font-size:0.8rem;margin:5px 0 0;">
              ✓ Номер заполнен по маске +7 (***) ***-**-**
            </p>
          </div>
          <div class="field">
            <label>E-mail (для чека и ссылки в кабинет)</label>
            <input v-model="checkout.email" type="email" class="input" required placeholder="anna@example.ru" />
            <p v-if="checkout.email && !checkoutEmailCheck.valid" style="color:var(--danger);font-size:0.8rem;margin:5px 0 0;">
              {{ checkoutEmailCheck.reason }}
            </p>
            <p v-else-if="checkout.email && checkoutEmailCheck.valid" style="color:var(--primary);font-size:0.8rem;margin:5px 0 0;">
              ✓ Формат E-mail корректен
            </p>
            <p v-else class="muted" style="font-size:0.78rem;margin:5px 0 0;">
              Проверяется формат адреса вида <code>name@domain.ru</code>
            </p>
          </div>
          <div class="field">
            <label>Срок беременности или статус</label>
            <input v-model="checkout.pregnancy_status" class="input" placeholder="24 недели / Планирую" />
          </div>
          <div class="field" style="display:flex;gap:8px;align-items:flex-start;">
            <input id="consent" v-model="checkout.consent_accepted" type="checkbox" style="margin-top:4px;width:auto;" />
            <label for="consent" style="font-weight:400;margin:0;color:var(--muted);">
              Согласна с договором-офертой и политикой обработки персональных данных (152-ФЗ)
            </label>
          </div>
          <button type="submit" class="btn" style="width:100%;" :disabled="loading">Перейти к оплате</button>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiFetch, setToken } from '../api'
import { formatPhoneMask, isPhoneComplete, validateEmailMask } from '../utils/masks'
import mascotUrl from '../assets/avocado-mascot.jpg'

const emit = defineEmits(['authenticated'])
const route = useRoute()
const router = useRouter()

const tab = ref('otp')
const loading = ref(false)
const magicLoading = ref(false)
const email = ref('')
const otpSent = ref(false)
const otpCode = ref('')
const devCodeHint = ref('')
const statusMsg = ref('')
const errorMsg = ref('')

const tariffs = ref([])
const checkout = ref({
  tariff_code: 'feedback_3m',
  full_name: '',
  phone: '',
  email: '',
  pregnancy_status: '24 недели',
  consent_accepted: true,
})

const otpEmailCheck = computed(() => validateEmailMask(email.value))
const checkoutEmailCheck = computed(() => validateEmailMask(checkout.value.email))
const phoneValid = computed(() => isPhoneComplete(checkout.value.phone))

function onPhoneFocus(e) {
  if (!checkout.value.phone) {
    checkout.value.phone = '+7 ('
    e.target.value = '+7 ('
  }
}

function onPhoneInput(e) {
  const masked = formatPhoneMask(e.target.value, checkout.value.phone)
  checkout.value.phone = masked
  e.target.value = masked
}

async function handleMagicToken(token) {
  magicLoading.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/auth/magic', { method: 'POST', body: { token } })
    if (res.access_token) setToken(res.access_token)
    emit('authenticated')
    router.replace('/dashboard')
  } catch (e) {
    errorMsg.value = e.message || 'Не удалось войти по ссылке'
  } finally {
    magicLoading.value = false
  }
}

async function requestOtp() {
  if (!otpEmailCheck.value.valid) {
    errorMsg.value = otpEmailCheck.value.reason
    return
  }
  loading.value = true
  errorMsg.value = ''
  statusMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/auth/otp/request', { method: 'POST', body: { email: email.value.trim() } })
    otpSent.value = true
    statusMsg.value = res.message
    devCodeHint.value = res.dev_code || ''
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    loading.value = false
  }
}

async function verifyOtp() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/auth/otp/verify', {
      method: 'POST',
      body: { email: email.value.trim(), code: otpCode.value },
    })
    if (res.access_token) setToken(res.access_token)
    emit('authenticated')
    router.replace('/dashboard')
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    loading.value = false
  }
}

async function switchToBuy() {
  tab.value = 'buy'
  if (tariffs.value.length === 0) {
    try {
      const res = await apiFetch('/api/v1/tariffs')
      tariffs.value = res.tariffs || []
      if (tariffs.value.length > 0 && !checkout.value.tariff_code) {
        checkout.value.tariff_code = tariffs.value[0].code
      }
    } catch {}
  }
}

async function submitCheckout() {
  errorMsg.value = ''
  if (!phoneValid.value) {
    errorMsg.value = 'Укажите полный номер телефона в формате +7 (***) ***-**-**'
    return
  }
  if (!checkoutEmailCheck.value.valid) {
    errorMsg.value = checkoutEmailCheck.value.reason
    return
  }
  loading.value = true
  try {
    const res = await apiFetch('/api/v1/public/checkout', {
      method: 'POST',
      body: {
        ...checkout.value,
        email: checkout.value.email.trim(),
      },
    })
    if (res.payment_url) {
      window.location.href = res.payment_url
    }
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  const token = route.query.token
  if (typeof token === 'string' && token.length > 0) {
    handleMagicToken(token)
  }
  if (route.query.tab === 'buy') {
    switchToBuy()
  }
})
</script>
