package maxbot

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/db"
)

func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func isSafePublicHTTPSURL(ctx context.Context, rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !isPrivateOrLocalIP(ip)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip.IP) {
			return false
		}
	}
	return true
}

func isAllowedBotUploadExt(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".pdf", ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func sanitizeUploadFileName(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	replacer := strings.NewReplacer(`"`, "", `'`, "", "\n", "", "\r", "", "\t", "_", " ", "_")
	clean := replacer.Replace(base)
	if clean == "" || clean == "." || clean == ".." {
		return "analysis.pdf"
	}
	return clean
}

func mskZone() *time.Location {
	return time.FixedZone("MSK", 3*3600)
}

func (e *Engine) showTariffsMenu(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	tariffs, err := e.store.ListTariffs(ctx, true)
	if err != nil {
		return err
	}
	sess, err := e.loadOrInitSession(ctx, uid, username)
	if err != nil {
		return err
	}
	sess.CurrentState = StateTariffs
	_ = e.store.SaveFSMSession(ctx, sess)

	var selfBtns, fbBtns [][]Button
	for _, t := range tariffs {
		btn := Button{
			Type:    "callback",
			Text:    fmt.Sprintf("%s — %d ₽", shortenTariffTitle(t), t.PriceKopecks/100),
			Payload: "buy:" + t.Code,
		}
		if t.HasFeedback {
			fbBtns = append(fbBtns, []Button{btn})
		} else {
			selfBtns = append(selfBtns, []Button{btn})
		}
	}
	var rows [][]Button
	rows = append(rows, selfBtns...)
	rows = append(rows, fbBtns...)
	rows = append(rows, []Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}})

	header := "*Стоимость и тарифы интенсива*\n\n"
	if _, sub, subActive := e.resolveLinkedUser(ctx, uid); sub != nil {
		if subActive {
			header = fmt.Sprintf("*Продление или смена подписки*\nВаш текущий тариф: *%s* (активен до %s, осталось %d дн.). При оплате нового тарифа срок доступа суммируется.\n\n",
				EscapeMarkdown(sub.TariffName),
				sub.ExpiresAt.In(mskZone()).Format("02.01.2006"),
				sub.DaysLeft,
			)
		} else {
			header = fmt.Sprintf("*Возобновление подписки*\nВаш предыдущий тариф: *%s* (срок истёк). Выберите тариф для активации доступа:\n\n",
				EscapeMarkdown(sub.TariffName),
			)
		}
	}

	msg := OutgoingMessage{
		Text: header +
			"1️⃣ *Самостоятельный* — доступ ко всем урокам, видео, чек-листам и персонализированным PDF-памяткам.\n" +
			"2️⃣ *С обратной связью* — всё из тарифа «Самостоятельный» + вопросы врачу, разбор анализов и персональная онлайн-консультация с официальным заключением.\n\nВыберите подходящий тариф и срок доступа:",
		Attachments: InlineKeyboard(rows...),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
}

// ---------------------------------------------------------------------------
// 1. Authorization in MAX Bot (Email + OTP / Profile Creation)
// ---------------------------------------------------------------------------

func (e *Engine) handleAuthEmailInput(ctx context.Context, uid, username string, sess db.BotFSMSession, text string) error {
	email, err := ValidateEmail(text)
	if err != nil {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        err.Error(),
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		})
	}

	fs := e.fullStore()
	if fs == nil {
		return e.sendMainMenu(ctx, uid, "", username, false)
	}

	u, err := fs.GetUserByEmail(ctx, email)
	if err != nil || u == nil {
		// Offer quick registration or tariff purchase
		sess.ContextData["email"] = email
		sess.CurrentState = StateStart
		_ = e.store.SaveFSMSession(ctx, sess)
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text: fmt.Sprintf("Профиль с e-mail `%s` ещё не зарегистрирован.\n\nВы можете выбрать тариф и оформить подписку прямо сейчас (ваш e-mail уже сохранён):", email),
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "💳 Выбрать подписку", Payload: "nav:tariffs"}},
				[]Button{{Type: "callback", Text: "🔐 Ввести другой e-mail", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		})
	}
	if u.Role != "client" {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        "Вход для врача и администратора выполняется только через веб-панель CRM. В боте MAX доступна авторизация для клиенток курса.",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		})
	}

	code, hash, err := auth.GenerateOTPForEmail(u.Email)
	if err != nil {
		return err
	}
	if err := fs.CreateAuthToken(ctx, u.ID, hash, "otp", u.Email, time.Now().UTC().Add(15*time.Minute)); err != nil {
		return err
	}

	subject := "Код подтверждения для бота MAX — «Беременность без тревоги»"
	body := fmt.Sprintf("Здравствуйте, %s!\n\nВаш одноразовый код для привязки и авторизации в боте MAX: %s\nКод действителен 15 минут.", u.FullName, code)
	if issuer, ok := e.payment.(MagicLinkIssuer); ok {
		issuer.DeliverEmail(u.Email, subject, body)
	} else if e.mailer != nil {
		_ = e.mailer.Send(u.Email, subject, body)
	}

	sess.ContextData["auth_email"] = u.Email
	sess.ContextData["auth_user_id"] = float64(u.ID)
	sess.ContextData["auth_otp_attempts"] = float64(0)
	sess.CurrentState = StateAuthOTP
	if err := e.store.SaveFSMSession(ctx, sess); err != nil {
		return err
	}

	return e.bot.SendMessage(ctx, uid, OutgoingMessage{
		Text: fmt.Sprintf(
			"📧 Мы нашли ваш профиль (*%s*) и отправили 6-значный код подтверждения на `%s`.\n\nВведите полученный *6-значный код* ответным сообщением:",
			EscapeMarkdown(u.FullName), u.Email,
		),
		Attachments: InlineKeyboard(
			[]Button{{Type: "callback", Text: "🔄 Указать другой e-mail", Payload: "auth:start"}},
			[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
		),
	})
}

func (e *Engine) handleAuthOTPInput(ctx context.Context, uid, username string, sess db.BotFSMSession, text string) error {
	fs := e.fullStore()
	if fs == nil {
		return e.sendMainMenu(ctx, uid, "", username, false)
	}
	code := strings.TrimSpace(text)
	targetEmail, _ := sess.ContextData["auth_email"].(string)
	if len(code) != 6 {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text: "Пожалуйста, введите 6-значный цифровой код из письма:",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔄 Ввести e-mail заново", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		})
	}

	u, err := fs.ConsumeAuthToken(ctx, auth.HashOTP(targetEmail, code), "otp")
	if err != nil || u == nil || u.Role != "client" || (targetEmail != "" && !strings.EqualFold(u.Email, targetEmail)) {
		attempts := 0
		if v, ok := sess.ContextData["auth_otp_attempts"].(float64); ok {
			attempts = int(v)
		}
		attempts++
		sess.ContextData["auth_otp_attempts"] = float64(attempts)
		if attempts >= 5 {
			sess.CurrentState = StateStart
			delete(sess.ContextData, "auth_email")
			delete(sess.ContextData, "auth_otp_attempts")
			_ = e.store.SaveFSMSession(ctx, sess)
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{
				Text: "❌ Превышено число попыток ввода кода (5). Пожалуйста, запросите новый код подтверждения:",
				Attachments: InlineKeyboard(
					[]Button{{Type: "callback", Text: "🔄 Запросить новый код", Payload: "auth:start"}},
					[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
				),
			})
		}
		_ = e.store.SaveFSMSession(ctx, sess)
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text: fmt.Sprintf("❌ Неверный или просроченный код (попытка %d/5). Проверьте письмо и попробуйте ещё раз:", attempts),
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔄 Отправить код повторно", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		})
	}

	linkedUser, err := fs.LinkMaxUserToAccount(ctx, u.ID, uid, username)
	if err != nil {
		return err
	}
	sess.CurrentState = StateStart
	_ = e.store.SaveFSMSession(ctx, sess)

	_ = e.bot.SendMessage(ctx, uid, OutgoingMessage{
		Text: fmt.Sprintf("✅ *Авторизация прошла успешно!*\n\nАккаунт *%s* (`%s`) привязан к вашему MAX. Теперь все материалы, запись на консультации, загрузка анализов и заключения доступны прямо в боте.", EscapeMarkdown(linkedUser.FullName), linkedUser.Email),
	})
	return e.sendMainMenu(ctx, uid, "", username, false)
}

// ---------------------------------------------------------------------------
// 2. Personal Data Completion & 152-FZ Consent Info
// ---------------------------------------------------------------------------

func (e *Engine) showProfileMenu(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	u, sub, subActive := e.resolveLinkedUser(ctx, uid)
	if u == nil {
		msg := OutgoingMessage{
			Text: "👤 *Личные данные и профиль*\n\nВы ещё не авторизованы. Войдите по e-mail или оформите подписку, чтобы управлять личными данными:",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔐 Войти по E-mail", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "💳 Выбрать подписку", Payload: "nav:tariffs"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
	}

	sess, err := e.loadOrInitSession(ctx, uid, username)
	if err == nil {
		sess.CurrentState = StateProfile
		_ = e.store.SaveFSMSession(ctx, sess)
	}

	subStatus := "Нет активной подписки"
	if subActive && sub != nil {
		subStatus = fmt.Sprintf("%s (активна до %s, осталось %d дн.)",
			sub.TariffName,
			sub.ExpiresAt.In(mskZone()).Format("02.01.2006"),
			sub.DaysLeft,
		)
	} else if sub != nil {
		subStatus = fmt.Sprintf("%s (истекла %s)", sub.TariffName, sub.ExpiresAt.In(mskZone()).Format("02.01.2006"))
	}

	consentStatus := "✅ Подтверждено (152-ФЗ и Оферта)"
	if fs := e.fullStore(); fs != nil {
		orders, _ := fs.ListOrders(ctx, u.ID, "")
		for _, ord := range orders {
			if ord.ConsentAcceptedAt != nil {
				consentStatus = fmt.Sprintf("✅ Подтверждено %s (заказ %s)", ord.ConsentAcceptedAt.In(mskZone()).Format("02.01.2006"), ord.OrderNumber)
				break
			}
		}
	}

	preg := u.PregnancyStatus
	if strings.TrimSpace(preg) == "" {
		preg = "не указан (нажмите «Срок беременности», чтобы дозаполнить)"
	}

	text := fmt.Sprintf(
		"👤 *Ваши личные данные и профиль*\n\n"+
			"• *ФИО:* %s\n"+
			"• *Телефон:* `%s`\n"+
			"• *E-mail:* `%s`\n"+
			"• *Срок беременности / статус:* %s\n"+
			"• *Подписка:* %s\n"+
			"• *Согласие на обработку ПДн:* %s\n\n"+
			"Выберите, какие данные вы хотите изменить или дозаполнить:",
		EscapeMarkdown(u.FullName),
		u.Phone,
		u.Email,
		EscapeMarkdown(preg),
		EscapeMarkdown(subStatus),
		consentStatus,
	)

	rows := [][]Button{
		{
			{Type: "callback", Text: "✏️ Изменить ФИО", Payload: "profile:edit:name"},
			{Type: "callback", Text: "🤰 Срок беременности", Payload: "profile:edit:preg"},
		},
		{
			{Type: "callback", Text: "📞 Изменить телефон", Payload: "profile:edit:phone"},
			{Type: "callback", Text: "📧 Изменить E-mail", Payload: "profile:edit:email"},
		},
		{
			{Type: "callback", Text: "📜 Согласие 152-ФЗ и Оферта", Payload: "profile:consent"},
			{Type: "callback", Text: "💳 Тарифы и продление", Payload: "nav:tariffs"},
		},
		{
			{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
		},
	}

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        text,
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

func (e *Engine) applyPersonalDataEdit(ctx context.Context, uid, username string, sess db.BotFSMSession, fullName, phone, email, pregStatus string) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendMainMenu(ctx, uid, "", username, false)
	}
	updated, err := fs.UpdateUserPersonalData(ctx, u.ID, fullName, phone, email, pregStatus)
	if err != nil {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        "Не удалось сохранить изменения (возможно, указанный e-mail уже занят другим профилем).",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Назад в профиль", Payload: "cab:profile"}}),
		})
	}
	sess.CurrentState = StateProfile
	_ = e.store.SaveFSMSession(ctx, sess)

	_ = e.bot.SendMessage(ctx, uid, OutgoingMessage{
		Text: fmt.Sprintf("✅ *Данные профиля обновлены!*\n\nФИО: *%s*\nТелефон: `%s`\nE-mail: `%s`\nСрок / статус: *%s*",
			EscapeMarkdown(updated.FullName), updated.Phone, updated.Email, EscapeMarkdown(updated.PregnancyStatus)),
	})
	return e.showProfileMenu(ctx, uid, "", username, false)
}

func (e *Engine) showConsentInfo(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	msg := OutgoingMessage{
		Text: "📜 *Согласие на обработку персональных данных (152-ФЗ) и Договор оферты*\n\n" +
			"1. Оператор обрабатывает персональные данные пациентки (ФИО, телефон, e-mail, сведения о сроке беременности, загруженные результаты анализов и медицинские заключения) исключительно в целях оказания информационно-консультационных услуг курса «Беременность без тревоги».\n" +
			"2. Перед передачей контекста в облачные ИИ-модели выполняется автоматическая деперсонализация (удаление ФИО, контактов и дат).\n" +
			"3. Хранение баз данных и объектного хранилища файлов осуществляется в защищённом контуре РФ.\n" +
			"4. Согласие фиксируется в базе данных с точной датой и временем (`consent_accepted_at`) при оформлении заказа.",
		Attachments: InlineKeyboard(
			[]Button{{Type: "callback", Text: "👤 Назад в личные данные", Payload: "cab:profile"}},
			[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
		),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
}

// ---------------------------------------------------------------------------
// 3. Course Materials & Watermarked PDF Delivery in Bot
// ---------------------------------------------------------------------------

func (e *Engine) showMaterialsList(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	fs := e.fullStore()
	u, sub, subActive := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text: "📚 *Учебные материалы курса*\n\nДля доступа к урокам, видео и именным PDF-методичкам войдите в свой профиль или оформите подписку:",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔐 Войти в аккаунт", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "💳 Выбрать подписку", Payload: "nav:tariffs"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}, editInPlace)
	}
	if !subActive && u.Role == "client" {
		expInfo := "У вас ещё нет активной подписки."
		if sub != nil {
			expInfo = fmt.Sprintf("Срок вашей подписки «%s» истёк %s.", EscapeMarkdown(sub.TariffName), sub.ExpiresAt.In(mskZone()).Format("02.01.2006"))
		}
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text: fmt.Sprintf("📚 *Мои материалы и уроки*\n\n⚠️ %s\nПродлите доступ, чтобы открывать уроки и скачивать персональные PDF-методички:", expInfo),
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "💳 Продлить подписку", Payload: "nav:tariffs"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}, editInPlace)
	}

	mats, err := fs.ListCabinetMaterials(ctx, u.ID)
	if err != nil {
		return err
	}

	completedCount := 0
	var b strings.Builder
	b.WriteString("📚 *Учебные материалы интенсива «Беременность без тревоги»*\n\n")
	var rows [][]Button

	for _, m := range mats {
		statusIcon := "📖"
		if m.Completed {
			statusIcon = "✅"
			completedCount++
		}
		pdfBadge := ""
		if m.FileType == "pdf" {
			if m.ClientStatus == "ready" {
				pdfBadge = " · 📄 PDF готов"
			} else {
				pdfBadge = " · ⏳ PDF готовится"
			}
		}
		fmt.Fprintf(&b, "%s *Модуль %d. Урок %d:* %s (%d мин%s)\n",
			statusIcon, m.ModuleNumber, m.LessonNumber, EscapeMarkdown(m.Title), m.DurationMinutes, pdfBadge)

		btnTitle := fmt.Sprintf("%s М%d.У%d: %s", statusIcon, m.ModuleNumber, m.LessonNumber, m.Title)
		if len([]rune(btnTitle)) > 44 {
			btnTitle = string([]rune(btnTitle)[:41]) + "..."
		}
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    btnTitle,
			Payload: fmt.Sprintf("mat:view:%d", m.ID),
		}})
	}

	fmt.Fprintf(&b, "\n*Прогресс обучения:* изучено %d из %d уроков.\nНажмите на урок ниже, чтобы прочитать конспект, скачать именную PDF-методичку и отметить прохождение:", completedCount, len(mats))
	rows = append(rows, []Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}})

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        b.String(),
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

func (e *Engine) showMaterialDetail(ctx context.Context, uid, callbackID, username string, matID int, editInPlace bool) error {
	fs := e.fullStore()
	u, _, subActive := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil || (!subActive && u.Role == "client") {
		return e.showMaterialsList(ctx, uid, callbackID, username, editInPlace)
	}

	mats, err := fs.ListCabinetMaterials(ctx, u.ID)
	if err != nil {
		return err
	}
	var item *db.CabinetMaterial
	for i := range mats {
		if mats[i].ID == matID {
			item = &mats[i]
			break
		}
	}
	if item == nil {
		return e.showMaterialsList(ctx, uid, callbackID, username, editInPlace)
	}

	statusLine := "📖 Статус: *В процессе изучения*"
	if item.Completed {
		statusLine = "✅ Статус: *Урок изучен*"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📘 *Модуль %d · Урок %d*\n*%s*\n%s · ⏱ %d мин\n\n",
		item.ModuleNumber, item.LessonNumber, EscapeMarkdown(item.Title), statusLine, item.DurationMinutes)
	if strings.TrimSpace(item.Description) != "" {
		fmt.Fprintf(&b, "_%s_\n\n", EscapeMarkdown(item.Description))
	}
	if strings.TrimSpace(item.ContentMarkdown) != "" {
		cleanNotes := strings.ReplaceAll(item.ContentMarkdown, "### ", "*")
		cleanNotes = strings.ReplaceAll(cleanNotes, "\n", "\n")
		fmt.Fprintf(&b, "%s\n\n", cleanNotes)
	}

	var rows [][]Button
	// Provide direct presigned PDF download link if S3 is available.
	// Security (TZ §5.1-5.2): clients MUST ONLY receive their personalized watermarked copy from BucketClient.
	if item.FileType == "pdf" && e.s3 != nil {
		var bucket, key string
		filename := fmt.Sprintf("module_%d_lesson_%d.pdf", item.ModuleNumber, item.LessonNumber)
		if u.Role == "admin" || u.Role == "doctor" {
			bucket = e.s3.BucketMaster
			key = item.S3MasterKey
		} else if cm, err := fs.GetClientMaterial(ctx, u.ID, item.ID); err == nil && cm != nil && cm.Status == "ready" && cm.S3ClientKey != "" {
			bucket = e.s3.BucketClient
			key = cm.S3ClientKey
		}
		if bucket != "" && key != "" {
			if pdfURL, err := e.s3.PresignedGetURL(ctx, bucket, key, 30*time.Minute, filename); err == nil && pdfURL != "" {
				rows = append(rows, []Button{{
					Type: "link",
					Text: "📄 Скачать именной PDF урока",
					URL:  pdfURL,
				}})
			}
		} else if u.Role == "client" {
			b.WriteString("⏳ _Ваш персональный PDF с защитной маркировкой сейчас подготавливается. Нажмите «Обновить» через несколько секунд._\n\n")
			rows = append(rows, []Button{{
				Type:    "callback",
				Text:    "🔄 Обновить статус PDF",
				Payload: fmt.Sprintf("mat:view:%d", item.ID),
			}})
		}
	}
	if strings.TrimSpace(item.VideoURL) != "" {
		rows = append(rows, []Button{{
			Type: "link",
			Text: "🎬 Смотреть видеоурок",
			URL:  item.VideoURL,
		}})
	}

	if item.Completed {
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    "↩️ Вернуть в список к изучению",
			Payload: fmt.Sprintf("mat:todo:%d", item.ID),
		}})
	} else {
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    "✅ Отметить урок как изученный",
			Payload: fmt.Sprintf("mat:done:%d", item.ID),
		}})
	}

	rows = append(rows, []Button{
		{Type: "callback", Text: "📚 Все материалы", Payload: "cab:materials"},
		{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
	})

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        b.String(),
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

func (e *Engine) toggleMaterialCompleted(ctx context.Context, uid, callbackID, username string, matID int, completed bool) error {
	fs := e.fullStore()
	u, _, subActive := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil || (!subActive && u.Role == "client") {
		return e.showMaterialsList(ctx, uid, callbackID, username, true)
	}
	_ = fs.SetMaterialCompleted(ctx, u.ID, matID, completed)
	return e.showMaterialDetail(ctx, uid, callbackID, username, matID, true)
}

// ---------------------------------------------------------------------------
// 4. Online Consultation Slots: Viewing, Booking, Question Input & Cancellation
// ---------------------------------------------------------------------------

func (e *Engine) showConsultationsMenu(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	fs := e.fullStore()
	u, sub, subActive := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text: "📅 *Запись на онлайн-консультацию к врачу*\n\nАвторизуйтесь или выберите тариф «С обратной связью», чтобы записаться на индивидуальный онлайн-приём:",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔐 Войти в аккаунт", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "💳 Тарифы с обратной связью", Payload: "nav:tariffs"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}, editInPlace)
	}

	slots, err := fs.ListConsultationSlots(ctx, false, u.ID, "")
	if err != nil {
		return err
	}
	cCfg, _ := fs.GetConsultationConfig(ctx)
	now := time.Now().UTC()

	var bookedSlots []db.ConsultationSlot
	var availableSlots []db.ConsultationSlot
	for _, sl := range slots {
		if sl.ClientID != nil && *sl.ClientID == u.ID && sl.Status == "booked" && sl.SlotEnd.After(now) {
			bookedSlots = append(bookedSlots, sl)
		} else if sl.Status == "available" && sl.SlotStart.After(now) {
			availableSlots = append(availableSlots, sl)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📅 *Онлайн-консультации с врачом*\nВрач: *%s*\n\n", EscapeMarkdown(cCfg.DoctorName))

	var rows [][]Button

	if len(bookedSlots) > 0 {
		b.WriteString("✅ *Ваши активные записи:*\n")
		for _, sl := range bookedSlots {
			startStr := sl.SlotStart.In(mskZone()).Format("02.01.2006 в 15:04 (МСК)")
			atts, _ := fs.ListConsultationAttachments(ctx, sl.ID)
			qText := sl.ClientQuestion
			if strings.TrimSpace(qText) == "" {
				qText = "не указан"
			}
			fmt.Fprintf(&b, "• *%s* (прикреплено файлов анализов: %d)\n  Ваш вопрос/жалобы: _%s_\n",
				startStr, len(atts), EscapeMarkdown(qText))

			if strings.TrimSpace(sl.MeetingURL) != "" {
				rows = append(rows, []Button{{
					Type: "link",
					Text: fmt.Sprintf("🎥 Подключиться к встрече (%s)", sl.SlotStart.In(mskZone()).Format("02.01 15:04")),
					URL:  sl.MeetingURL,
				}})
			}
			rows = append(rows, []Button{
				{Type: "callback", Text: "🧪 Прикрепить анализы", Payload: fmt.Sprintf("ana:slot:%d", sl.ID)},
				{Type: "callback", Text: "✏️ Изменить вопрос", Payload: fmt.Sprintf("slot:edit_q:%d", sl.ID)},
			})
			rows = append(rows, []Button{
				{Type: "callback", Text: fmt.Sprintf("❌ Отменить запись %s", sl.SlotStart.In(mskZone()).Format("02.01 15:04")), Payload: fmt.Sprintf("slot:cancel:%d", sl.ID)},
			})
		}
		b.WriteString("\n")
	}

	hasFeedback := (sub != nil && sub.HasFeedback && subActive) || u.Role == "admin" || u.Role == "doctor"
	if !hasFeedback {
		b.WriteString("ℹ️ _Запись на индивидуальные онлайн-консультации доступна на тарифах «С обратной связью»._\n")
		rows = append(rows, []Button{{Type: "callback", Text: "💳 Перейти на тариф «С обратной связью»", Payload: "nav:tariffs"}})
	} else if len(availableSlots) == 0 {
		b.WriteString("Свободных окон на ближайшие дни пока нет. Напишите врачу в чат, и мы подберём удобное время.\n")
	} else {
		b.WriteString("🕒 *Свободные слоты для записи (время МСК):*\nНажмите на удобное окно, чтобы забронировать встречу:")
		maxAvailable := 8 - len(rows)
		if maxAvailable < 2 {
			maxAvailable = 2
		}
		limit := len(availableSlots)
		if limit > maxAvailable {
			limit = maxAvailable
		}
		for i := 0; i < limit; i++ {
			sl := availableSlots[i]
			label := fmt.Sprintf("🗓 %s – %s МСК",
				sl.SlotStart.In(mskZone()).Format("02.01.2006 15:04"),
				sl.SlotEnd.In(mskZone()).Format("15:04"),
			)
			rows = append(rows, []Button{{
				Type:    "callback",
				Text:    label,
				Payload: fmt.Sprintf("slot:pick:%d", sl.ID),
			}})
		}
	}

	rows = append(rows, []Button{
		{Type: "callback", Text: "📋 Мои заключения", Payload: "cab:docs"},
		{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
	})

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        b.String(),
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

func (e *Engine) promptSlotBookingQuestion(ctx context.Context, uid, callbackID, username string, sess db.BotFSMSession, slotID int64) error {
	fs := e.fullStore()
	u, sub, subActive := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.showConsultationsMenu(ctx, uid, callbackID, username, true)
	}
	if u.Role == "client" && (sub == nil || !subActive || !sub.HasFeedback) {
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{
			Notification: "Запись доступна на активном тарифе «С обратной связью»",
		})
	}
	sl, err := fs.GetConsultationSlot(ctx, slotID, false)
	if err != nil || sl == nil || sl.Status != "available" {
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{
			Notification: "Этот слот уже занят или недоступен",
		})
	}

	sess.ContextData["booking_slot_id"] = float64(slotID)
	sess.CurrentState = StepBookSlotQuestion
	if err := e.store.SaveFSMSession(ctx, sess); err != nil {
		return err
	}

	dateStr := sl.SlotStart.In(mskZone()).Format("02.01.2006 в 15:04 (МСК)")
	msg := OutgoingMessage{
		Text: fmt.Sprintf(
			"🗓 Вы выбрали слот для онлайн-консультации: *%s*\n\n"+
				"Напишите одним сообщением *ваш запрос, жалобы или вопросы к врачу* (чтобы за 30 минут до встречи система подготовила клиническое ИИ-саммари для доктора), либо нажмите кнопку ниже для записи без комментария:",
			dateStr,
		),
		Attachments: InlineKeyboard(
			[]Button{{Type: "callback", Text: "✅ Записаться без комментария", Payload: fmt.Sprintf("slot:book_now:%d", slotID)}},
			[]Button{{Type: "callback", Text: "⬅️ Назад к слотам", Payload: "cab:slots"}},
		),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, true)
}

func (e *Engine) completeSlotBookingWithQuestion(ctx context.Context, uid, callbackID, username string, sess db.BotFSMSession, question string, editInPlace bool) error {
	fs := e.fullStore()
	u, sub, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendMainMenu(ctx, uid, callbackID, username, editInPlace)
	}
	var slotID int64
	if v, ok := sess.ContextData["booking_slot_id"].(float64); ok {
		slotID = int64(v)
	} else if v, ok := sess.ContextData["booking_slot_id"].(int64); ok {
		slotID = v
	}
	if slotID <= 0 {
		return e.showConsultationsMenu(ctx, uid, callbackID, username, editInPlace)
	}

	var orderID int64
	if sub != nil {
		orderID = sub.OrderID
	}
	sl, err := fs.BookConsultationSlot(ctx, slotID, u.ID, orderID, strings.TrimSpace(question))
	if err != nil {
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text:        "⚠️ Выбранный слот уже занят или недоступен. Пожалуйста, выберите другое время:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "📅 Выбрать другой слот", Payload: "cab:slots"}}),
		}, editInPlace)
	}

	sess.CurrentState = StateConsultations
	sess.ContextData["active_slot_id"] = float64(sl.ID)
	_ = e.store.SaveFSMSession(ctx, sess)

	dateStr := sl.SlotStart.In(mskZone()).Format("02.01.2006 в 15:04 (МСК)")
	qInfo := sl.ClientQuestion
	if strings.TrimSpace(qInfo) == "" {
		qInfo = "не указан"
	}

	var rows [][]Button
	if strings.TrimSpace(sl.MeetingURL) != "" {
		rows = append(rows, []Button{{Type: "link", Text: "🎥 Ссылка на видеовстречу", URL: sl.MeetingURL}})
	}
	rows = append(rows,
		[]Button{{Type: "callback", Text: "🧪 Прикрепить анализы / УЗИ к приёму", Payload: fmt.Sprintf("ana:slot:%d", sl.ID)}},
		[]Button{
			{Type: "callback", Text: "📅 Мои записи", Payload: "cab:slots"},
			{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
		},
	)

	msg := OutgoingMessage{
		Text: fmt.Sprintf(
			"✅ *Запись на онлайн-консультацию подтверждена!*\n\n"+
				"• *Дата и время:* %s\n"+
				"• *Ваш запрос врачу:* %s\n\n"+
				"За 30 минут до начала приёма система автоматически сформирует для врача предконсультационное саммари по вашему профилю и загруженным бланкам анализов.\n"+
				"Прикрепить файлы анализов или исследований можно прямо сейчас кнопкой ниже:",
			dateStr, EscapeMarkdown(qInfo),
		),
		Attachments: InlineKeyboard(rows...),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
}

func (e *Engine) promptUpdateSlotQuestion(ctx context.Context, uid, callbackID string, sess db.BotFSMSession, slotID int64) error {
	sess.ContextData["editing_slot_id"] = float64(slotID)
	sess.CurrentState = StepUpdateSlotQuestion
	_ = e.store.SaveFSMSession(ctx, sess)

	msg := OutgoingMessage{
		Text:        "✏️ *Изменение вопроса / жалоб к консультации*\n\nНапишите новый текст вашего запроса или симптомов, которые важно разобрать на встрече с врачом:",
		Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Назад к консультациям", Payload: "cab:slots"}}),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, true)
}

func (e *Engine) completeSlotQuestionUpdate(ctx context.Context, uid, username string, sess db.BotFSMSession, question string) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendMainMenu(ctx, uid, "", username, false)
	}
	var slotID int64
	if v, ok := sess.ContextData["editing_slot_id"].(float64); ok {
		slotID = int64(v)
	} else if v, ok := sess.ContextData["editing_slot_id"].(int64); ok {
		slotID = v
	}
	if slotID > 0 {
		_ = fs.UpdateSlotQuestion(ctx, slotID, u.ID, strings.TrimSpace(question))
	}
	sess.CurrentState = StateConsultations
	_ = e.store.SaveFSMSession(ctx, sess)

	_ = e.bot.SendMessage(ctx, uid, OutgoingMessage{
		Text: "✅ Ваш вопрос к консультации сохранён и передан врачу.",
	})
	return e.showConsultationsMenu(ctx, uid, "", username, false)
}

func (e *Engine) cancelBookedSlotInBot(ctx context.Context, uid, callbackID, username string, slotID int64) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendMainMenu(ctx, uid, callbackID, username, true)
	}
	if err := fs.CancelConsultationSlot(ctx, slotID, u.ID, false); err != nil {
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{
			Notification: "Не удалось отменить запись (возможно, она уже отменена)",
		})
	}
	return e.showConsultationsMenu(ctx, uid, callbackID, username, true)
}

// ---------------------------------------------------------------------------
// 5. Adding Lab Analyses, Ultrasound Studies & Other Materials via Bot
// ---------------------------------------------------------------------------

func (e *Engine) resolveTargetSlotForAnalysis(ctx context.Context, userID, preferredSlotID int64) (*db.ConsultationSlot, error) {
	fs := e.fullStore()
	if fs == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	if preferredSlotID > 0 {
		if sl, err := fs.GetConsultationSlot(ctx, preferredSlotID, false); err == nil && sl != nil && sl.ClientID != nil && *sl.ClientID == userID {
			return sl, nil
		}
	}
	slots, _ := fs.ListConsultationSlots(ctx, false, userID, "")
	now := time.Now().UTC()
	// 1. Prefer upcoming booked slot
	for i := range slots {
		if slots[i].ClientID != nil && *slots[i].ClientID == userID && slots[i].Status == "booked" && slots[i].SlotEnd.After(now) {
			return &slots[i], nil
		}
	}
	// 2. Fallback to most recent slot of this client
	var latest *db.ConsultationSlot
	for i := range slots {
		if slots[i].ClientID != nil && *slots[i].ClientID == userID {
			if latest == nil || slots[i].SlotStart.After(latest.SlotStart) {
				latest = &slots[i]
			}
		}
	}
	return latest, nil
}

func (e *Engine) showAnalysesMenu(ctx context.Context, uid, callbackID, username string, editInPlace bool, targetSlotID int64) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text: "🧪 *Загрузка анализов, УЗИ и исследований*\n\nВойдите в свой профиль, чтобы прикреплять файлы анализов и обследований к вашей медицинской карте:",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔐 Войти в аккаунт", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}, editInPlace)
	}

	targetSlot, _ := e.resolveTargetSlotForAnalysis(ctx, u.ID, targetSlotID)
	sess, err := e.loadOrInitSession(ctx, uid, username)
	if err == nil {
		sess.CurrentState = StateUploadAnalysis
		if targetSlot != nil {
			sess.ContextData["upload_slot_id"] = float64(targetSlot.ID)
		}
		_ = e.store.SaveFSMSession(ctx, sess)
	}

	atts, _ := fs.ListClientAttachments(ctx, u.ID)
	var b strings.Builder
	b.WriteString("🧪 *Анализы, УЗИ и медицинские материалы*\n\n")

	if targetSlot != nil {
		fmt.Fprintf(&b, "📌 *Привязка к консультации:* %s (%s)\n\n",
			targetSlot.SlotStart.In(mskZone()).Format("02.01.2006 15:04 МСК"),
			targetSlot.Status,
		)
	} else {
		b.WriteString("📌 _У вас пока нет выбранного слота консультации, но вы всё равно можете отправить результаты анализов текстовым сообщением или сначала записаться на слот._\n\n")
	}

	if len(atts) > 0 {
		b.WriteString("📂 *Загруженные вами материалы:*\n")
		limit := len(atts)
		if limit > 8 {
			limit = 8
		}
		for i := 0; i < limit; i++ {
			a := atts[i]
			kb := a.FileSize / 1024
			if kb < 1 {
				kb = 1
			}
			fmt.Fprintf(&b, "• `%s` (%d КБ, %s)\n", a.FileName, kb, a.CreatedAt.In(mskZone()).Format("02.01.2006 15:04"))
		}
		b.WriteString("\n")
	}

	b.WriteString("*Как добавить анализы или исследования прямо здесь в боте:*\n" +
		"1️⃣ Прикрепите *файл (PDF) или фото бланка (JPG/PNG/WEBP)* к сообщению и отправьте его в этот чат.\n" +
		"2️⃣ Или отправьте *текстом показатели анализов / заключение УЗИ / ссылку на архив лаборатории* — бот сохранит документ в вашу медицинскую карту для врача и учтёт его в ИИ-саммари.")

	rows := [][]Button{
		{
			{Type: "callback", Text: "📅 Выбрать слот консультации", Payload: "cab:slots"},
			{Type: "callback", Text: "📋 Мои заключения", Payload: "cab:docs"},
		},
		{
			{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
		},
	}

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        b.String(),
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

func (e *Engine) handleAttachmentUpload(ctx context.Context, uid, username string, sess db.BotFSMSession, text string, atts []IncomingAttachment) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendMainMenu(ctx, uid, "", username, false)
	}

	var prefSlotID int64
	if v, ok := sess.ContextData["upload_slot_id"].(float64); ok {
		prefSlotID = int64(v)
	}
	targetSlot, _ := e.resolveTargetSlotForAnalysis(ctx, u.ID, prefSlotID)

	// If the user has no booked slot yet, also save text notes into doctor chat & questions so nothing is lost
	if targetSlot == nil && len(atts) == 0 && strings.TrimSpace(text) == "" {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        "Пожалуйста, прикрепите файл/фото анализа или напишите показатели текстом.",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		})
	}

	var savedFiles []string
	var rejectedFiles []string

	// Process binary/URL attachments from MAX message
	for idx, att := range atts {
		if att.Type == "inline_keyboard" {
			continue
		}
		fileURL := strings.TrimSpace(att.URL)
		if fileURL == "" {
			fileURL = strings.TrimSpace(att.Payload.URL)
		}
		fileName := strings.TrimSpace(att.Filename)
		if fileName == "" {
			fileName = strings.TrimSpace(att.Payload.Filename)
		}
		if fileName == "" {
			fileName = strings.TrimSpace(att.Payload.Name)
		}
		if fileName == "" {
			ext := ".jpg"
			if strings.EqualFold(att.Type, "file") {
				ext = ".pdf"
			}
			fileName = fmt.Sprintf("analysis_%s_%d%s", time.Now().In(mskZone()).Format("20060102_150405"), idx+1, ext)
		}
		safeName := sanitizeUploadFileName(fileName)
		if !isAllowedBotUploadExt(filepath.Ext(safeName)) {
			rejectedFiles = append(rejectedFiles, safeName)
			continue
		}

		var data []byte
		contentType := "application/octet-stream"
		if fileURL != "" && isSafePublicHTTPSURL(ctx, fileURL) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
			if err == nil {
				if resp, rErr := e.httpClient.Do(req); rErr == nil {
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						data, _ = io.ReadAll(io.LimitReader(resp.Body, 20<<20))
						if ct := resp.Header.Get("Content-Type"); ct != "" {
							contentType = ct
						}
					}
					resp.Body.Close()
				}
			}
		}
		if len(data) == 0 {
			metaSummary := fmt.Sprintf("Вложение из MAX (%s): %s\nКомментарий пациентки: %s", att.Type, safeName, text)
			data = []byte(metaSummary)
			contentType = "text/plain; charset=utf-8"
			safeName = strings.TrimSuffix(safeName, filepath.Ext(safeName)) + ".txt"
		}

		if targetSlot != nil {
			s3Key := fmt.Sprintf("attachments/slot_%d/max_user_%d_%d_%s", targetSlot.ID, u.ID, time.Now().UnixNano(), safeName)
			if e.s3 != nil {
				_ = e.s3.PutBytes(ctx, e.s3.BucketConsult, s3Key, data, contentType)
			}
			if _, err := fs.AddConsultationAttachment(ctx, targetSlot.ID, u.ID, safeName, contentType, int64(len(data)), s3Key); err == nil {
				savedFiles = append(savedFiles, safeName)
			}
		}
	}

	if len(rejectedFiles) > 0 && len(savedFiles) == 0 && strings.TrimSpace(text) == "" {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        "⚠️ Поддерживаются только файлы *PDF, JPG, JPEG, PNG и WEBP* (до 20 МБ). Пожалуйста, отправьте бланк анализа в одном из этих форматов или напишите показатели текстом.",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		})
	}

	// If user sent text (lab values, ultrasound notes, link to lab results, or comment accompanying files)
	if strings.TrimSpace(text) != "" {
		if len(savedFiles) == 0 && targetSlot != nil {
			fileName := fmt.Sprintf("analysis_note_%s.txt", time.Now().In(mskZone()).Format("20060102_150405"))
			data := []byte(text)
			s3Key := fmt.Sprintf("attachments/slot_%d/max_user_%d_%d_%s", targetSlot.ID, u.ID, time.Now().UnixNano(), fileName)
			if e.s3 != nil {
				_ = e.s3.PutBytes(ctx, e.s3.BucketConsult, s3Key, data, "text/plain; charset=utf-8")
			}
			if _, err := fs.AddConsultationAttachment(ctx, targetSlot.ID, u.ID, fileName, "text/plain; charset=utf-8", int64(len(data)), s3Key); err == nil {
				savedFiles = append(savedFiles, fileName)
			}
		} else if targetSlot == nil {
			// No booked slot yet: forward text note into doctor chat so nothing is lost
			chatNote := fmt.Sprintf("🧪 Результаты анализов / исследований (из бота MAX):\n%s", strings.TrimSpace(text))
			_, _ = fs.CreateChatMessage(ctx, u.ID, u.ID, "client", chatNote)
		}
	}

	var confirmText string
	if len(savedFiles) > 0 && targetSlot != nil {
		confirmText = fmt.Sprintf(
			"✅ *Материалы сохранены в вашей карте и прикреплены к консультации (%s):*\n• `%s`\n\n"+
				"Врач увидит их в карточке приёма, а ИИ-модель учтёт при формировании предконсультационного саммари.\n"+
				"Вы можете отправить ещё один файл или вернуться в меню:",
			targetSlot.SlotStart.In(mskZone()).Format("02.01.2006 15:04"),
			strings.Join(savedFiles, "`\n• `"),
		)
	} else {
		confirmText = "✅ *Сведения по анализам и исследованиям сохранены в вашей карте и переданы врачу!*\n\nВы можете записаться на слот онлайн-консультации, чтобы разобрать их на видеовстрече:"
	}

	return e.bot.SendMessage(ctx, uid, OutgoingMessage{
		Text: confirmText,
		Attachments: InlineKeyboard(
			[]Button{
				{Type: "callback", Text: "🧪 Мои загруженные анализы", Payload: "cab:analyses"},
				{Type: "callback", Text: "📅 Слоты консультаций", Payload: "cab:slots"},
			},
			[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
		),
	})
}

// ---------------------------------------------------------------------------
// 6. Medical Conclusions & Official Recommendations (`patient_documents`)
// ---------------------------------------------------------------------------

func (e *Engine) showDocumentsList(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text: "📋 *Мои медицинские заключения*\n\nАвторизуйтесь в профиле, чтобы просматривать и скачивать официальные заключения и рекомендации врача:",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "🔐 Войти в аккаунт", Payload: "auth:start"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}, editInPlace)
	}

	docs, err := fs.ListPatientDocuments(ctx, u.ID, true)
	if err != nil {
		return err
	}

	if len(docs) == 0 {
		msg := OutgoingMessage{
			Text: "📋 *Мои медицинские заключения и рекомендации*\n\n" +
				"У вас пока нет опубликованных врачебных заключений.\n" +
				"После проведения онлайн-консультации врач проверит и утвердит итоговые рекомендации — они автоматически придут вам сюда в бот вместе с PDF-выпиской.",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "📅 Записаться на консультацию", Payload: "cab:slots"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📋 *Ваши медицинские заключения и рекомендации (%d):*\n\n", len(docs))

	var rows [][]Button
	limit := len(docs)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		d := docs[i]
		dateStr := d.CreatedAt.In(mskZone()).Format("02.01.2006")
		if d.PublishedAt != nil {
			dateStr = d.PublishedAt.In(mskZone()).Format("02.01.2006")
		}
		fmt.Fprintf(&b, "%d. *%s* (опубликовано %s)\n", i+1, EscapeMarkdown(d.Title), dateStr)
		rows = append(rows, []Button{{
			Type:    "callback",
			Text:    fmt.Sprintf("📄 Открыть: %s", d.Title),
			Payload: fmt.Sprintf("doc:view:%d", d.ID),
		}})
	}

	b.WriteString("\nВыберите документ ниже, чтобы прочитать полный текст рекомендаций и скачать официальный PDF-бланк:")
	rows = append(rows, []Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}})

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        b.String(),
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

func (e *Engine) showDocumentDetail(ctx context.Context, uid, callbackID, username string, docID int64, editInPlace bool) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.showDocumentsList(ctx, uid, callbackID, username, editInPlace)
	}

	docs, err := fs.ListPatientDocuments(ctx, u.ID, true)
	if err != nil {
		return err
	}
	var item *db.PatientDocument
	for i := range docs {
		if docs[i].ID == docID {
			item = &docs[i]
			break
		}
	}
	if item == nil {
		return e.showDocumentsList(ctx, uid, callbackID, username, editInPlace)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📋 *%s*\n", EscapeMarkdown(item.Title))
	if strings.TrimSpace(item.DoctorName) != "" {
		fmt.Fprintf(&b, "👩‍⚕️ Врач: *%s*\n", EscapeMarkdown(item.DoctorName))
	}
	b.WriteString("\n")
	if strings.TrimSpace(item.Complaints) != "" {
		fmt.Fprintf(&b, "• *Жалобы и запрос:* %s\n\n", EscapeMarkdown(item.Complaints))
	}
	if strings.TrimSpace(item.Anamnesis) != "" {
		fmt.Fprintf(&b, "• *Анамнез и динамика:* %s\n\n", EscapeMarkdown(item.Anamnesis))
	}
	if strings.TrimSpace(item.Recommendations) != "" {
		fmt.Fprintf(&b, "✅ *Рекомендации и назначения:*\n%s\n\n", EscapeMarkdown(item.Recommendations))
	}
	if strings.TrimSpace(item.DiagnosticsPlan) != "" {
		fmt.Fprintf(&b, "🧪 *План обследований и анализов:*\n%s\n\n", EscapeMarkdown(item.DiagnosticsPlan))
	}
	if strings.TrimSpace(item.FollowUp) != "" {
		fmt.Fprintf(&b, "🗓 *План контроля:*\n%s\n", EscapeMarkdown(item.FollowUp))
	}

	var rows [][]Button
	if e.s3 != nil && item.S3PDFKey != "" {
		filename := fmt.Sprintf("recommendations_%d.pdf", item.ID)
		if pdfURL, err := e.s3.PresignedGetURL(ctx, e.s3.BucketConsult, item.S3PDFKey, 30*time.Minute, filename); err == nil && pdfURL != "" {
			rows = append(rows, []Button{{
				Type: "link",
				Text: "📥 Скачать официальное заключение (PDF)",
				URL:  pdfURL,
			}})
		}
	}
	rows = append(rows, []Button{
		{Type: "callback", Text: "📋 Все заключения", Payload: "cab:docs"},
		{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
	})

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text:        b.String(),
		Attachments: InlineKeyboard(rows...),
	}, editInPlace)
}

// ---------------------------------------------------------------------------
// 7. Direct Doctor Chat & Magic Link to Web Cabinet
// ---------------------------------------------------------------------------

func (e *Engine) showDoctorChatMenu(ctx context.Context, uid, callbackID, username string, sess db.BotFSMSession, editInPlace bool) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		sess.CurrentState = StateQuestion
		_ = e.store.SaveFSMSession(ctx, sess)
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text:        "💬 *Вопрос врачу и службе заботы*\n\nНапишите ваш вопрос одним сообщением — мы передадим его врачу и ответим вам прямо в этот чат:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		}, editInPlace)
	}

	sess.CurrentState = StateDoctorChat
	_ = e.store.SaveFSMSession(ctx, sess)

	msgs, _ := fs.ListChatMessages(ctx, u.ID)
	var b strings.Builder
	b.WriteString("💬 *Личный чат с врачом*\n\n")
	if len(msgs) > 0 {
		b.WriteString("*Последние сообщения:*\n")
		start := 0
		if len(msgs) > 4 {
			start = len(msgs) - 4
		}
		for _, m := range msgs[start:] {
			author := "Вы"
			if m.SenderRole != "client" {
				author = "👩‍⚕️ Врач"
			}
			fmt.Fprintf(&b, "• *%s* (%s): %s\n", author, m.CreatedAt.In(mskZone()).Format("02.01 15:04"), EscapeMarkdown(m.Body))
		}
		b.WriteString("\n")
	}
	b.WriteString("Напишите ваше сообщение врачу ниже — оно сразу появится в вашем личном кабинете и у доктора в CRM:")

	return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
		Text: b.String(),
		Attachments: InlineKeyboard(
			[]Button{
				{Type: "callback", Text: "🧪 Отправить анализы", Payload: "cab:analyses"},
				{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
			},
		),
	}, editInPlace)
}

func (e *Engine) handleDoctorChatInput(ctx context.Context, uid, username string, sess db.BotFSMSession, text string) error {
	fs := e.fullStore()
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if fs == nil || u == nil {
		return e.sendMainMenu(ctx, uid, "", username, false)
	}
	if len([]rune(strings.TrimSpace(text))) < 2 {
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        "Введите текст сообщения врачу или вернитесь в главное меню:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		})
	}
	_, _ = fs.CreateChatMessage(ctx, u.ID, u.ID, "client", text)

	return e.bot.SendMessage(ctx, uid, OutgoingMessage{
		Text: "✅ *Сообщение отправлено врачу!*\nОтвет придёт прямо сюда в бот MAX и продублируется в вашем личном кабинете.",
		Attachments: InlineKeyboard(
			[]Button{
				{Type: "callback", Text: "💬 История чата", Payload: "cab:chat"},
				{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
			},
		),
	})
}

func (e *Engine) sendWebCabinetMagicLink(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	u, _, _ := e.resolveLinkedUser(ctx, uid)
	if u == nil || u.Role != "client" {
		return e.sendMainMenu(ctx, uid, callbackID, username, editInPlace)
	}
	magicURL := ""
	if issuer, ok := e.payment.(MagicLinkIssuer); ok {
		magicURL, _ = issuer.IssueMagicLink(ctx, *u)
	}
	if magicURL == "" {
		base := e.publicBaseURL
		if base == "" {
			base = "http://localhost:5173"
		}
		magicURL = strings.TrimRight(base, "/") + "/cabinet/"
	}

	msg := OutgoingMessage{
		Text: fmt.Sprintf(
			"🌐 *Быстрый вход в веб-версию личного кабинета*\n\n"+
				"Пациентка: *%s* (`%s`)\n"+
				"Нажмите кнопку ниже для мгновенного входа без ввода пароля:",
			EscapeMarkdown(u.FullName), u.Email,
		),
		Attachments: InlineKeyboard(
			[]Button{{Type: "link", Text: "🎓 Открыть веб-кабинет", URL: magicURL}},
			[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
		),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
}
