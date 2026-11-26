<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div>
      <h1 style="margin:0;">Заказы и подтверждение канала MAX</h1>
      <p class="muted" style="margin:3px 0 0;">
        Статусы оплат ЮKassa и отметка одобрения доступа в закрытый канал курса
      </p>
    </div>

    <div v-if="notice" class="alert alert-ok" style="margin:0;">{{ notice }}</div>
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <div class="avo-card" style="margin:0;overflow-x:auto;">
      <table>
        <thead>
          <tr>
            <th>Заказ</th>
            <th>Пациентка</th>
            <th>Тариф / Сумма</th>
            <th>Оплата</th>
            <th>Закрытый канал MAX</th>
            <th>Действие</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="o in orders" :key="o.id">
            <td>
              <b>{{ o.order_number }}</b>
              <div class="muted" style="font-size:0.76rem;">{{ formatDateTime(o.created_at) }}</div>
            </td>
            <td>
              <b>{{ o.user_full_name }}</b>
              <div class="muted" style="font-size:0.76rem;">{{ o.user_email }} · {{ o.user_phone }}</div>
            </td>
            <td>
              {{ o.tariff_title }}
              <div><b>{{ formatRub(o.amount_kopecks) }}</b></div>
            </td>
            <td>
              <span class="badge" :class="o.status === 'paid' ? 'badge-green' : 'badge-peach'">
                {{ o.status === 'paid' ? 'Оплачен ✓' : o.status }}
              </span>
            </td>
            <td>
              <span class="badge" :class="['approved', 'active'].includes(o.channel_access_status) ? 'badge-green' : 'badge-soft'">
                {{ o.channel_access_status }}
              </span>
            </td>
            <td>
              <button
                v-if="o.status === 'paid' && o.channel_access_status !== 'approved' && o.channel_access_status !== 'active'"
                class="btn btn-sm"
                @click="approveChannel(o)"
              >
                Подтвердить канал MAX
              </button>
              <span v-else class="muted">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { apiFetch, formatRub, formatDateTime } from '../api'

const orders = ref([])
const notice = ref('')
const errorMsg = ref('')

async function loadOrders() {
  try {
    const res = await apiFetch('/api/v1/admin/orders')
    orders.value = res.orders || []
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function approveChannel(o) {
  try {
    await apiFetch(`/api/v1/admin/orders/${o.id}/approve`, { method: 'POST' })
    notice.value = `Доступ к каналу для заказа ${o.order_number} подтверждён.`
    await loadOrders()
  } catch (e) {
    errorMsg.value = e.message
  }
}

onMounted(loadOrders)
</script>
