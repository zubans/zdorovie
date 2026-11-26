<template>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div>
      <router-link to="/clients" class="muted" style="font-weight:600;">← Назад ко всем пациенткам</router-link>
    </div>

    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>
    <div v-if="noticeMsg" class="alert alert-ok" style="margin:0;">{{ noticeMsg }}</div>

    <!-- Top Patient Summary Header Card -->
    <div
      v-if="profile"
      class="avo-card"
      style="margin:0;padding:16px 20px;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:14px;"
    >
      <div style="display:flex;align-items:center;gap:14px;">
        <span class="avatar-circle" style="width:44px;height:44px;font-size:0.95rem;">
          {{ getInitials(profile.user.full_name) }}
        </span>
        <div>
          <div style="font-size:1.1rem;font-weight:700;">
            {{ profile.user.full_name }} · {{ profile.user.pregnancy_status || 'срок не указан' }}
          </div>
          <div class="muted" style="font-size:0.8rem;margin-top:2px;">
            {{ profile.user.phone || '—' }} · {{ profile.user.email || '—' }}
            <template v-if="profile.subscription?.tariff_title">
              · {{ profile.subscription.tariff_title }}
              <template v-if="profile.subscription?.expires_at"> до {{ formatDate(profile.subscription.expires_at) }}</template>
            </template>
          </div>
        </div>
      </div>

      <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;">
        <span class="badge" :class="profile.sub_active ? 'badge-green' : 'badge-red'">
          {{ profile.sub_active ? 'Подписка активна' : 'Доступ не активен' }}
        </span>
        <span class="badge badge-green">
          Уроки {{ completedMaterialsCount }}/{{ profile.materials?.length || 0 }}
        </span>
        <span class="badge badge-soft">
          Встречи {{ profile.consultations?.length || 0 }}
        </span>
        <span class="badge badge-soft">
          Анализы {{ profile.attachments?.length || 0 }}
        </span>
        <span class="badge badge-soft">
          Назначения {{ profile.documents?.length || 0 }}
        </span>
      </div>
    </div>

    <!-- Auto Pre-Consultation Summary Alert (if generated 30 min before upcoming slot) -->
    <div
      v-if="preConsultationSummary"
      class="avo-flesh"
      style="padding:13px 18px;display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;"
    >
      <div style="font-size:0.84rem;">
        <b>Подготовлено предконсультационное ИИ-саммари (за 30 мин до приёма)</b> ·
        <span class="muted">{{ preConsultationSummary.scope_label }}</span>
      </div>
      <router-link
        v-if="preConsultationSummary.slot_id"
        :to="`/consultations/${preConsultationSummary.slot_id}`"
        class="btn btn-sm"
      >
        Перейти в рабочее место встречи →
      </router-link>
    </div>

    <!-- Desktop Split Grid: Left (5 cols) = Chat, Right (7 cols) = AI Summary & Tabbed History -->
    <div v-if="profile" class="split-5-7">
      <!-- LEFT 5 COLS: Chat with Patient -->
      <div
        id="chat"
        class="avo-card"
        style="margin:0;display:flex;flex-direction:column;gap:12px;"
        :style="unreadFromClient > 0 ? 'border-color:#c96f3b;box-shadow:0 0 0 2px rgba(201,111,59,0.18);' : ''"
      >
        <div style="display:flex;align-items:center;justify-content:space-between;gap:8px;flex-wrap:wrap;">
          <div>
            <h3 style="margin:0;">Чат с пациенткой</h3>
            <div class="muted" style="font-size:0.76rem;">Внутренняя переписка кабинета (входит в ИИ-саммари)</div>
          </div>
          <div style="display:flex;gap:6px;align-items:center;">
            <span v-if="unreadFromClient > 0" class="badge badge-peach">
              Новых: {{ unreadFromClient }}
            </span>
            <button
              v-if="unreadFromClient > 0"
              type="button"
              class="btn btn-secondary btn-sm"
              @click="markChatRead"
            >
              Прочитано
            </button>
            <button type="button" class="btn btn-secondary btn-sm" @click="loadChat(true)">↻</button>
          </div>
        </div>

        <div
          ref="chatBox"
          class="avo-soft"
          style="height:380px;overflow-y:auto;padding:12px;display:flex;flex-direction:column;gap:8px;"
        >
          <div v-if="chatMessages.length === 0" class="muted" style="margin:auto;text-align:center;font-size:0.82rem;">
            Переписки в кабинете пока нет. Вы можете первой написать {{ profile.user.full_name }}.
          </div>

          <div
            v-for="m in chatMessages"
            :key="m.id"
            :style="{
              alignSelf: m.sender_role === 'client' ? 'flex-start' : 'flex-end',
              maxWidth: '85%',
              padding: '9px 12px',
              borderRadius: '14px',
              background: m.sender_role === 'client' ? '#ffffff' : 'var(--primary-soft)',
              border:
                m.sender_role === 'client'
                  ? m.read_by_doctor
                    ? '1px solid rgba(0,0,0,0.06)'
                    : '1px solid #c96f3b'
                  : '1px solid rgba(75,112,38,0.2)',
            }"
          >
            <div class="muted" style="display:flex;justify-content:space-between;gap:10px;font-size:0.72rem;margin-bottom:2px;">
              <b :style="m.sender_role === 'client' ? 'color:var(--text);' : 'color:var(--primary-text);'">
                {{ m.sender_role === 'client' ? profile.user.full_name : (m.sender_name || 'Врач') }}
              </b>
              <span>{{ formatDateTime(m.created_at) }}</span>
            </div>
            <div style="white-space:pre-wrap;line-height:1.45;font-size:0.84rem;">{{ m.body }}</div>
          </div>
        </div>

        <form @submit.prevent="sendChatMessage" style="display:flex;gap:8px;align-items:flex-end;">
          <textarea
            ref="chatInput"
            v-model="chatDraft"
            rows="2"
            class="input input-soft"
            style="flex:1;resize:vertical;font-size:0.84rem;"
            placeholder="Ответ прямо с карточки... (Enter — отправить)"
            @keydown.enter.exact.prevent="sendChatMessage"
          ></textarea>
          <button type="submit" class="btn btn-sm" style="height:40px;" :disabled="chatSending || !chatDraft.trim()">
            {{ chatSending ? '...' : 'Отправить' }}
          </button>
        </form>
      </div>

      <!-- RIGHT 7 COLS: AI Summary Context & Tabbed Medical History -->
      <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:16px;">
        <!-- AI Summary Section -->
        <div style="display:flex;flex-direction:column;gap:10px;">
          <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:10px;flex-wrap:wrap;">
            <div>
              <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
                <h3 style="margin:0;">ИИ-резюме пациентки (по шаблону «Авокадо»)</h3>
                <span v-if="unprocessedEventsCount > 0" class="badge badge-peach">
                  +{{ unprocessedEventsCount }} новых необработанных событий
                </span>
                <span v-else class="badge badge-green">Все данные учтены ✓</span>
              </div>
              <div class="muted" style="font-size:0.76rem;margin-top:3px;">
                {{ customSummary?.scope_label || 'Все материалы и история профиля' }}
                <template v-if="customSummary?.events_count != null">
                  · единиц данных: <b>{{ customSummary.events_count }}</b>
                </template>
              </div>
            </div>

            <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;">
              <button
                v-if="unprocessedEventsCount > 0"
                type="button"
                class="btn btn-peach btn-sm"
                :disabled="summaryLoading"
                @click="quickIncrementalSummary"
              >
                {{ summaryLoading ? 'Обновляем...' : `Дополнить новыми (+${unprocessedEventsCount})` }}
              </button>
              <button
                type="button"
                class="btn btn-sm"
                @click="openSummaryModal"
              >
                Сформировать саммари (выбор данных)...
              </button>
            </div>
          </div>

          <!-- Formatted 6-Section AI Summary Box -->
          <div class="avo-flesh" style="padding:15px 18px;display:flex;flex-direction:column;gap:10px;font-size:0.83rem;line-height:1.52;">
            <template v-if="parsedSummary.length > 0">
              <div
                v-for="(sec, idx) in parsedSummary"
                :key="idx"
                style="padding-bottom:8px;border-bottom:1px solid rgba(75,112,38,0.10);"
              >
                <div v-if="sec.heading" style="font-weight:700;color:var(--primary-text);margin-bottom:3px;font-size:0.86rem;">
                  {{ sec.heading }}
                </div>
                <p v-for="(p, pIdx) in sec.paragraphs" :key="'p-' + pIdx" style="margin:0 0 4px;">
                  {{ p }}
                </p>
                <ul v-if="sec.items.length" style="margin:2px 0 0;padding-left:18px;">
                  <li v-for="(it, iIdx) in sec.items" :key="'i-' + iIdx" style="margin-bottom:2px;">{{ it }}</li>
                </ul>
              </div>
            </template>
            <div v-else class="muted">
              {{ customSummary?.summary_text || 'Нет данных для отображения сводки.' }}
            </div>
          </div>
        </div>

        <!-- Tabbed History Switcher (Consultations / Analyses / Prescriptions / Lessons / MAX Questions) -->
        <div>
          <div
            style="display:flex;gap:6px;flex-wrap:wrap;padding-bottom:10px;border-bottom:1px solid var(--border);margin-bottom:12px;"
          >
            <button
              type="button"
              class="badge"
              style="cursor:pointer;border:none;padding:6px 13px;font-family:inherit;"
              :class="historyTab === 'consultations' ? 'badge-green' : 'badge-soft'"
              @click="historyTab = 'consultations'"
            >
              Встречи ({{ profile.consultations?.length || 0 }})
            </button>
            <button
              type="button"
              class="badge"
              style="cursor:pointer;border:none;padding:6px 13px;font-family:inherit;"
              :class="historyTab === 'attachments' ? 'badge-green' : 'badge-soft'"
              @click="historyTab = 'attachments'"
            >
              Анализы ({{ profile.attachments?.length || 0 }})
            </button>
            <button
              type="button"
              class="badge"
              style="cursor:pointer;border:none;padding:6px 13px;font-family:inherit;"
              :class="historyTab === 'documents' ? 'badge-green' : 'badge-soft'"
              @click="historyTab = 'documents'"
            >
              Назначения ({{ profile.documents?.length || 0 }})
            </button>
            <button
              type="button"
              class="badge"
              style="cursor:pointer;border:none;padding:6px 13px;font-family:inherit;"
              :class="historyTab === 'materials' ? 'badge-green' : 'badge-soft'"
              @click="historyTab = 'materials'"
            >
              Уроки ({{ profile.materials?.length || 0 }})
            </button>
            <button
              type="button"
              class="badge"
              style="cursor:pointer;border:none;padding:6px 13px;font-family:inherit;"
              :class="historyTab === 'questions' ? 'badge-green' : 'badge-soft'"
              @click="historyTab = 'questions'"
            >
              Вопросы MAX ({{ profile.questions?.length || 0 }})
            </button>
          </div>

          <!-- Tab: Consultations -->
          <div v-if="historyTab === 'consultations'" style="display:flex;flex-direction:column;gap:8px;">
            <div v-if="!profile.consultations?.length" class="avo-soft muted">Консультаций пока не было.</div>
            <div
              v-for="c in profile.consultations"
              :key="c.id"
              class="avo-soft"
              style="display:flex;flex-direction:column;gap:6px;"
            >
              <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap;">
                <b style="font-size:0.86rem;">{{ formatDateTime(c.slot_start) }}</b>
                <div style="display:flex;gap:8px;align-items:center;">
                  <span class="badge" :class="c.status === 'completed' ? 'badge-green' : c.status === 'booked' ? 'badge-peach' : 'badge-soft'">
                    {{ c.status === 'completed' ? 'Завершена' : c.status === 'booked' ? 'Запланирована' : c.status }}
                  </span>
                  <router-link :to="`/consultations/${c.id}`" style="font-size:0.8rem;font-weight:600;color:var(--primary);">
                    Открыть рабочее место →
                  </router-link>
                </div>
              </div>
              <div v-if="c.client_question" class="muted" style="font-size:0.8rem;">
                <b>Запрос:</b> {{ c.client_question }}
              </div>
              <div v-if="c.doctor_notes" class="muted" style="font-size:0.78rem;">
                <b>Заметки врача:</b> {{ c.doctor_notes }}
              </div>
            </div>
          </div>

          <!-- Tab: Attachments / Lab files -->
          <div v-else-if="historyTab === 'attachments'" style="display:flex;flex-direction:column;gap:8px;">
            <div v-if="!profile.attachments?.length" class="avo-soft muted">Прикреплённых анализов пока нет.</div>
            <div
              v-for="a in profile.attachments"
              :key="a.id"
              class="avo-soft"
              style="display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap;"
            >
              <div>
                <b style="font-size:0.84rem;">{{ a.file_name }}</b>
                <div class="muted" style="font-size:0.76rem;">
                  {{ Math.round(a.file_size_bytes / 1024) }} КБ · загружен {{ formatDateTime(a.created_at) }} · слот #{{ a.slot_id }}
                </div>
              </div>
              <button type="button" class="btn btn-secondary btn-sm" @click="downloadAttachment(a.id)">
                Открыть файл
              </button>
            </div>
          </div>

          <!-- Tab: Documents / Prescriptions -->
          <div v-else-if="historyTab === 'documents'" style="display:flex;flex-direction:column;gap:8px;">
            <div v-if="!profile.documents?.length" class="avo-soft muted">Утверждённых заключений пока нет.</div>
            <div
              v-for="d in profile.documents"
              :key="d.id"
              class="avo-soft"
              style="display:flex;flex-direction:column;gap:6px;"
            >
              <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;">
                <b style="font-size:0.85rem;">{{ d.title }}</b>
                <span class="muted" style="font-size:0.76rem;">{{ formatDate(d.published_at || d.created_at) }}</span>
              </div>
              <div v-if="d.recommendations" style="font-size:0.82rem;white-space:pre-line;line-height:1.48;">
                {{ d.recommendations }}
              </div>
              <div v-if="d.follow_up" class="muted" style="font-size:0.78rem;">
                <b>Контроль:</b> {{ d.follow_up }}
              </div>
            </div>
          </div>

          <!-- Tab: Course Materials -->
          <div v-else-if="historyTab === 'materials'" style="display:flex;flex-direction:column;gap:6px;">
            <div v-if="!profile.materials?.length" class="avo-soft muted">Материалов нет.</div>
            <div
              v-for="m in profile.materials"
              :key="m.id"
              class="avo-soft"
              style="padding:10px 14px;display:flex;justify-content:space-between;align-items:center;gap:8px;"
            >
              <span style="font-size:0.83rem;">
                <b>{{ m.module_number }}.{{ m.lesson_number }}</b> {{ m.title }}
                <span v-if="m.completed_at" class="muted" style="font-size:0.75rem;">
                  ({{ formatDate(m.completed_at) }})
                </span>
              </span>
              <span class="badge" :class="m.completed ? 'badge-green' : 'badge-soft'">
                {{ m.completed ? 'Изучен ✓' : 'В процессе' }}
              </span>
            </div>
          </div>

          <!-- Tab: MAX Bot Questions -->
          <div v-else-if="historyTab === 'questions'" style="display:flex;flex-direction:column;gap:8px;">
            <div v-if="!profile.questions?.length" class="avo-soft muted">Вопросов из бота MAX не зафиксировано.</div>
            <div
              v-for="q in profile.questions"
              :key="q.id"
              class="avo-soft"
              style="display:flex;flex-direction:column;gap:4px;"
            >
              <div style="display:flex;justify-content:space-between;align-items:center;">
                <span class="muted" style="font-size:0.76rem;">{{ formatDateTime(q.created_at) }}</span>
                <span class="badge" :class="q.status === 'answered' ? 'badge-green' : 'badge-peach'">
                  {{ q.status === 'answered' ? 'Отвечено' : 'Новый' }}
                </span>
              </div>
              <div style="font-size:0.84rem;">{{ q.question_text }}</div>
              <div v-if="q.answer_text" class="muted" style="font-size:0.78rem;">
                <b>Ответ:</b> {{ q.answer_text }}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- =========================================================================
         MODAL DIALOG: MANUAL AI SUMMARY BUILDER (FULL / INCREMENTAL / CUSTOM)
         ========================================================================= -->
    <div v-if="showSummaryModal && profile" class="modal-backdrop" @click.self="showSummaryModal = false">
      <div class="modal-card">
        <div class="modal-header">
          <div>
            <h2 style="margin:0;font-size:1.18rem;">Сформировать ИИ-саммари профиля пациентки</h2>
            <p class="muted" style="margin:3px 0 0;font-size:0.8rem;">
              {{ profile.user.full_name }} ({{ profile.user.pregnancy_status || 'срок не указан' }}) ·
              Шаблон: <b>{{ summaryTemplate?.title || 'Клинический паспорт беременности «Авокадо» (6 разделов)' }}</b>
            </p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="showSummaryModal = false">✕</button>
        </div>

        <div class="modal-body">
          <!-- Step 1: Choose Generation Mode -->
          <div>
            <div style="font-size:0.84rem;font-weight:700;margin-bottom:8px;">1. Выберите режим формирования саммари</div>
            <div class="grid-3">
              <div
                class="mode-card"
                :class="{ active: modalForm.mode === 'full' }"
                @click="selectModalMode('full')"
              >
                <div style="display:flex;justify-content:space-between;align-items:center;">
                  <b style="font-size:0.85rem;">По всем материалам с нуля</b>
                  <span class="badge badge-green">Полный</span>
                </div>
                <p class="muted" style="margin:2px 0 0;font-size:0.76rem;line-height:1.4;">
                  Предыдущее саммари <b>не используется</b>. ИИ заново перечитывает и обрабатывает все материалы, встречи, анализы, выписки, чат и вопросы пациентки.
                </p>
              </div>

              <div
                class="mode-card"
                :class="{ active: modalForm.mode === 'incremental' }"
                @click="selectModalMode('incremental')"
              >
                <div style="display:flex;justify-content:space-between;align-items:center;">
                  <b style="font-size:0.85rem;">Предыдущее + новые</b>
                  <span class="badge badge-peach">+{{ unprocessedEventsCount }} новых</span>
                </div>
                <p class="muted" style="margin:2px 0 0;font-size:0.76rem;line-height:1.4;">
                  Берёт за основу предыдущее саммари и дополняет его только новыми материалами, которые ещё не обработаны (так же работает авто-сбор за 30 мин до слота).
                </p>
              </div>

              <div
                class="mode-card"
                :class="{ active: modalForm.mode === 'custom' }"
                @click="selectModalMode('custom')"
              >
                <div style="display:flex;justify-content:space-between;align-items:center;">
                  <b style="font-size:0.85rem;">По выбранным данным</b>
                  <span class="badge badge-soft">Фильтр</span>
                </div>
                <p class="muted" style="margin:2px 0 0;font-size:0.76rem;line-height:1.4;">
                  Ручной выбор разделов профиля, периода дат или конкретных уроков, встреч, анализов, назначений и сообщений.
                </p>
              </div>
            </div>
          </div>

          <!-- Step 2: Data Sources from Patient Profile -->
          <div class="avo-soft" style="display:flex;flex-direction:column;gap:12px;">
            <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;">
              <div style="font-size:0.84rem;font-weight:700;">
                2. Сведения из профиля пациентки
                <span v-if="modalForm.mode !== 'custom'" class="muted" style="font-weight:400;">
                  (в этом режиме собираются все 6 источников профиля)
                </span>
              </div>
              <label
                v-if="modalForm.mode === 'custom'"
                style="display:inline-flex;align-items:center;gap:6px;font-size:0.78rem;cursor:pointer;"
              >
                <input v-model="modalForm.use_previous_summary" type="checkbox" style="width:auto;" />
                <span>Использовать предыдущее саммари как базу</span>
              </label>
            </div>

            <div class="grid-3" style="gap:8px;font-size:0.8rem;">
              <label class="avo-card" style="margin:0;padding:10px 12px;border-radius:12px;display:flex;align-items:center;gap:8px;cursor:pointer;">
                <input
                  v-model="modalForm.event_types"
                  type="checkbox"
                  value="materials"
                  :disabled="modalForm.mode !== 'custom'"
                  style="width:auto;"
                />
                <span><b>Уроки курса</b> ({{ profile.materials?.length || 0 }})</span>
              </label>
              <label class="avo-card" style="margin:0;padding:10px 12px;border-radius:12px;display:flex;align-items:center;gap:8px;cursor:pointer;">
                <input
                  v-model="modalForm.event_types"
                  type="checkbox"
                  value="consultations"
                  :disabled="modalForm.mode !== 'custom'"
                  style="width:auto;"
                />
                <span><b>Консультации и заметки</b> ({{ profile.consultations?.length || 0 }})</span>
              </label>
              <label class="avo-card" style="margin:0;padding:10px 12px;border-radius:12px;display:flex;align-items:center;gap:8px;cursor:pointer;">
                <input
                  v-model="modalForm.event_types"
                  type="checkbox"
                  value="attachments"
                  :disabled="modalForm.mode !== 'custom'"
                  style="width:auto;"
                />
                <span><b>Файлы анализов</b> ({{ profile.attachments?.length || 0 }})</span>
              </label>
              <label class="avo-card" style="margin:0;padding:10px 12px;border-radius:12px;display:flex;align-items:center;gap:8px;cursor:pointer;">
                <input
                  v-model="modalForm.event_types"
                  type="checkbox"
                  value="documents"
                  :disabled="modalForm.mode !== 'custom'"
                  style="width:auto;"
                />
                <span><b>Назначения и выписки</b> ({{ profile.documents?.length || 0 }})</span>
              </label>
              <label class="avo-card" style="margin:0;padding:10px 12px;border-radius:12px;display:flex;align-items:center;gap:8px;cursor:pointer;">
                <input
                  v-model="modalForm.event_types"
                  type="checkbox"
                  value="chat"
                  :disabled="modalForm.mode !== 'custom'"
                  style="width:auto;"
                />
                <span><b>Внутренний чат</b> ({{ profile.messages?.length || 0 }})</span>
              </label>
              <label class="avo-card" style="margin:0;padding:10px 12px;border-radius:12px;display:flex;align-items:center;gap:8px;cursor:pointer;">
                <input
                  v-model="modalForm.event_types"
                  type="checkbox"
                  value="questions"
                  :disabled="modalForm.mode !== 'custom'"
                  style="width:auto;"
                />
                <span><b>Вопросы из MAX</b> ({{ profile.questions?.length || 0 }})</span>
              </label>
            </div>

            <!-- Date range & specific item checkboxes when in custom mode -->
            <div v-if="modalForm.mode === 'custom'" style="display:flex;flex-direction:column;gap:10px;padding-top:6px;border-top:1px solid var(--border);">
              <div style="display:flex;gap:12px;flex-wrap:wrap;align-items:center;font-size:0.8rem;">
                <label style="display:inline-flex;align-items:center;gap:6px;">
                  <span>Период с:</span>
                  <input v-model="modalForm.date_from" type="date" class="input" style="width:auto;padding:5px 10px;" />
                </label>
                <label style="display:inline-flex;align-items:center;gap:6px;">
                  <span>по:</span>
                  <input v-model="modalForm.date_to" type="date" class="input" style="width:auto;padding:5px 10px;" />
                </label>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="showItemPicker = !showItemPicker"
                >
                  {{ showItemPicker ? 'Скрыть точечный выбор записей' : `Выбрать конкретные записи (${selectedCount} отмечено)` }}
                </button>
                <button
                  v-if="selectedCount > 0"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="clearExplicitSelections"
                >
                  Сбросить отметки
                </button>
              </div>

              <!-- Expandable specific items selector inside modal -->
              <div
                v-if="showItemPicker"
                style="max-height:200px;overflow-y:auto;background:#ffffff;border-radius:12px;padding:10px 12px;border:1px solid var(--border);display:grid;gap:10px;font-size:0.78rem;"
              >
                <div v-if="profile.materials?.length">
                  <b>Уроки курса:</b>
                  <div v-for="m in profile.materials" :key="'m-' + m.id" style="margin-top:3px;">
                    <label style="display:inline-flex;gap:6px;align-items:center;cursor:pointer;">
                      <input v-model="selectedEvents.material_ids" type="checkbox" :value="m.id" style="width:auto;" />
                      <span>{{ m.module_number }}.{{ m.lesson_number }} {{ m.title }} ({{ m.completed ? 'пройден' : 'в плане' }})</span>
                    </label>
                  </div>
                </div>

                <div v-if="profile.consultations?.length">
                  <b>Встречи и консультации:</b>
                  <div v-for="c in profile.consultations" :key="'c-' + c.id" style="margin-top:3px;">
                    <label style="display:inline-flex;gap:6px;align-items:center;cursor:pointer;">
                      <input v-model="selectedEvents.consultation_ids" type="checkbox" :value="c.id" style="width:auto;" />
                      <span>{{ formatDateTime(c.slot_start) }} — {{ c.client_question || c.status }}</span>
                    </label>
                  </div>
                </div>

                <div v-if="profile.attachments?.length">
                  <b>Прикреплённые анализы:</b>
                  <div v-for="a in profile.attachments" :key="'a-' + a.id" style="margin-top:3px;">
                    <label style="display:inline-flex;gap:6px;align-items:center;cursor:pointer;">
                      <input v-model="selectedEvents.attachment_ids" type="checkbox" :value="a.id" style="width:auto;" />
                      <span>{{ a.file_name }} ({{ formatDate(a.created_at) }})</span>
                    </label>
                  </div>
                </div>

                <div v-if="profile.documents?.length">
                  <b>Назначения и выписки:</b>
                  <div v-for="d in profile.documents" :key="'d-' + d.id" style="margin-top:3px;">
                    <label style="display:inline-flex;gap:6px;align-items:center;cursor:pointer;">
                      <input v-model="selectedEvents.document_ids" type="checkbox" :value="d.id" style="width:auto;" />
                      <span>{{ d.title }} ({{ formatDate(d.published_at || d.created_at) }})</span>
                    </label>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Step 3: Preview of the 6-Section Clinical Template -->
          <div class="avo-flesh" style="padding:13px 16px;">
            <div style="font-size:0.82rem;font-weight:700;margin-bottom:6px;">
              3. Структура шаблона «{{ summaryTemplate?.title || 'Клинический паспорт беременности Авокадо' }}»
            </div>
            <div class="grid-2" style="gap:6px;font-size:0.76rem;">
              <div><b>1. Паспорт срока и статус ведения</b> — срок, тариф, прогресс уроков</div>
              <div><b>2. Что нового с прошлого саммари</b> — дельта новых событий</div>
              <div><b>3. Клиническая картина, анализы и назначения</b> — файлы, жалобы, выписки</div>
              <div><b>4. Коммуникация: чат и вопросы MAX</b> — переписка пациентки с врачом</div>
              <div><b>5. Освоение программы «Авокадо»</b> — изученные модули и следующий шаг</div>
              <div><b>6. Фокус внимания к следующей встрече</b> — акценты для врача</div>
            </div>
          </div>
        </div>

        <div class="modal-footer">
          <span class="muted" style="font-size:0.76rem;">
            Автоматическое инкрементальное саммари также запускается за 30 минут до каждой запланированной консультации.
          </span>
          <div style="display:flex;gap:8px;">
            <button type="button" class="btn btn-secondary" @click="showSummaryModal = false">
              Отмена
            </button>
            <button type="button" class="btn" :disabled="summaryLoading" @click="submitModalSummary">
              {{ summaryLoading ? 'Генерация по шаблону...' : 'Сформировать саммари по шаблону' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { apiFetch, formatDate, formatDateTime } from '../api'
import { parseSummarySections, getInitials } from '../utils/formatSummary'

const emit = defineEmits(['chat-updated'])

const route = useRoute()
const clientId = route.params.id

const profile = ref(null)
const customSummary = ref(null)
const preConsultationSummary = ref(null)
const summaryTemplate = ref(null)
const unprocessedEventsCount = ref(0)
const summaryLoading = ref(false)
const showSummaryModal = ref(false)
const showItemPicker = ref(false)
const historyTab = ref('consultations')
const errorMsg = ref('')
const noticeMsg = ref('')

const chatMessages = ref([])
const unreadFromClient = ref(0)
const chatDraft = ref('')
const chatSending = ref(false)
const chatBox = ref(null)
const chatInput = ref(null)
let chatPollTimer = null

const modalForm = ref({
  mode: 'full', // full | incremental | custom
  use_previous_summary: false,
  date_from: '',
  date_to: '',
  event_types: ['materials', 'consultations', 'attachments', 'documents', 'chat', 'questions'],
})

const selectedEvents = ref({
  material_ids: [],
  consultation_ids: [],
  attachment_ids: [],
  document_ids: [],
  chat_message_ids: [],
  question_ids: [],
})

const completedMaterialsCount = computed(() => (profile.value?.materials || []).filter((m) => m.completed).length)
const selectedCount = computed(
  () =>
    selectedEvents.value.material_ids.length +
    selectedEvents.value.consultation_ids.length +
    selectedEvents.value.attachment_ids.length +
    selectedEvents.value.document_ids.length +
    selectedEvents.value.chat_message_ids.length +
    selectedEvents.value.question_ids.length
)

const parsedSummary = computed(() => parseSummarySections(customSummary.value?.summary_text || ''))

const allAdminEventTypes = ['materials', 'consultations', 'attachments', 'documents', 'chat', 'questions']

function selectModalMode(mode) {
  modalForm.value.mode = mode
  if (mode === 'full') {
    modalForm.value.use_previous_summary = false
    modalForm.value.event_types = [...allAdminEventTypes]
  } else if (mode === 'incremental') {
    modalForm.value.use_previous_summary = true
    modalForm.value.event_types = [...allAdminEventTypes]
  }
}

function openSummaryModal() {
  selectModalMode(unprocessedEventsCount.value > 0 ? 'incremental' : 'full')
  showSummaryModal.value = true
}

async function scrollChat() {
  await nextTick()
  if (chatBox.value) {
    chatBox.value.scrollTop = chatBox.value.scrollHeight
  }
}

async function markChatRead() {
  try {
    await apiFetch(`/api/v1/admin/clients/${clientId}/chat/read`, { method: 'POST' })
    unreadFromClient.value = 0
    chatMessages.value = chatMessages.value.map((m) =>
      m.sender_role === 'client' ? { ...m, read_by_doctor: true } : m
    )
    emit('chat-updated')
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function loadChat(silent = false) {
  try {
    const prevCount = chatMessages.value.length
    const res = await apiFetch(`/api/v1/admin/clients/${clientId}/chat`)
    chatMessages.value = res.messages || []
    unreadFromClient.value = res.unread_by_doctor || 0
    if (chatMessages.value.length !== prevCount) {
      await scrollChat()
    }
    emit('chat-updated')
  } catch (e) {
    if (!silent) errorMsg.value = e.message
  }
}

async function sendChatMessage() {
  const body = chatDraft.value.trim()
  if (!body || chatSending.value) return
  chatSending.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch(`/api/v1/admin/clients/${clientId}/chat`, {
      method: 'POST',
      body: { body },
    })
    if (res.message) {
      chatMessages.value.push(res.message)
      unprocessedEventsCount.value++
    }
    chatDraft.value = ''
    unreadFromClient.value = 0
    chatMessages.value = chatMessages.value.map((m) =>
      m.sender_role === 'client' ? { ...m, read_by_doctor: true } : m
    )
    emit('chat-updated')
    await scrollChat()
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    chatSending.value = false
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

async function loadProfile() {
  errorMsg.value = ''
  try {
    const res = await apiFetch(`/api/v1/admin/clients/${clientId}/profile`)
    profile.value = res.profile
    customSummary.value = res.ai_summary
    preConsultationSummary.value = res.pre_consultation_summary || null
    summaryTemplate.value = res.summary_template || null
    unprocessedEventsCount.value = res.unprocessed_events_count || 0
    chatMessages.value = res.profile?.messages || []
    unreadFromClient.value = res.profile?.unread_by_doctor || 0
    await scrollChat()
    if (route.hash === '#chat') {
      await nextTick()
      document.getElementById('chat')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      chatInput.value?.focus()
      if (unreadFromClient.value > 0) {
        await markChatRead()
      }
    }
  } catch (e) {
    errorMsg.value = e.message
  }
}

async function runSummaryRequest(payload) {
  summaryLoading.value = true
  errorMsg.value = ''
  noticeMsg.value = ''
  try {
    const res = await apiFetch(`/api/v1/admin/clients/${clientId}/summary`, {
      method: 'POST',
      body: payload,
    })
    customSummary.value = res.summary
    unprocessedEventsCount.value = res.unprocessed_events_count ?? 0
    if (res.summary_template) summaryTemplate.value = res.summary_template
    showSummaryModal.value = false
    noticeMsg.value = `ИИ-саммари обновлено (${res.summary?.scope_label || 'по шаблону'}).`
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    summaryLoading.value = false
  }
}

async function submitModalSummary() {
  const mode = modalForm.value.mode
  if (mode === 'full') {
    await runSummaryRequest({ mode: 'full', use_previous_summary: false })
  } else if (mode === 'incremental') {
    await runSummaryRequest({ mode: 'incremental', use_previous_summary: true })
  } else {
    await runSummaryRequest({
      mode: 'custom',
      use_previous_summary: modalForm.value.use_previous_summary,
      date_from: modalForm.value.date_from,
      date_to: modalForm.value.date_to,
      event_types: modalForm.value.event_types,
      material_ids: selectedEvents.value.material_ids,
      consultation_ids: selectedEvents.value.consultation_ids,
      attachment_ids: selectedEvents.value.attachment_ids,
      document_ids: selectedEvents.value.document_ids,
      chat_message_ids: selectedEvents.value.chat_message_ids,
      question_ids: selectedEvents.value.question_ids,
    })
  }
}

async function quickIncrementalSummary() {
  await runSummaryRequest({ mode: 'incremental', use_previous_summary: true })
}

function clearExplicitSelections() {
  selectedEvents.value = {
    material_ids: [],
    consultation_ids: [],
    attachment_ids: [],
    document_ids: [],
    chat_message_ids: [],
    question_ids: [],
  }
}

onMounted(async () => {
  await loadProfile()
  chatPollTimer = setInterval(() => loadChat(true), 5000)
})

onUnmounted(() => {
  if (chatPollTimer) clearInterval(chatPollTimer)
})
</script>
