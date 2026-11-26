<template>
  <div style="display:flex;flex-direction:column;gap:20px;">
    <div v-if="errorMsg" class="alert alert-error" style="margin:0;">{{ errorMsg }}</div>

    <div v-if="data && !data.sub_active" class="alert alert-warn" style="margin:0;display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;">
      <div>
        <b>Срок вашего доступа истёк</b> (или тариф ещё не активирован). Продлите подписку, чтобы продолжить уроки и консультации.
      </div>
      <router-link class="btn btn-accent btn-sm" to="/profile">Продлить доступ</router-link>
    </div>

    <!-- Calm Hero Banner -->
    <div
      v-if="data"
      class="avo-flesh"
      style="display:flex;align-items:center;justify-content:space-between;gap:20px;flex-wrap:wrap;"
    >
      <div style="display:flex;align-items:flex-start;gap:18px;flex:1;min-width:280px;">
        <img
          :src="mascotUrl"
          alt="Авокадо"
          style="width:68px;height:68px;border-radius:20px;object-fit:cover;border:1px solid rgba(75,112,38,0.18);box-shadow:0 8px 20px -8px rgba(68,52,34,0.14);flex-shrink:0;"
        />
        <div style="display:flex;flex-direction:column;gap:8px;">
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <span class="badge badge-green">
              {{ data.user?.pregnancy_status || 'Беременность без тревоги' }}
            </span>
            <span class="badge badge-card">
              {{
                data.sub_active
                  ? `Доступ активен · ${data.subscription?.days_left ?? 0} дн.`
                  : 'Доступ ожидает продления'
              }}
            </span>
            <span v-if="data.subscription?.tariff_title" class="badge badge-soft">
              {{ data.subscription.tariff_title }}
            </span>
          </div>

          <h1 style="font-size:1.65rem;margin:0;">Добрый день, {{ firstName }}</h1>
          <p class="muted" style="margin:0;max-width:560px;font-size:0.92rem;">
            {{ heroSubtitle }}
          </p>

          <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding-top:4px;">
            <router-link to="/materials" class="btn">
              <span>{{ nextLessonCtaText }}</span>
            </router-link>
            <router-link to="/consultations" class="btn btn-card">
              {{ data.upcoming_consultation ? 'Прикрепить анализы к встрече' : 'Записаться к врачу' }}
            </router-link>
            <a
              v-if="data.consultation_config?.max_channel_invite_url"
              :href="data.consultation_config.max_channel_invite_url"
              target="_blank"
              class="btn btn-secondary btn-sm"
            >
              Закрытый канал в MAX
            </a>
          </div>
        </div>
      </div>

      <!-- Progress Ring Widget -->
      <div
        class="avo-card"
        style="margin:0;padding:16px 20px;border-radius:20px;display:flex;align-items:center;gap:16px;flex-shrink:0;"
      >
        <div style="position:relative;width:76px;height:76px;display:flex;align-items:center;justify-content:center;">
          <svg style="width:76px;height:76px;transform:rotate(-90deg);" viewBox="0 0 100 100">
            <circle cx="50" cy="50" r="40" fill="none" stroke="rgba(75,112,38,0.14)" stroke-width="10" />
            <circle
              cx="50"
              cy="50"
              r="40"
              fill="none"
              stroke="#4b7026"
              stroke-width="10"
              stroke-linecap="round"
              stroke-dasharray="251.2"
              :stroke-dashoffset="ringOffset"
              style="transition:stroke-dashoffset 0.5s ease;"
            />
          </svg>
          <span style="position:absolute;font-size:0.98rem;font-weight:700;">{{ progressPct }}%</span>
        </div>
        <div>
          <div class="muted" style="font-size:0.78rem;">Пройдено курса</div>
          <div style="font-size:1.02rem;font-weight:700;margin-top:2px;">
            {{ data.stats.completed_materials }} из {{ data.stats.total_materials }} уроков
          </div>
          <div class="muted" style="font-size:0.76rem;margin-top:2px;">Без спешки, в вашем темпе</div>
        </div>
      </div>
    </div>

    <!-- 2-Column Desktop Layout (7 cols left + 5 cols right) -->
    <div v-if="data" class="split-7-5">
      <!-- LEFT 7 COLS: Next Lessons & Structured Health Summary -->
      <div style="display:flex;flex-direction:column;gap:18px;">
        <!-- Nearest Program Lessons -->
        <div class="avo-card" style="margin:0;">
          <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:12px;">
            <h3 style="margin:0;">Ближайшие уроки программы</h3>
            <router-link to="/materials" style="font-size:0.8rem;font-weight:600;color:var(--primary);">
              Вся программа →
            </router-link>
          </div>

          <div v-if="previewLessons.length > 0" style="display:flex;flex-direction:column;gap:9px;">
            <div
              v-for="item in previewLessons"
              :key="item.id"
              class="avo-soft"
              style="display:flex;align-items:center;justify-content:space-between;gap:12px;"
              :style="item.isNext ? 'background:var(--primary-soft);border-color:rgba(75,112,38,0.22);' : ''"
            >
              <div>
                <div
                  style="font-size:0.75rem;font-weight:600;"
                  :style="item.isNext ? 'color:var(--primary);' : 'color:var(--muted);'"
                >
                  Модуль {{ item.module_number }} · Урок {{ item.lesson_number }} ·
                  {{ item.isNext ? 'Рекомендуем сейчас' : item.completed ? 'Пройдено' : 'В программе' }}
                </div>
                <div style="font-size:0.9rem;font-weight:700;margin-top:2px;">{{ item.title }}</div>
              </div>
              <router-link
                v-if="!item.completed"
                to="/materials"
                class="btn btn-sm"
                style="flex-shrink:0;"
              >
                Открыть
              </router-link>
              <span v-else class="badge badge-green" style="flex-shrink:0;">Изучен ✓</span>
            </div>
          </div>
          <div v-else class="avo-soft" style="display:flex;align-items:center;justify-content:space-between;gap:12px;">
            <div>
              <div style="font-size:0.88rem;font-weight:600;">Перейдите к модулям курса «Авокадо»</div>
              <div class="muted" style="font-size:0.78rem;">Уроки, видео и именные памятки по триместрам</div>
            </div>
            <router-link to="/materials" class="btn btn-sm">Открыть уроки</router-link>
          </div>
        </div>

        <!-- Structured AI Health Summary (NO raw <pre>, no technical model badges) -->
        <div class="avo-card" style="margin:0;">
          <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:14px;">
            <div>
              <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
                <h3 style="margin:0;">Личная памятка здоровья (ИИ-саммари профиля)</h3>
                <span v-if="summaryData?.generation_mode" class="badge badge-card">
                  {{ modeLabel(summaryData.generation_mode) }}
                </span>
                <span v-if="unprocessedEventsCount > 0" class="badge badge-peach">
                  +{{ unprocessedEventsCount }} новых событий не в саммари
                </span>
              </div>
              <p class="muted" style="margin:3px 0 0;font-size:0.8rem;">
                Шаблон: <b>{{ summaryTemplate?.title || 'Клинический паспорт беременности «Авокадо» (6 разделов)' }}</b>
                <span v-if="summaryData?.scope_label"> · {{ summaryData.scope_label }}</span>
                <span v-if="summaryData?.created_at"> · {{ formatDateTime(summaryData.created_at) }}</span>
              </p>
            </div>
            <div v-if="data.sub_active" style="display:flex;gap:8px;flex-wrap:wrap;">
              <button
                v-if="unprocessedEventsCount > 0 && summaryData"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="summaryLoading"
                @click="quickIncrementalSummary"
              >
                {{ summaryLoading ? 'Обновляем...' : `Дополнить новыми (+${unprocessedEventsCount})` }}
              </button>
              <button
                type="button"
                class="btn btn-sm"
                :disabled="summaryLoading"
                @click="openSummaryModal"
              >
                Собрать саммари (выбор данных)...
              </button>
            </div>
          </div>

          <div v-if="parsedSummarySections.length > 0" class="grid-2" style="gap:10px;">
            <div
              v-for="(sec, idx) in parsedSummarySections"
              :key="idx"
              class="avo-soft"
              style="display:flex;flex-direction:column;gap:6px;"
            >
              <div>
                <span class="badge" :class="idx % 2 === 1 ? 'badge-peach' : 'badge-green'">
                  {{ sectionBadgeLabel(sec.heading, idx) }}
                </span>
              </div>
              <div v-if="sec.heading" style="font-size:0.88rem;font-weight:700;margin-top:2px;">
                {{ sec.heading }}
              </div>
              <p
                v-for="(p, pIdx) in sec.paragraphs"
                :key="'p-' + pIdx"
                class="muted"
                style="margin:0;font-size:0.8rem;line-height:1.48;color:var(--text);"
              >
                {{ p }}
              </p>
              <ul
                v-if="sec.items.length"
                style="margin:2px 0 0;padding-left:16px;font-size:0.8rem;line-height:1.48;color:var(--muted);"
              >
                <li v-for="(it, iIdx) in sec.items" :key="'i-' + iIdx" style="margin-bottom:3px;">{{ it }}</li>
              </ul>
            </div>
          </div>
          <div v-else class="avo-soft muted" style="font-size:0.84rem;">
            Нажмите «Собрать саммари (выбор данных)...» — система соберёт структурированную памятку по всем материалам вашего профиля или выбранным разделам.
          </div>
        </div>
      </div>

      <!-- RIGHT 5 COLS: Doctor Support (1 Consultation Card + 1 Unified Chat Widget) -->
      <div style="display:flex;flex-direction:column;gap:18px;">
        <!-- Upcoming Consultation Card -->
        <div class="avo-card" style="margin:0;">
          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">
            <span class="muted" style="font-size:0.78rem;font-weight:600;">Ближайшая консультация</span>
            <span class="badge" :class="data.upcoming_consultation ? 'badge-green' : 'badge-soft'">
              {{ data.upcoming_consultation ? 'Подтверждена' : 'Свободная запись' }}
            </span>
          </div>

          <template v-if="data.upcoming_consultation">
            <div style="font-size:1.15rem;font-weight:700;">
              {{ formatDateTime(data.upcoming_consultation.slot_start) }}
            </div>
            <div class="muted" style="font-size:0.8rem;margin-top:3px;">
              {{ data.consultation_config?.doctor_name || 'Врач-терапевт, нутрициолог' }} · онлайн-встреча
            </div>
            <div style="display:flex;align-items:center;gap:8px;margin-top:14px;">
              <a
                v-if="data.upcoming_consultation.meeting_url"
                :href="data.upcoming_consultation.meeting_url"
                target="_blank"
                class="btn btn-sm"
                style="flex:1;"
              >
                Подключиться к встрече
              </a>
              <router-link to="/consultations" class="btn btn-secondary btn-sm">
                Анализы и детали
              </router-link>
            </div>
          </template>
          <template v-else>
            <div style="font-size:1rem;font-weight:700;">Онлайн-разбор с врачом</div>
            <p class="muted" style="margin:4px 0 12px;font-size:0.8rem;">
              Выберите удобное окно для разбора анализов и самочувствия. Утверждённых заключений:
              <b>{{ data.stats.documents_count }}</b>.
            </p>
            <div style="display:flex;gap:8px;flex-wrap:wrap;">
              <router-link to="/consultations" class="btn btn-sm">Выбрать время записи</router-link>
              <router-link to="/documents" class="btn btn-secondary btn-sm">
                Заключения ({{ data.stats.documents_count }})
              </router-link>
            </div>
          </template>
        </div>

        <!-- Unified Doctor Chat Widget (replaces duplicate banners) -->
        <div class="avo-card" style="margin:0;display:flex;flex-direction:column;gap:12px;">
          <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:8px;">
            <div>
              <h3 style="margin:0;line-height:1.25;">Личный чат с врачом</h3>
              <div class="muted" style="font-size:0.78rem;margin-top:2px;">
                Быстрый вопрос без записи на звонок
              </div>
            </div>
            <span v-if="data.chat_unread_count > 0" class="badge badge-peach">
              {{ data.chat_unread_count }} новое
            </span>
          </div>

          <div class="avo-soft" style="display:flex;flex-direction:column;gap:8px;">
            <template v-if="recentChatMessages.length > 0">
              <div
                v-for="msg in recentChatMessages"
                :key="msg.id"
                style="padding:10px 12px;border-radius:12px;font-size:0.81rem;line-height:1.45;"
                :style="
                  msg.sender_role === 'client'
                    ? 'background:var(--primary-soft);color:var(--primary-text);'
                    : 'background:#ffffff;border:1px solid rgba(0,0,0,0.05);'
                "
              >
                <div
                  style="font-weight:700;font-size:0.75rem;margin-bottom:2px;"
                  :style="msg.sender_role === 'client' ? '' : 'color:var(--primary);'"
                >
                  {{ msg.sender_role === 'client' ? 'Вы' : (msg.sender_name || chatDoctorName || 'Врач') }} ·
                  <span class="muted" style="font-weight:400;font-size:0.72rem;">{{ formatDateTime(msg.created_at) }}</span>
                </div>
                <div style="white-space:pre-wrap;">{{ msg.body }}</div>
              </div>
            </template>
            <div v-else class="muted" style="font-size:0.8rem;">
              Напишите врачу по самочувствию, анализам или материалам курса — ответ придёт сюда.
            </div>
          </div>

          <form @submit.prevent="sendQuickChat" style="display:flex;gap:8px;align-items:center;">
            <input
              v-model="quickChatDraft"
              type="text"
              class="input input-soft"
              placeholder="Ответить врачу..."
              style="padding:9px 12px;font-size:0.82rem;"
            />
            <button type="submit" class="btn btn-sm" :disabled="quickChatSending || !quickChatDraft.trim()">
              {{ quickChatSending ? '...' : 'Отправить' }}
            </button>
          </form>

          <router-link to="/chat" style="font-size:0.8rem;font-weight:600;color:var(--accent);">
            Открыть всю историю переписки →
          </router-link>
        </div>
      </div>
    </div>

    <!-- Modal Window for Manual Patient Summary Generation -->
    <div v-if="showSummaryModal" class="modal-backdrop" @click.self="showSummaryModal = false">
      <div class="modal-card">
        <div class="modal-header">
          <div>
            <span class="badge badge-green">Единый шаблон ИИ-саммари «Авокадо»</span>
            <h3 style="margin:6px 0 0;font-size:1.2rem;">Собрать личную памятку здоровья</h3>
            <p class="muted" style="margin:3px 0 0;font-size:0.82rem;">
              Выберите режим: полный пересчёт всех материалов с нуля, дополнение предыдущего саммари новыми событиями или выборочно по разделам.
            </p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="showSummaryModal = false">✕</button>
        </div>

        <div class="modal-body">
          <!-- 1. Generation Mode Cards -->
          <div>
            <div style="font-size:0.85rem;font-weight:700;margin-bottom:8px;">1. Режим формирования саммари</div>
            <div class="grid-3">
              <div
                class="mode-card"
                :class="{ active: summaryForm.mode === 'full' }"
                @click="selectModalMode('full')"
              >
                <div style="display:flex;align-items:center;justify-content:space-between;gap:6px;">
                  <span style="font-weight:700;font-size:0.88rem;">По всем материалам с нуля</span>
                  <span class="badge badge-green">Полное</span>
                </div>
                <p class="muted" style="margin:6px 0 0;font-size:0.77rem;line-height:1.4;">
                  Предыдущее саммари <b>не используется</b>. Система заново перечитывает и обрабатывает все материалы вашего профиля.
                </p>
              </div>

              <div
                class="mode-card"
                :class="{ active: summaryForm.mode === 'incremental' }"
                @click="selectModalMode('incremental')"
              >
                <div style="display:flex;align-items:center;justify-content:space-between;gap:6px;">
                  <span style="font-weight:700;font-size:0.88rem;">Предыдущее + новые</span>
                  <span class="badge badge-peach">+{{ unprocessedEventsCount }} новых</span>
                </div>
                <p class="muted" style="margin:6px 0 0;font-size:0.77rem;line-height:1.4;">
                  Берёт за основу прошлое саммари и дополняет его только новыми необработанными уроками, встречами и анализами.
                </p>
              </div>

              <div
                class="mode-card"
                :class="{ active: summaryForm.mode === 'custom' }"
                @click="selectModalMode('custom')"
              >
                <div style="display:flex;align-items:center;justify-content:space-between;gap:6px;">
                  <span style="font-weight:700;font-size:0.88rem;">По выбранным данным</span>
                  <span class="badge badge-card">Выборочно</span>
                </div>
                <p class="muted" style="margin:6px 0 0;font-size:0.77rem;line-height:1.4;">
                  Ручной выбор разделов профиля, периода дат и конкретных уроков, консультаций, анализов или заключений.
                </p>
              </div>
            </div>
          </div>

          <!-- 2. Profile Categories & Dates -->
          <div class="avo-soft" style="display:flex;flex-direction:column;gap:12px;">
            <div style="display:flex;align-items:center;justify-content:space-between;gap:8px;flex-wrap:wrap;">
              <div style="font-size:0.85rem;font-weight:700;">2. Какие данные профиля включить в саммари</div>
              <label
                v-if="summaryForm.mode === 'custom' && summaryData?.id"
                style="display:inline-flex;align-items:center;gap:6px;font-size:0.78rem;cursor:pointer;"
              >
                <input v-model="summaryForm.use_previous_summary" type="checkbox" style="width:auto;" />
                <span>Учитывать предыдущее саммари как базу</span>
              </label>
            </div>

            <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(200px, 1fr));gap:8px;font-size:0.82rem;">
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
                <input v-model="summaryForm.event_types" type="checkbox" value="materials" :disabled="summaryForm.mode === 'full'" style="width:auto;" />
                <span>Уроки курса ({{ materials.length }})</span>
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
                <input v-model="summaryForm.event_types" type="checkbox" value="consultations" :disabled="summaryForm.mode === 'full'" style="width:auto;" />
                <span>Консультации ({{ (data?.consultations || []).length }})</span>
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
                <input v-model="summaryForm.event_types" type="checkbox" value="attachments" :disabled="summaryForm.mode === 'full'" style="width:auto;" />
                <span>Файлы анализов ({{ (data?.attachments || []).length }})</span>
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
                <input v-model="summaryForm.event_types" type="checkbox" value="documents" :disabled="summaryForm.mode === 'full'" style="width:auto;" />
                <span>Заключения врача ({{ (data?.documents || []).length }})</span>
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
                <input v-model="summaryForm.event_types" type="checkbox" value="chat" :disabled="summaryForm.mode === 'full'" style="width:auto;" />
                <span>Переписка с врачом ({{ chatMessages.length }})</span>
              </label>
            </div>

            <!-- Date range & item selection for Custom mode -->
            <div v-if="summaryForm.mode === 'custom'" style="display:flex;flex-direction:column;gap:10px;padding-top:8px;border-top:1px solid var(--border);">
              <div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap;font-size:0.81rem;">
                <span class="muted">Период событий (необязательно):</span>
                <label style="display:inline-flex;align-items:center;gap:6px;">
                  <span>С:</span>
                  <input v-model="summaryForm.date_from" type="date" class="input" style="width:auto;padding:5px 9px;font-size:0.8rem;" />
                </label>
                <label style="display:inline-flex;align-items:center;gap:6px;">
                  <span>По:</span>
                  <input v-model="summaryForm.date_to" type="date" class="input" style="width:auto;padding:5px 9px;font-size:0.8rem;" />
                </label>
              </div>

              <details style="font-size:0.81rem;">
                <summary style="cursor:pointer;font-weight:600;color:var(--primary);">
                  Точечный выбор конкретных уроков, встреч, анализов или заключений (опционально)
                </summary>
                <div class="grid-2" style="margin-top:10px;max-height:200px;overflow-y:auto;gap:10px;">
                  <div v-if="materials.length">
                    <div style="font-weight:700;margin-bottom:4px;">Уроки курса:</div>
                    <label
                      v-for="m in materials"
                      :key="'m-' + m.id"
                      style="display:flex;align-items:center;gap:6px;margin-bottom:4px;font-size:0.77rem;"
                    >
                      <input v-model="summaryForm.material_ids" type="checkbox" :value="m.id" style="width:auto;" />
                      <span>Урок {{ m.lesson_number }}: {{ m.title }} ({{ m.completed ? 'изучен' : 'в плане' }})</span>
                    </label>
                  </div>
                  <div>
                    <div v-if="(data?.consultations || []).length" style="margin-bottom:8px;">
                      <div style="font-weight:700;margin-bottom:4px;">Консультации:</div>
                      <label
                        v-for="c in data.consultations"
                        :key="'c-' + c.id"
                        style="display:flex;align-items:center;gap:6px;margin-bottom:4px;font-size:0.77rem;"
                      >
                        <input v-model="summaryForm.consultation_ids" type="checkbox" :value="c.id" style="width:auto;" />
                        <span>{{ formatDateTime(c.slot_start) }} ({{ c.status }})</span>
                      </label>
                    </div>
                    <div v-if="(data?.attachments || []).length" style="margin-bottom:8px;">
                      <div style="font-weight:700;margin-bottom:4px;">Файлы анализов:</div>
                      <label
                        v-for="a in data.attachments"
                        :key="'a-' + a.id"
                        style="display:flex;align-items:center;gap:6px;margin-bottom:4px;font-size:0.77rem;"
                      >
                        <input v-model="summaryForm.attachment_ids" type="checkbox" :value="a.id" style="width:auto;" />
                        <span>{{ a.file_name }}</span>
                      </label>
                    </div>
                    <div v-if="(data?.documents || []).length">
                      <div style="font-weight:700;margin-bottom:4px;">Заключения врача:</div>
                      <label
                        v-for="d in data.documents"
                        :key="'d-' + d.id"
                        style="display:flex;align-items:center;gap:6px;margin-bottom:4px;font-size:0.77rem;"
                      >
                        <input v-model="summaryForm.document_ids" type="checkbox" :value="d.id" style="width:auto;" />
                        <span>{{ d.title }}</span>
                      </label>
                    </div>
                  </div>
                </div>
              </details>
            </div>
          </div>

          <!-- 3. Template Preview -->
          <div class="avo-soft" style="background:rgba(75,112,38,0.05);border:1px solid rgba(75,112,38,0.16);">
            <div style="font-size:0.82rem;font-weight:700;margin-bottom:6px;">
              3. Структура шаблона: {{ summaryTemplate?.title || 'Клинический паспорт беременности «Авокадо» (6 разделов)' }}
            </div>
            <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(230px, 1fr));gap:6px;font-size:0.76rem;" class="muted">
              <div v-for="(sec, idx) in templateSectionsList" :key="idx">
                • <b>{{ sec }}</b>
              </div>
            </div>
          </div>
        </div>

        <div class="modal-footer">
          <button type="button" class="btn btn-secondary btn-sm" @click="showSummaryModal = false">
            Отмена
          </button>
          <button type="button" class="btn btn-sm" :disabled="summaryLoading" @click="submitModalSummary">
            {{
              summaryLoading
                ? 'Формируем саммари по шаблону...'
                : summaryForm.mode === 'full'
                  ? 'Пересчитать по всем материалам с нуля'
                  : summaryForm.mode === 'incremental'
                    ? 'Обновить (предыдущее + новые материалы)'
                    : 'Сформировать по выбранным данным'
            }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { apiFetch, formatDateTime } from '../api'
import { parseSummarySections } from '../utils/formatSummary'
import mascotUrl from '../assets/avocado-mascot.jpg'

const allPatientEventTypes = ['materials', 'consultations', 'attachments', 'documents', 'chat']

const data = ref(null)
const materials = ref([])
const chatMessages = ref([])
const chatDoctorName = ref('')
const quickChatDraft = ref('')
const quickChatSending = ref(false)
const showSummaryModal = ref(false)
const errorMsg = ref('')
const summaryData = ref(null)
const summaryTemplate = ref(null)
const unprocessedEventsCount = ref(0)
const summaryLoading = ref(false)
const summaryForm = ref({
  mode: 'full',
  use_previous_summary: false,
  date_from: '',
  date_to: '',
  event_types: [...allPatientEventTypes],
  material_ids: [],
  consultation_ids: [],
  attachment_ids: [],
  document_ids: [],
})

const templateSectionsList = computed(() => {
  if (summaryTemplate.value?.sections?.length) {
    return summaryTemplate.value.sections
  }
  return [
    '1. Паспорт срока и статус ведения',
    '2. Что нового с прошлого саммари (Динамика)',
    '3. Клиническая картина, анализы и назначения',
    '4. Коммуникация: чат с врачом и вопросы',
    '5. Освоение программы курса «Авокадо»',
    '6. Фокус внимания и план к следующей встрече',
  ]
})

const firstName = computed(() => {
  const full = String(data.value?.user?.full_name || '').trim()
  if (!full) return 'участница'
  const parts = full.split(/\s+/)
  return parts.length > 1 && parts[0].endsWith('а') && parts[1] ? parts[1] : parts[0]
})

const progressPct = computed(() => {
  if (!data.value?.stats?.total_materials) return 0
  return Math.round((data.value.stats.completed_materials / data.value.stats.total_materials) * 100)
})

const ringOffset = computed(() => {
  const circ = 251.2
  return Number((circ - (circ * progressPct.value) / 100).toFixed(1))
})

const nextMaterial = computed(() => {
  return materials.value.find((m) => !m.completed) || materials.value[0] || null
})

const nextLessonCtaText = computed(() => {
  if (nextMaterial.value) {
    return `Продолжить: Урок ${nextMaterial.value.lesson_number} · ${nextMaterial.value.title} →`
  }
  return 'Перейти к урокам программы →'
})

const heroSubtitle = computed(() => {
  if (nextMaterial.value && data.value?.upcoming_consultation) {
    return `Ваш спокойный фокус сегодня — пройти урок «${nextMaterial.value.title}» перед консультацией ${formatDateTime(data.value.upcoming_consultation.slot_start)}.`
  }
  if (nextMaterial.value) {
    return `Рекомендуемый следующий шаг — «${nextMaterial.value.title}» (Модуль ${nextMaterial.value.module_number}).`
  }
  return 'Интенсив «Беременность без тревоги» — ваш личный маршрут здоровья и спокойствия.'
})

const previewLessons = computed(() => {
  if (!materials.value.length) return []
  const nextId = nextMaterial.value?.id
  return materials.value.slice(0, 3).map((m) => ({
    ...m,
    isNext: m.id === nextId && !m.completed,
  }))
})

const parsedSummarySections = computed(() => {
  return parseSummarySections(summaryData.value?.summary_text || '')
})

const recentChatMessages = computed(() => {
  if (!chatMessages.value.length) return []
  return chatMessages.value.slice(-2)
})

function modeLabel(mode) {
  switch (mode) {
    case 'full':
      return 'Полное по всем материалам'
    case 'incremental':
      return 'Предыдущее + новые материалы'
    case 'pre_consultation':
      return 'Авто за 30 мин до приёма'
    case 'custom':
      return 'По выбранным данным'
    default:
      return 'ИИ-саммари'
  }
}

function sectionBadgeLabel(heading, idx) {
  const h = String(heading || '').toLowerCase()
  if (h.includes('паспорт') || h.includes('срок')) return 'Паспорт срока'
  if (h.includes('нового') || h.includes('динамик')) return 'Динамика'
  if (h.includes('клинич') || h.includes('анализ') || h.includes('назнач')) return 'Анализы и назначения'
  if (h.includes('коммуник') || h.includes('чат')) return 'Чат с врачом'
  if (h.includes('освоен') || h.includes('курс') || h.includes('урок')) return 'Программа курса'
  if (h.includes('фокус') || h.includes('план')) return 'План к встрече'
  const labels = ['Паспорт срока', 'Динамика', 'Анализы и назначения', 'Чат с врачом', 'Программа курса', 'План к встрече']
  return labels[idx % labels.length]
}

function openSummaryModal() {
  if (summaryData.value?.id && unprocessedEventsCount.value > 0) {
    selectModalMode('incremental')
  } else {
    selectModalMode('full')
  }
  showSummaryModal.value = true
}

function selectModalMode(mode) {
  summaryForm.value.mode = mode
  if (mode === 'full') {
    summaryForm.value.use_previous_summary = false
    summaryForm.value.date_from = ''
    summaryForm.value.date_to = ''
    summaryForm.value.event_types = [...allPatientEventTypes]
    summaryForm.value.material_ids = []
    summaryForm.value.consultation_ids = []
    summaryForm.value.attachment_ids = []
    summaryForm.value.document_ids = []
  } else if (mode === 'incremental') {
    summaryForm.value.use_previous_summary = true
    summaryForm.value.date_from = ''
    summaryForm.value.date_to = ''
    summaryForm.value.event_types = [...allPatientEventTypes]
  }
}

async function runSummaryRequest(payload) {
  summaryLoading.value = true
  errorMsg.value = ''
  try {
    const res = await apiFetch('/api/v1/cabinet/summary', {
      method: 'POST',
      body: payload,
    })
    summaryData.value = res.summary
    if (typeof res.unprocessed_events_count === 'number') {
      unprocessedEventsCount.value = res.unprocessed_events_count
    }
    if (res.summary_template) {
      summaryTemplate.value = res.summary_template
    }
    showSummaryModal.value = false
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    summaryLoading.value = false
  }
}

async function submitModalSummary() {
  await runSummaryRequest({
    mode: summaryForm.value.mode,
    use_previous_summary: summaryForm.value.use_previous_summary,
    date_from: summaryForm.value.date_from,
    date_to: summaryForm.value.date_to,
    event_types: summaryForm.value.event_types,
    material_ids: summaryForm.value.material_ids,
    consultation_ids: summaryForm.value.consultation_ids,
    attachment_ids: summaryForm.value.attachment_ids,
    document_ids: summaryForm.value.document_ids,
  })
}

async function quickIncrementalSummary() {
  await runSummaryRequest({
    mode: 'incremental',
    use_previous_summary: true,
    event_types: [...allPatientEventTypes],
  })
}

async function sendQuickChat() {
  const body = quickChatDraft.value.trim()
  if (!body || quickChatSending.value) return
  quickChatSending.value = true
  try {
    const res = await apiFetch('/api/v1/cabinet/chat', {
      method: 'POST',
      body: { body },
    })
    if (res?.message) {
      chatMessages.value.push(res.message)
      unprocessedEventsCount.value += 1
    }
    quickChatDraft.value = ''
  } catch (e) {
    errorMsg.value = e.message
  } finally {
    quickChatSending.value = false
  }
}

onMounted(async () => {
  try {
    data.value = await apiFetch('/api/v1/cabinet/dashboard')
    summaryData.value = data.value?.completed_summary || null
    unprocessedEventsCount.value = data.value?.unprocessed_events_count || 0
    summaryTemplate.value = data.value?.summary_template || null
    if (data.value?.sub_active) {
      const [mRes, cRes] = await Promise.allSettled([
        apiFetch('/api/v1/cabinet/materials'),
        apiFetch('/api/v1/cabinet/chat'),
      ])
      if (mRes.status === 'fulfilled') {
        materials.value = mRes.value.materials || []
      }
      if (cRes.status === 'fulfilled') {
        chatMessages.value = cRes.value.messages || []
        chatDoctorName.value = cRes.value.doctor_name || ''
      }
    }
  } catch (e) {
    errorMsg.value = e.message
  }
})
</script>
