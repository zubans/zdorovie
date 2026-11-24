package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"zdorovie/backend/internal/ai"
	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/maxbot"
	"zdorovie/backend/internal/yukassa"
)

// Runner executes periodic background tasks: payment reconciliation, subscription expiry reminders,
// and upcoming consultation notifications (24h / 2h).
type Runner struct {
	cfg   config.Config
	store *db.Store
	yoo   *yukassa.Service
	bot   maxbot.Messenger
}

// New creates a new background scheduler runner.
func New(cfg config.Config, store *db.Store, yoo *yukassa.Service, bot maxbot.Messenger) *Runner {
	return &Runner{cfg: cfg, store: store, yoo: yoo, bot: bot}
}

// Start launches the scheduler loop in the background until ctx is canceled.
func (r *Runner) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		// Short initial delay so DB migrations finish before first tick
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
		r.Tick(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.Tick(ctx)
			}
		}
	}()
}

// Tick runs one cycle of reconciliation and notification checks.
func (r *Runner) Tick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	if r.yoo != nil {
		r.yoo.ReconcilePending(tickCtx)
	}
	r.processSubscriptionReminders(tickCtx)
	r.processConsultationReminders(tickCtx)
	r.processPreConsultationSummaries(tickCtx)
}

// processPreConsultationSummaries automatically builds an incremental AI summary 30 minutes before a booked consultation
// based on the patient's previous summary (if any) and newly unprocessed materials/events.
func (r *Runner) processPreConsultationSummaries(ctx context.Context) {
	if r.store == nil {
		return
	}
	dueSlots, err := r.store.FindSlotsDueForPreConsultationSummary(ctx)
	if err != nil {
		log.Printf("find slots due for 30m pre-consultation summary failed: %v", err)
		return
	}
	for _, sl := range dueSlots {
		if sl.ClientID == nil || *sl.ClientID <= 0 {
			continue
		}
		clientID := *sl.ClientID
		profile, err := r.store.GetClientProfile(ctx, clientID)
		if err != nil || profile == nil {
			log.Printf("pre-consultation summary: load profile client=%d slot=%d failed: %v", clientID, sl.ID, err)
			continue
		}
		prev, _ := r.store.GetLatestPatientAISummary(ctx, clientID, "doctor")
		slotCopy := sl
		pCtx, err := ai.BuildPatientAIContext(
			profile,
			ai.SummaryFilterRequest{Mode: "pre_consultation", SlotID: &slotCopy.ID},
			prev,
			&slotCopy,
			false,
		)
		if err != nil {
			log.Printf("pre-consultation summary: build context slot=%d failed: %v", sl.ID, err)
			continue
		}
		aiOut, err := ai.GeneratePatientSummary(ctx, r.cfg, pCtx)
		if err != nil {
			log.Printf("pre-consultation summary: generate slot=%d failed: %v", sl.ID, err)
			continue
		}
		_, err = r.store.SavePatientAISummary(ctx, db.PatientAISummary{
			ClientID:           clientID,
			SlotID:             &slotCopy.ID,
			Audience:           "doctor",
			GenerationMode:     "pre_consultation",
			TemplateCode:       aiOut.TemplateCode,
			ScopeLabel:         aiOut.ScopeLabel,
			SummaryText:        aiOut.SummaryText,
			ProcessedEventKeys: aiOut.ProcessedEventKeys,
			EventsCount:        aiOut.EventsCount,
			NewEventsCount:     aiOut.NewEventsCount,
			BaseSummaryID:      aiOut.BaseSummaryID,
			LLMModelName:       aiOut.LLMModelName,
			CoveredUntilAt:     time.Now().UTC(),
		})
		if err != nil {
			log.Printf("pre-consultation summary: save slot=%d failed: %v", sl.ID, err)
			continue
		}
		_ = r.store.MarkSlotPreSummaryGenerated(ctx, sl.ID)
		log.Printf("generated 30m pre-consultation AI summary for slot=%d client=%d (new_events=%d total_events=%d)",
			sl.ID, clientID, aiOut.NewEventsCount, aiOut.EventsCount)
	}
}

func (r *Runner) processSubscriptionReminders(ctx context.Context) {
	due, err := r.store.FindDueSubscriptionReminders(ctx)
	if err != nil {
		log.Printf("find subscription reminders failed: %v", err)
		return
	}
	msk := time.FixedZone("MSK", 3*3600)
	renewURL := strings.TrimRight(r.cfg.PublicBaseURL, "/") + "/cabinet/profile"

	for _, t := range due {
		expStr := t.ExpiresAt.In(msk).Format("02.01.2006")
		subject := fmt.Sprintf("Напоминание: доступ к интенсиву истекает через %d дн.", t.DaysWindow)
		body := fmt.Sprintf(
			"Здравствуйте, %s!\n\n"+
				"Срок вашей подписки на интенсив «Беременность без тревоги» заканчивается %s (осталось около %d дн.).\n"+
				"Вы можете продлить доступ со скидкой или выбрать новый тариф в личном кабинете:\n%s\n",
			t.FullName, expStr, t.DaysWindow, renewURL,
		)
		if r.yoo != nil {
			r.yoo.DeliverEmail(t.Email, subject, body)
		}
		if t.MaxUserID != "" && r.bot != nil {
			msg := maxbot.OutgoingMessage{
				Text: fmt.Sprintf(
					"⏳ *Напоминание о подписке*\n\nДоступ к материалам интенсива действует до *%s* (осталось %d дн.). Продлите доступ в личном кабинете, чтобы сохранить материалы и консультации.",
					expStr, t.DaysWindow,
				),
				Attachments: maxbot.InlineKeyboard(
					[]maxbot.Button{{Type: "link", Text: "🔄 Продлить в кабинете", URL: renewURL}},
				),
			}
			_ = r.bot.SendMessage(ctx, t.MaxUserID, msg)
		}
		_ = r.store.MarkSubscriptionReminderSent(ctx, t.SubscriptionID, t.DaysWindow)
	}
}

func (r *Runner) processConsultationReminders(ctx context.Context) {
	due, err := r.store.FindDueConsultationReminders(ctx)
	if err != nil {
		log.Printf("find consultation reminders failed: %v", err)
		return
	}
	msk := time.FixedZone("MSK", 3*3600)

	for _, t := range due {
		startStr := t.SlotStart.In(msk).Format("02.01.2006 в 15:04 (МСК)")
		meetingInfo := "Ссылка на видеовстречу появится в вашем личном кабинете ближе к началу."
		if t.MeetingURL != "" {
			meetingInfo = "Ссылка для подключения к видеовстрече (Яндекс Телемост / VK Звонки): " + t.MeetingURL
		}
		subject := fmt.Sprintf("Напоминание о консультации с врачом (%s)", startStr)
		body := fmt.Sprintf(
			"Здравствуйте, %s!\n\n"+
				"Напоминаем, что ваша индивидуальная консультация состоится %s (осталось ~%d ч).\n\n%s\n\n"+
				"Вы можете заранее прикрепить результаты анализов и вопрос в разделе «Консультация с врачом» личного кабинета.\n",
			t.FullName, startStr, t.HoursWin, meetingInfo,
		)
		if r.yoo != nil {
			r.yoo.DeliverEmail(t.Email, subject, body)
		}
		if t.MaxUserID != "" && r.bot != nil {
			var rows [][]maxbot.Button
			if t.MeetingURL != "" {
				rows = append(rows, []maxbot.Button{{Type: "link", Text: "📹 Подключиться к встрече", URL: t.MeetingURL}})
			}
			_ = r.bot.SendMessage(ctx, t.MaxUserID, maxbot.OutgoingMessage{
				Text: fmt.Sprintf(
					"🩺 *Напоминание о консультации*\n\nЖдём вас *%s* (через ~%d ч).\n%s",
					startStr, t.HoursWin, maxbot.EscapeMarkdown(meetingInfo),
				),
				Attachments: maxbot.InlineKeyboard(rows...),
			})
		}
		_ = r.store.MarkConsultationReminderSent(ctx, t.SlotID, t.HoursWin)
	}
}
