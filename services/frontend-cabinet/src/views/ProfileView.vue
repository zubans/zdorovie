<template>
  <div style="display:flex;flex-direction:column;gap:18px;">
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <div v-if="profile" class="split-6-6">
      <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:12px;">
        <div style="display:flex;align-items:center;gap:12px;">
          <span class="avatar-circle" style="width:42px;height:42px;font-size:0.95rem;">
            {{ getInitials(profile.user.full_name) }}
          </span>
          <div>
            <h3 style="margin:0;">{{ profile.user.full_name }}</h3>
            <span class="muted" style="font-size:0.8rem;">{{ profile.user.pregnancy_status || 'Участница курса' }}</span>
          </div>
        </div>

        <div class="avo-soft" style="display:grid;gap:6px;font-size:0.84rem;">
          <div><b>E-mail:</b> {{ profile.user.email }}</div>
          <div><b>Телефон:</b> {{ profile.user.phone || '—' }}</div>
          <div>
            <b>Доступ:</b>
            <span class="badge" :class="profile.sub_active ? 'badge-green' : 'badge-red'" style="margin-left:6px;">
              {{ profile.sub_active ? `Активен до ${formatDate(profile.subscription?.expires_at)}` : 'Истёк / не оплачен' }}
            </span>
          </div>
          <div v-if="profile.subscription?.tariff_title">
            <b>Тариф:</b> {{ profile.subscription.tariff_title }}
          </div>
        </div>
      </div>

      <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:12px;">
        <div>
          <h3 style="margin:0;">Продлить или повысить тариф</h3>
          <p class="muted" style="margin:3px 0 0;font-size:0.8rem;">
            При продлении оставшиеся дни текущей подписки сохраняются и суммируются.
          </p>
        </div>

        <div class="field" style="margin:0;">
          <label>Выберите тариф</label>
          <select v-model="selectedTariff" class="input input-soft">
            <option v-for="t in profile.tariffs" :key="t.code" :value="t.code">
              {{ t.title }} — {{ Math.round(t.price_kopecks / 100) }} ₽
            </option>
          </select>
        </div>

        <div>
          <button class="btn" :disabled="renewing" @click="renewOrder">
            {{ renewing ? 'Переход к оплате...' : 'Перейти к оплате' }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="profile?.orders?.length" class="avo-card" style="margin:0;">
      <h3 style="margin-bottom:12px;">История заказов</h3>
      <div style="overflow-x:auto;">
        <table style="width:100%;border-collapse:collapse;font-size:0.84rem;">
          <thead>
            <tr style="border-bottom:1px solid var(--border);text-align:left;color:var(--muted);">
              <th style="padding:10px 8px;">Заказ</th>
              <th style="padding:10px 8px;">Тариф</th>
              <th style="padding:10px 8px;">Сумма</th>
              <th style="padding:10px 8px;">Статус</th>
              <th style="padding:10px 8px;">Чек / Канал</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="o in profile.orders" :key="o.id" style="border-bottom:1px solid var(--border);">
              <td style="padding:10px 8px;"><b>{{ o.order_number }}</b></td>
              <td style="padding:10px 8px;">{{ o.tariff_title }}</td>
              <td style="padding:10px 8px;">{{ Math.round(o.amount_kopecks / 100) }} ₽</td>
              <td style="padding:10px 8px;">
                <span class="badge" :class="o.status === 'paid' ? 'badge-green' : 'badge-soft'">
                  {{ o.status === 'paid' ? 'Оплачен' : o.status }}
                </span>
              </td>
              <td style="padding:10px 8px;">
                <a v-if="o.receipt_url" :href="o.receipt_url" target="_blank">Открыть чек</a>
                <a v-else-if="o.confirmation_url && o.status === 'pending'" :href="o.confirmation_url" class="btn btn-sm">
                  Оплатить
                </a>
                <span v-else class="muted">Канал: {{ o.channel_access_status }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiFetch, formatDate } from '../api'
import { getInitials } from '../utils/formatSummary'

const profile = ref(null)
const selectedTariff = ref('feedback_3m')
const renewing = ref(false)
const errorMsg = ref('')

async function renewOrder() {
  renewing.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/cabinet/renew', {
      method: 'POST',
      body: { tariff_code: selectedTariff.value },
    })
    if (res.payment_url) window.location.href = res.payment_url
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    renewing.value = false
  }
}

onMounted(async () => {
  try {
    profile.value = await apiFetch('/api/v1/cabinet/profile')
    if (profile.value?.tariffs?.length) {
      selectedTariff.value = profile.value.tariffs[0].code
    }
  } catch (e) {
    errorMsg.value = e.message
  }
})
</script>
