package maxbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/mailer"
	"zdorovie/backend/internal/s3"
)

// FSM state constants matching TZ §3.2 & MAX API docs, plus patient cabinet states.
const (
	StateStart             = "STATE_START"
	StateAbout             = "STATE_ABOUT"
	StateProgram           = "STATE_PROGRAM"
	StateTariffs           = "STATE_TARIFFS"
	StateQuestion          = "STATE_QUESTION"
	StepInputName          = "STEP_INPUT_NAME"
	StepInputPhone         = "STEP_INPUT_PHONE"
	StepInputEmail         = "STEP_INPUT_EMAIL"
	StepInputPregStatus    = "STEP_INPUT_PREGNANCY_STATUS"
	StepConfirmPolicy      = "STEP_CONFIRM_POLICY"
	StatePaymentPending    = "STATE_PAYMENT_PENDING"
	StatePaid              = "STATE_PAID"

	// Authorization & Personal Data States
	StateAuthEmail         = "STATE_AUTH_EMAIL"
	StateAuthOTP           = "STATE_AUTH_OTP"
	StateProfile           = "STATE_PROFILE"
	StepEditName           = "STEP_EDIT_NAME"
	StepEditPhone          = "STEP_EDIT_PHONE"
	StepEditEmail          = "STEP_EDIT_EMAIL"
	StepEditPregStatus     = "STEP_EDIT_PREGNANCY_STATUS"

	// Patient Functional States (Materials, Slots, Analyses, Conclusions, Doctor Chat)
	StateMaterials         = "STATE_MATERIALS"
	StateConsultations     = "STATE_CONSULTATIONS"
	StepBookSlotQuestion   = "STEP_BOOK_SLOT_QUESTION"
	StepUpdateSlotQuestion = "STEP_UPDATE_SLOT_QUESTION"
	StateUploadAnalysis    = "STATE_UPLOAD_ANALYSIS"
	StateDocuments         = "STATE_DOCUMENTS"
	StateDoctorChat        = "STATE_DOCTOR_CHAT"
)

// FlexID decodes JSON string or number into a string (MAX sends user_id as int or string).
type FlexID string

func (f *FlexID) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		*f = FlexID(str)
		return nil
	}
	*f = FlexID(s)
	return nil
}

// IncomingAttachment models a file/image/audio attachment received inside a MAX message.
type IncomingAttachment struct {
	Type     string `json:"type"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	URL      string `json:"url,omitempty"`
	Payload  struct {
		URL      string `json:"url,omitempty"`
		Token    string `json:"token,omitempty"`
		Filename string `json:"filename,omitempty"`
		Name     string `json:"name,omitempty"`
		Size     int64  `json:"size,omitempty"`
	} `json:"payload,omitempty"`
}

// WebhookEvent models incoming MAX Bot API webhook payloads.
type WebhookEvent struct {
	UpdateType string `json:"update_type"`
	Timestamp  int64  `json:"timestamp"`
	User       *struct {
		UserID    FlexID `json:"user_id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
	} `json:"user"`
	Message *struct {
		ID          string               `json:"id"`
		Mid         string               `json:"mid"`
		ChatID      FlexID               `json:"chat_id"`
		UserID      FlexID               `json:"user_id"`
		Text        string               `json:"text"`
		Attachments []IncomingAttachment `json:"attachments,omitempty"`
		Sender      *struct {
			UserID    FlexID `json:"user_id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"sender"`
		Body *struct {
			Mid         string               `json:"mid"`
			Text        string               `json:"text"`
			Attachments []IncomingAttachment `json:"attachments,omitempty"`
		} `json:"body"`
	} `json:"message"`
	Callback *struct {
		CallbackID string `json:"callback_id"`
		Payload    string `json:"payload"`
		User       struct {
			UserID    FlexID `json:"user_id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
		} `json:"user"`
	} `json:"callback"`
}

// PaymentCreator abstracts YooKassa payment creation and magic-link generation from the FSM.
type PaymentCreator interface {
	CreatePaymentForOrder(ctx context.Context, order db.Order, user db.User, tariff db.Tariff) (paymentURL string, err error)
}

// MagicLinkIssuer is an optional extension implemented by yukassa.Service to issue web cabinet magic links and emails.
type MagicLinkIssuer interface {
	IssueMagicLink(ctx context.Context, user db.User) (string, error)
	DeliverEmail(to, subject, body string)
}

// SessionStore abstracts database operations needed by the FSM engine.
type SessionStore interface {
	RecordWebhookEvent(ctx context.Context, provider, eventKey, eventType string) (bool, error)
	GetFSMSession(ctx context.Context, maxUserID string) (*db.BotFSMSession, error)
	SaveFSMSession(ctx context.Context, sess db.BotFSMSession) error
	ListTariffs(ctx context.Context, activeOnly bool) ([]db.Tariff, error)
	GetTariffByCode(ctx context.Context, code string) (*db.Tariff, error)
	UpsertUserByMaxOrEmail(ctx context.Context, maxUserID, maxUsername, fullName, phone, email, pregStatus string) (*db.User, error)
	CreateOrder(ctx context.Context, userID int64, tariff db.Tariff, consentAccepted bool) (*db.Order, error)
	CreateQuestion(ctx context.Context, maxUserID, authorName, text string) (*db.Question, error)
	MarkChannelMembershipByMaxUser(ctx context.Context, maxUserID string, active bool) error
}

// FullBotStore is an optional extended store interface implemented by *db.Store for full patient cabinet actions inside MAX bot.
type FullBotStore interface {
	GetUserByMaxUserID(ctx context.Context, maxUserID string) (*db.User, error)
	GetUserByEmail(ctx context.Context, email string) (*db.User, error)
	GetUserByID(ctx context.Context, id int64) (*db.User, error)
	LinkMaxUserToAccount(ctx context.Context, userID int64, maxUserID, maxUsername string) (*db.User, error)
	UpdateUserPersonalData(ctx context.Context, userID int64, fullName, phone, email, pregStatus string) (*db.User, error)
	CreateAuthToken(ctx context.Context, userID int64, tokenHash, tokenType, targetEmail string, expiresAt time.Time) error
	ConsumeAuthToken(ctx context.Context, tokenHash, tokenType string) (*db.User, error)
	GetLatestSubscription(ctx context.Context, userID int64) (*db.Subscription, error)
	ListOrders(ctx context.Context, userID int64, statusFilter string) ([]db.Order, error)
	GetOrderByID(ctx context.Context, id int64) (*db.Order, error)
	ListCabinetMaterials(ctx context.Context, userID int64) ([]db.CabinetMaterial, error)
	GetMaterialByID(ctx context.Context, id int) (*db.Material, error)
	GetClientMaterial(ctx context.Context, userID int64, materialID int) (*db.ClientMaterial, error)
	SetMaterialCompleted(ctx context.Context, userID int64, materialID int, completed bool) error
	ListConsultationSlots(ctx context.Context, includeDoctorNotes bool, clientID int64, statusFilter string) ([]db.ConsultationSlot, error)
	GetConsultationSlot(ctx context.Context, slotID int64, includeNotes bool) (*db.ConsultationSlot, error)
	BookConsultationSlot(ctx context.Context, slotID, clientID, orderID int64, question string) (*db.ConsultationSlot, error)
	CancelConsultationSlot(ctx context.Context, slotID, clientID int64, byAdmin bool) error
	UpdateSlotQuestion(ctx context.Context, slotID, clientID int64, question string) error
	AddConsultationAttachment(ctx context.Context, slotID, clientID int64, fileName, contentType string, size int64, s3Key string) (*db.ConsultationAttachment, error)
	ListConsultationAttachments(ctx context.Context, slotID int64) ([]db.ConsultationAttachment, error)
	ListClientAttachments(ctx context.Context, clientID int64) ([]db.ConsultationAttachment, error)
	ListPatientDocuments(ctx context.Context, clientID int64, publishedOnly bool) ([]db.PatientDocument, error)
	GetPatientDocument(ctx context.Context, docID int64) (*db.PatientDocument, error)
	ListChatMessages(ctx context.Context, clientID int64) ([]db.ChatMessage, error)
	CreateChatMessage(ctx context.Context, clientID, senderUserID int64, senderRole, rawBody string) (*db.ChatMessage, error)
	GetConsultationConfig(ctx context.Context) (db.ConsultationConfig, error)
}

// Engine executes the MAX bot dialog finite state machine.
type Engine struct {
	store         SessionStore
	bot           Messenger
	payment       PaymentCreator
	s3            *s3.Client
	mailer        mailer.Sender
	publicBaseURL string
	httpClient    *http.Client
}

// NewEngine creates an FSM engine.
func NewEngine(store SessionStore, bot Messenger, payment PaymentCreator) *Engine {
	return &Engine{
		store:      store,
		bot:        bot,
		payment:    payment,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// WithServices attaches optional S3, mailer, and public base URL dependencies for full cabinet features inside the bot.
func (e *Engine) WithServices(s3Client *s3.Client, mail mailer.Sender, publicBaseURL string) *Engine {
	e.s3 = s3Client
	e.mailer = mail
	e.publicBaseURL = strings.TrimRight(publicBaseURL, "/")
	return e
}

// HandleEvent processes one MAX webhook update idempotently.
func (e *Engine) HandleEvent(ctx context.Context, ev WebhookEvent) error {
	dedupKey := e.dedupKey(ev)
	if dedupKey != "" {
		isNew, err := e.store.RecordWebhookEvent(ctx, "max", dedupKey, ev.UpdateType)
		if err != nil {
			return err
		}
		if !isNew {
			log.Printf("[MAX FSM] duplicate event skipped key=%s", dedupKey)
			return nil
		}
	}

	switch ev.UpdateType {
	case "bot_started":
		if ev.User == nil || ev.User.UserID == "" {
			return nil
		}
		uid := string(ev.User.UserID)
		name := displayUserName(ev.User.FirstName, ev.User.LastName, ev.User.Username)
		return e.sendMainMenu(ctx, uid, "", name, false)

	case "message_created":
		uid, username, text, atts := extractMessageInfo(ev)
		if uid == "" {
			return nil
		}
		return e.handleIncomingMessage(ctx, uid, username, text, atts)

	case "message_callback":
		if ev.Callback == nil || ev.Callback.User.UserID == "" {
			return nil
		}
		uid := string(ev.Callback.User.UserID)
		name := displayUserName(ev.Callback.User.FirstName, ev.Callback.User.LastName, ev.Callback.User.Username)
		return e.handleCallback(ctx, uid, name, ev.Callback.CallbackID, ev.Callback.Payload)

	case "user_added":
		if ev.User != nil && ev.User.UserID != "" {
			return e.store.MarkChannelMembershipByMaxUser(ctx, string(ev.User.UserID), true)
		}
		return nil

	case "user_removed":
		if ev.User != nil && ev.User.UserID != "" {
			return e.store.MarkChannelMembershipByMaxUser(ctx, string(ev.User.UserID), false)
		}
		return nil

	default:
		log.Printf("[MAX FSM] unhandled update_type=%q", ev.UpdateType)
		return nil
	}
}

func (e *Engine) dedupKey(ev WebhookEvent) string {
	switch ev.UpdateType {
	case "message_callback":
		if ev.Callback != nil && ev.Callback.CallbackID != "" {
			return "cb:" + ev.Callback.CallbackID
		}
	case "message_created":
		if ev.Message != nil {
			if ev.Message.ID != "" {
				return "msg:" + ev.Message.ID
			}
			if ev.Message.Mid != "" {
				return "msg:" + ev.Message.Mid
			}
			if ev.Message.Body != nil && ev.Message.Body.Mid != "" {
				return "msg:" + ev.Message.Body.Mid
			}
		}
	case "bot_started":
		if ev.User != nil && ev.Timestamp > 0 {
			return fmt.Sprintf("start:%s:%d", ev.User.UserID, ev.Timestamp)
		}
	}
	return ""
}

func displayUserName(first, last, username string) string {
	if u := strings.TrimSpace(username); u != "" {
		return u
	}
	return strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
}

func extractMessageInfo(ev WebhookEvent) (uid, username, text string, atts []IncomingAttachment) {
	if ev.Message == nil {
		return "", "", "", nil
	}
	if ev.Message.UserID != "" {
		uid = string(ev.Message.UserID)
	} else if ev.Message.Sender != nil && ev.Message.Sender.UserID != "" {
		uid = string(ev.Message.Sender.UserID)
	}
	if ev.Message.Sender != nil {
		username = displayUserName(ev.Message.Sender.FirstName, ev.Message.Sender.LastName, ev.Message.Sender.Username)
	}
	if uid == "" && ev.User != nil {
		uid = string(ev.User.UserID)
	}
	if username == "" && ev.User != nil {
		username = displayUserName(ev.User.FirstName, ev.User.LastName, ev.User.Username)
	}
	text = ev.Message.Text
	if text == "" && ev.Message.Body != nil {
		text = ev.Message.Body.Text
	}
	atts = append(atts, ev.Message.Attachments...)
	if ev.Message.Body != nil && len(ev.Message.Body.Attachments) > 0 {
		atts = append(atts, ev.Message.Body.Attachments...)
	}
	return uid, username, strings.TrimSpace(text), atts
}

func (e *Engine) loadOrInitSession(ctx context.Context, uid, username string) (db.BotFSMSession, error) {
	sess, err := e.store.GetFSMSession(ctx, uid)
	if err != nil {
		return db.BotFSMSession{}, err
	}
	if sess == nil {
		return db.BotFSMSession{
			UserID:       uid,
			CurrentState: StateStart,
			ContextData:  map[string]interface{}{"username": username},
		}, nil
	}
	if sess.ContextData == nil {
		sess.ContextData = map[string]interface{}{}
	}
	if username != "" {
		sess.ContextData["username"] = username
	}
	return *sess, nil
}

func (e *Engine) fullStore() FullBotStore {
	if fs, ok := e.store.(FullBotStore); ok {
		return fs
	}
	return nil
}

func (e *Engine) resolveLinkedUser(ctx context.Context, uid string) (*db.User, *db.Subscription, bool) {
	fs := e.fullStore()
	if fs == nil {
		return nil, nil, false
	}
	u, err := fs.GetUserByMaxUserID(ctx, uid)
	if err != nil || u == nil {
		return nil, nil, false
	}
	sub, _ := fs.GetLatestSubscription(ctx, u.ID)
	subActive := sub != nil && sub.IsActive && sub.ExpiresAt.After(time.Now().UTC())
	return u, sub, subActive
}

func (e *Engine) sendOrAnswer(ctx context.Context, uid, callbackID string, msg OutgoingMessage, editInPlace bool) error {
	if editInPlace && callbackID != "" {
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})
	}
	return e.bot.SendMessage(ctx, uid, msg)
}

func (e *Engine) sendMainMenu(ctx context.Context, uid, callbackID, username string, editInPlace bool) error {
	sess, err := e.loadOrInitSession(ctx, uid, username)
	if err != nil {
		return err
	}
	sess.CurrentState = StateStart
	if callbackID != "" {
		sess.LastCallbackID = callbackID
	}
	if err := e.store.SaveFSMSession(ctx, sess); err != nil {
		return err
	}

	user, sub, subActive := e.resolveLinkedUser(ctx, uid)
	if user != nil {
		subLine := "Статус доступа: *нет активной подписки*"
		if subActive && sub != nil {
			msk := time.FixedZone("MSK", 3*3600)
			subLine = fmt.Sprintf("Тариф: *%s*\nДоступ активен до: *%s* (осталось %d дн.)",
				EscapeMarkdown(sub.TariffName),
				sub.ExpiresAt.In(msk).Format("02.01.2006"),
				sub.DaysLeft,
			)
		} else if sub != nil {
			subLine = fmt.Sprintf("Тариф: *%s* (срок доступа истёк — продлите подписку)", EscapeMarkdown(sub.TariffName))
		}
		pregLine := user.PregnancyStatus
		if strings.TrimSpace(pregLine) == "" {
			pregLine = "не указан (можно дозаполнить в профиле)"
		}

		text := fmt.Sprintf(
			"🥑 *Личный кабинет в MAX — «Беременность без тревоги»*\n\n"+
				"👩‍⚕️ Пациентка: *%s*\n"+
				"🤰 Срок / статус: *%s*\n"+
				"📞 Телефон: `%s` · E-mail: `%s`\n"+
				"💳 %s\n\n"+
				"Выберите нужный раздел:",
			EscapeMarkdown(user.FullName),
			EscapeMarkdown(pregLine),
			user.Phone,
			user.Email,
			subLine,
		)

		rows := [][]Button{
			{
				{Type: "callback", Text: "📚 Мои материалы и уроки", Payload: "cab:materials"},
				{Type: "callback", Text: "📅 Запись на консультацию", Payload: "cab:slots"},
			},
			{
				{Type: "callback", Text: "🧪 Добавить анализы / УЗИ", Payload: "cab:analyses"},
				{Type: "callback", Text: "📋 Мои заключения врача", Payload: "cab:docs"},
			},
			{
				{Type: "callback", Text: "💬 Чат с врачом", Payload: "cab:chat"},
				{Type: "callback", Text: "👤 Личные данные и профиль", Payload: "cab:profile"},
			},
			{
				{Type: "callback", Text: "💳 Тарифы и продление", Payload: "nav:tariffs"},
				{Type: "callback", Text: "🌐 Ссылка в веб-кабинет", Payload: "cab:web_link"},
			},
		}
		return e.sendOrAnswer(ctx, uid, callbackID, OutgoingMessage{
			Text:        text,
			Attachments: InlineKeyboard(rows...),
		}, editInPlace)
	}

	msg := OutgoingMessage{
		Text: "Здравствуйте! Я бот-помощник медицинского интенсива *«Беременность без тревоги»* 🥑\n\n" +
			"Через меня вы можете:\n" +
			"• Авторизоваться в своём профиле или оформить подписку (с согласием 152-ФЗ и оплатой ЮKassa);\n" +
			"• Получать учебные материалы, видео и именные PDF-памятки;\n" +
			"• Выбирать слоты и записываться на онлайн-консультации к врачу;\n" +
			"• Загружать результаты анализов, УЗИ и исследований;\n" +
			"• Получать готовые медицинские заключения и общаться с врачом.\n\n" +
			"Выберите действие:",
		Attachments: InlineKeyboard(
			[]Button{
				{Type: "callback", Text: "🔐 Войти в аккаунт (Авторизация)", Payload: "auth:start"},
			},
			[]Button{
				{Type: "callback", Text: "ℹ️ Подробности интенсива", Payload: "nav:about"},
				{Type: "callback", Text: "📋 Программа", Payload: "nav:program"},
			},
			[]Button{
				{Type: "callback", Text: "💳 Выбрать подписку и оплатить", Payload: "nav:tariffs"},
				{Type: "callback", Text: "❓ Задать вопрос", Payload: "nav:question"},
			},
		),
	}
	return e.sendOrAnswer(ctx, uid, callbackID, msg, editInPlace)
}

func (e *Engine) handleIncomingMessage(ctx context.Context, uid, username, text string, atts []IncomingAttachment) error {
	norm := strings.ToLower(strings.TrimSpace(text))
	switch norm {
	case "/start", "начать", "меню", "/menu", "кабинет", "/cabinet":
		return e.sendMainMenu(ctx, uid, "", username, false)
	case "профиль", "/profile":
		return e.showProfileMenu(ctx, uid, "", username, false)
	case "материалы", "уроки", "/materials":
		return e.showMaterialsList(ctx, uid, "", username, false)
	case "консультации", "запись", "слоты", "/slots":
		return e.showConsultationsMenu(ctx, uid, "", username, false)
	case "анализы", "исследования", "/analyses":
		return e.showAnalysesMenu(ctx, uid, "", username, false, 0)
	case "заключения", "документы", "/docs":
		return e.showDocumentsList(ctx, uid, "", username, false)
	case "тарифы", "подписка", "/tariffs":
		return e.showTariffsMenu(ctx, uid, "", username, false)
	}

	sess, err := e.loadOrInitSession(ctx, uid, username)
	if err != nil {
		return err
	}

	// If user attached files (photo, PDF, document) in any state or in StateUploadAnalysis, handle attachment upload
	if len(atts) > 0 && sess.CurrentState != StepInputName && sess.CurrentState != StepInputPhone && sess.CurrentState != StepInputEmail {
		return e.handleAttachmentUpload(ctx, uid, username, sess, text, atts)
	}

	switch sess.CurrentState {
	case StateAuthEmail:
		return e.handleAuthEmailInput(ctx, uid, username, sess, text)

	case StateAuthOTP:
		return e.handleAuthOTPInput(ctx, uid, username, sess, text)

	case StepEditName:
		name, err := ValidateFullName(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{Text: err.Error()})
		}
		return e.applyPersonalDataEdit(ctx, uid, username, sess, name, "", "", "")

	case StepEditPhone:
		phone, err := NormalizePhoneE164(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{Text: err.Error()})
		}
		return e.applyPersonalDataEdit(ctx, uid, username, sess, "", phone, "", "")

	case StepEditEmail:
		email, err := ValidateEmail(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{Text: err.Error()})
		}
		return e.applyPersonalDataEdit(ctx, uid, username, sess, "", "", email, "")

	case StepEditPregStatus:
		status, err := ParsePregnancyStatus(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{
				Text: err.Error(),
				Attachments: InlineKeyboard(
					[]Button{
						{Type: "callback", Text: "🌱 Планирую беременность", Payload: "profile:preg:planning"},
						{Type: "callback", Text: "🍼 После родов", Payload: "profile:preg:postpartum"},
					},
					[]Button{{Type: "callback", Text: "⬅️ Назад в профиль", Payload: "cab:profile"}},
				),
			})
		}
		return e.applyPersonalDataEdit(ctx, uid, username, sess, "", "", "", status)

	case StepBookSlotQuestion:
		return e.completeSlotBookingWithQuestion(ctx, uid, "", username, sess, text, false)

	case StepUpdateSlotQuestion:
		return e.completeSlotQuestionUpdate(ctx, uid, username, sess, text)

	case StateUploadAnalysis:
		return e.handleAttachmentUpload(ctx, uid, username, sess, text, atts)

	case StateDoctorChat:
		return e.handleDoctorChatInput(ctx, uid, username, sess, text)

	case StateQuestion:
		if len([]rune(text)) < 3 {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{
				Text:        "Пожалуйста, напишите ваш вопрос подробнее (минимум 3 символа) или вернитесь в главное меню.",
				Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
			})
		}
		authorName, _ := sess.ContextData["name"].(string)
		if authorName == "" {
			authorName = username
		}
		if _, err := e.store.CreateQuestion(ctx, uid, authorName, text); err != nil {
			return err
		}
		if fs := e.fullStore(); fs != nil {
			if u, _, _ := e.resolveLinkedUser(ctx, uid); u != nil {
				_, _ = fs.CreateChatMessage(ctx, u.ID, u.ID, "client", text)
			}
		}
		sess.CurrentState = StateStart
		_ = e.store.SaveFSMSession(ctx, sess)
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text: "Спасибо! Ваш вопрос передан врачу и службе заботы. Мы обязательно ответим вам в ближайшее время прямо в этот чат.",
			Attachments: InlineKeyboard(
				[]Button{{Type: "callback", Text: "💳 Стоимость и тарифы", Payload: "nav:tariffs"}},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		})

	case StepInputName:
		name, err := ValidateFullName(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{Text: err.Error()})
		}
		sess.ContextData["name"] = name
		sess.CurrentState = StepInputPhone
		if err := e.store.SaveFSMSession(ctx, sess); err != nil {
			return err
		}
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        fmt.Sprintf("Приятно познакомиться, *%s*!\n\nШаг 2/4. Укажите ваш контактный номер телефона для связи (например: `+7 999 123-45-67`):", EscapeMarkdown(name)),
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Отменить заявку", Payload: "nav:main_menu"}}),
		})

	case StepInputPhone:
		phone, err := NormalizePhoneE164(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{Text: err.Error()})
		}
		sess.ContextData["phone"] = phone
		sess.CurrentState = StepInputEmail
		if err := e.store.SaveFSMSession(ctx, sess); err != nil {
			return err
		}
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text:        "Шаг 3/4. Укажите ваш *e-mail*. На него придёт ссылка для входа в личный кабинет и электронный чек об оплате:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Отменить заявку", Payload: "nav:main_menu"}}),
		})

	case StepInputEmail:
		email, err := ValidateEmail(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{Text: err.Error()})
		}
		// Security: prevent unauthenticated checkout from hijacking an existing account (admin/doctor or another client)
		if fs := e.fullStore(); fs != nil {
			if existing, gErr := fs.GetUserByEmail(ctx, email); gErr == nil && existing != nil {
				if existing.Role != "client" {
					return e.bot.SendMessage(ctx, uid, OutgoingMessage{
						Text:        "Этот e-mail принадлежит служебному аккаунту и не может использоваться для клиентской заявки. Укажите ваш личный e-mail:",
						Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Отменить заявку", Payload: "nav:main_menu"}}),
					})
				}
				if existing.MaxUserID == nil || *existing.MaxUserID != uid {
					return e.bot.SendMessage(ctx, uid, OutgoingMessage{
						Text: fmt.Sprintf("Профиль с e-mail `%s` уже зарегистрирован.\n\nЧтобы продлить подписку или пользоваться кабинетом в боте, пожалуйста, сначала подтвердите вход по коду из письма:", email),
						Attachments: InlineKeyboard(
							[]Button{{Type: "callback", Text: "🔐 Войти по E-mail (код)", Payload: "auth:start"}},
							[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
						),
					})
				}
			}
		}
		sess.ContextData["email"] = email
		sess.CurrentState = StepInputPregStatus
		if err := e.store.SaveFSMSession(ctx, sess); err != nil {
			return err
		}
		return e.bot.SendMessage(ctx, uid, OutgoingMessage{
			Text: "Шаг 4/4. Укажите срок вашей беременности в неделях (от 1 до 42) или выберите вариант кнопкой:",
			Attachments: InlineKeyboard(
				[]Button{
					{Type: "callback", Text: "🌱 Планирую беременность", Payload: "status:planning"},
					{Type: "callback", Text: "🍼 После родов", Payload: "status:postpartum"},
				},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		})

	case StepInputPregStatus:
		status, err := ParsePregnancyStatus(text)
		if err != nil {
			return e.bot.SendMessage(ctx, uid, OutgoingMessage{
				Text: err.Error(),
				Attachments: InlineKeyboard(
					[]Button{
						{Type: "callback", Text: "🌱 Планирую беременность", Payload: "status:planning"},
						{Type: "callback", Text: "🍼 После родов", Payload: "status:postpartum"},
					},
				),
			})
		}
		return e.moveToOrderConfirmation(ctx, uid, "", sess, status, false)

	default:
		return e.sendMainMenu(ctx, uid, "", username, false)
	}
}

func (e *Engine) handleCallback(ctx context.Context, uid, username, callbackID, payload string) error {
	sess, err := e.loadOrInitSession(ctx, uid, username)
	if err != nil {
		return err
	}
	sess.LastCallbackID = callbackID

	switch {
	case payload == "nav:main_menu":
		return e.sendMainMenu(ctx, uid, callbackID, username, true)

	case payload == "nav:about":
		sess.CurrentState = StateAbout
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text: "*Интенсив «Беременность без тревоги»*\n\nАвтор интенсива — практикующий врач-терапевт, нефролог, нутрициолог и IV-терапевт.\n\nВы получите системную и научно обоснованную базу: как понимать сигналы организма, ориентироваться в анализах, выстроить питание и нутритивную поддержку без тревожности и мифов.",
			Attachments: InlineKeyboard(
				[]Button{
					{Type: "callback", Text: "📋 Программа", Payload: "nav:program"},
					{Type: "callback", Text: "💳 Тарифы", Payload: "nav:tariffs"},
				},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})

	case payload == "nav:program":
		sess.CurrentState = StateProgram
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text: "*Программа интенсива:*\n\n" +
				"• *Модуль 1. 1-й триместр* — токсикоз, адаптация, базовые дефициты, безопасность.\n" +
				"• *Модуль 2. 2-й триместр* — скрининги, анализы, профилактика отёков и гестозов.\n" +
				"• *Модуль 3. 3-й триместр* — подготовка почек и сосудов, контроль давления, подготовка к родам.\n" +
				"• *Модуль 4. Послеродовое восстановление* — восполнение ресурсов, лактация, чек-ап здоровья.",
			Attachments: InlineKeyboard(
				[]Button{
					{Type: "callback", Text: "💳 Выбрать тариф", Payload: "nav:tariffs"},
					{Type: "callback", Text: "❓ Задать вопрос", Payload: "nav:question"},
				},
				[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
			),
		}
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})

	case payload == "nav:question":
		sess.CurrentState = StateQuestion
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text: "Напишите ваш вопрос одним сообщением — мы передадим его врачу и ответим вам.",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		}
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})

	case payload == "nav:tariffs":
		return e.showTariffsMenu(ctx, uid, callbackID, username, true)

	case payload == "auth:start":
		sess.CurrentState = StateAuthEmail
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text: "🔐 *Авторизация в профиле «Беременность без тревоги»*\n\n" +
				"Введите ваш *e-mail*, который вы указывали при регистрации или покупке подписки.\n" +
				"Мы найдём ваш профиль и отправим код подтверждения для привязки бота MAX:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}}),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, true)

	case payload == "cab:profile":
		return e.showProfileMenu(ctx, uid, callbackID, username, true)

	case payload == "profile:edit:name":
		sess.CurrentState = StepEditName
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text:        "✏️ *Изменение ФИО*\n\nНапишите ваше имя и фамилию (например: `Иванова Анна Сергеевна`):",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Назад в профиль", Payload: "cab:profile"}}),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, true)

	case payload == "profile:edit:phone":
		sess.CurrentState = StepEditPhone
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text:        "📞 *Изменение номера телефона*\n\nВведите ваш номер телефона в формате `+7 999 123-45-67`:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Назад в профиль", Payload: "cab:profile"}}),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, true)

	case payload == "profile:edit:email":
		sess.CurrentState = StepEditEmail
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text:        "📧 *Изменение E-mail*\n\nВведите ваш актуальный адрес электронной почты:",
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Назад в профиль", Payload: "cab:profile"}}),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, true)

	case payload == "profile:edit:preg":
		sess.CurrentState = StepEditPregStatus
		_ = e.store.SaveFSMSession(ctx, sess)
		msg := OutgoingMessage{
			Text: "🤰 *Обновление срока беременности / статуса*\n\nНапишите текущий срок в неделях (число от 1 до 42) или выберите кнопку:",
			Attachments: InlineKeyboard(
				[]Button{
					{Type: "callback", Text: "🌱 Планирую беременность", Payload: "profile:preg:planning"},
					{Type: "callback", Text: "🍼 После родов", Payload: "profile:preg:postpartum"},
				},
				[]Button{{Type: "callback", Text: "⬅️ Назад в профиль", Payload: "cab:profile"}},
			),
		}
		return e.sendOrAnswer(ctx, uid, callbackID, msg, true)

	case payload == "profile:preg:planning" || payload == "profile:preg:postpartum":
		statusText, _ := ParsePregnancyStatus(strings.TrimPrefix(payload, "profile:preg:"))
		return e.applyPersonalDataEdit(ctx, uid, username, sess, "", "", "", statusText)

	case payload == "profile:consent":
		return e.showConsentInfo(ctx, uid, callbackID, username, true)

	case payload == "cab:materials":
		return e.showMaterialsList(ctx, uid, callbackID, username, true)

	case strings.HasPrefix(payload, "mat:view:"):
		matID, _ := strconv.Atoi(strings.TrimPrefix(payload, "mat:view:"))
		return e.showMaterialDetail(ctx, uid, callbackID, username, matID, true)

	case strings.HasPrefix(payload, "mat:done:"):
		matID, _ := strconv.Atoi(strings.TrimPrefix(payload, "mat:done:"))
		return e.toggleMaterialCompleted(ctx, uid, callbackID, username, matID, true)

	case strings.HasPrefix(payload, "mat:todo:"):
		matID, _ := strconv.Atoi(strings.TrimPrefix(payload, "mat:todo:"))
		return e.toggleMaterialCompleted(ctx, uid, callbackID, username, matID, false)

	case payload == "cab:slots":
		return e.showConsultationsMenu(ctx, uid, callbackID, username, true)

	case strings.HasPrefix(payload, "slot:pick:"):
		slotID, _ := strconv.ParseInt(strings.TrimPrefix(payload, "slot:pick:"), 10, 64)
		return e.promptSlotBookingQuestion(ctx, uid, callbackID, username, sess, slotID)

	case strings.HasPrefix(payload, "slot:book_now:"):
		slotID, _ := strconv.ParseInt(strings.TrimPrefix(payload, "slot:book_now:"), 10, 64)
		sess.ContextData["booking_slot_id"] = float64(slotID)
		return e.completeSlotBookingWithQuestion(ctx, uid, callbackID, username, sess, "", true)

	case strings.HasPrefix(payload, "slot:edit_q:"):
		slotID, _ := strconv.ParseInt(strings.TrimPrefix(payload, "slot:edit_q:"), 10, 64)
		return e.promptUpdateSlotQuestion(ctx, uid, callbackID, sess, slotID)

	case strings.HasPrefix(payload, "slot:cancel:"):
		slotID, _ := strconv.ParseInt(strings.TrimPrefix(payload, "slot:cancel:"), 10, 64)
		return e.cancelBookedSlotInBot(ctx, uid, callbackID, username, slotID)

	case payload == "cab:analyses":
		return e.showAnalysesMenu(ctx, uid, callbackID, username, true, 0)

	case strings.HasPrefix(payload, "ana:slot:"):
		slotID, _ := strconv.ParseInt(strings.TrimPrefix(payload, "ana:slot:"), 10, 64)
		return e.showAnalysesMenu(ctx, uid, callbackID, username, true, slotID)

	case payload == "cab:docs":
		return e.showDocumentsList(ctx, uid, callbackID, username, true)

	case strings.HasPrefix(payload, "doc:view:"):
		docID, _ := strconv.ParseInt(strings.TrimPrefix(payload, "doc:view:"), 10, 64)
		return e.showDocumentDetail(ctx, uid, callbackID, username, docID, true)

	case payload == "cab:chat":
		return e.showDoctorChatMenu(ctx, uid, callbackID, username, sess, true)

	case payload == "cab:web_link":
		return e.sendWebCabinetMagicLink(ctx, uid, callbackID, username, true)

	case strings.HasPrefix(payload, "buy:"):
		// Stale callback protection: only accept tariff selection from browse states
		if sess.CurrentState == StatePaymentPending || sess.CurrentState == StepConfirmPolicy {
			return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{
				Notification: "У вас уже оформляется заявка. Завершите оплату или вернитесь в главное меню.",
			})
		}
		code := strings.TrimPrefix(payload, "buy:")
		t, err := e.store.GetTariffByCode(ctx, code)
		if err != nil || !t.IsActive {
			return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Notification: "Тариф временно недоступен"})
		}
		sess.ContextData["tariff_code"] = t.Code
		sess.ContextData["tariff_title"] = t.Title
		sess.ContextData["tariff_price"] = t.PriceKopecks

		// If the user is already linked and has personal data filled, jump straight to 152-FZ Consent & Confirmation!
		if u, _, _ := e.resolveLinkedUser(ctx, uid); u != nil && u.FullName != "" && u.Phone != "" && u.Email != "" {
			sess.ContextData["name"] = u.FullName
			sess.ContextData["phone"] = u.Phone
			sess.ContextData["email"] = u.Email
			preg := u.PregnancyStatus
			if strings.TrimSpace(preg) == "" {
				preg = "Планирую беременность"
			}
			return e.moveToOrderConfirmation(ctx, uid, callbackID, sess, preg, true)
		}

		sess.CurrentState = StepInputName
		if err := e.store.SaveFSMSession(ctx, sess); err != nil {
			return err
		}
		msg := OutgoingMessage{
			Text:        fmt.Sprintf("Вы выбрали тариф *«%s»* (%d ₽).\n\nШаг 1/4. Как к вам обращаться? Напишите ваше *имя и фамилию*:", EscapeMarkdown(t.Title), t.PriceKopecks/100),
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Отменить", Payload: "nav:main_menu"}}),
		}
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})

	case payload == "order:edit_data":
		sess.CurrentState = StepInputName
		if err := e.store.SaveFSMSession(ctx, sess); err != nil {
			return err
		}
		tTitle, _ := sess.ContextData["tariff_title"].(string)
		msg := OutgoingMessage{
			Text:        fmt.Sprintf("Тариф *«%s»*.\n\nШаг 1/4. Напишите ваше *имя и фамилию*:", EscapeMarkdown(tTitle)),
			Attachments: InlineKeyboard([]Button{{Type: "callback", Text: "⬅️ Отменить", Payload: "nav:main_menu"}}),
		}
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})

	case payload == "status:planning" || payload == "status:postpartum":
		// Stale callback protection: must be on pregnancy status step
		if sess.CurrentState != StepInputPregStatus {
			return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Notification: "Этот шаг уже пройден"})
		}
		statusText, _ := ParsePregnancyStatus(strings.TrimPrefix(payload, "status:"))
		return e.moveToOrderConfirmation(ctx, uid, callbackID, sess, statusText, true)

	case payload == "order:confirm":
		// Stale callback protection: prevent duplicate order/payment creation
		if sess.CurrentState != StepConfirmPolicy {
			return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Notification: "Заявка уже подтверждена или устарела"})
		}
		return e.finalizeOrderAndSendPayment(ctx, uid, callbackID, sess)

	default:
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Notification: "Действие устарело, откройте меню /start"})
	}
}

func (e *Engine) moveToOrderConfirmation(ctx context.Context, uid, callbackID string, sess db.BotFSMSession, pregStatus string, editInPlace bool) error {
	sess.ContextData["pregnancy_status"] = pregStatus
	sess.CurrentState = StepConfirmPolicy
	if err := e.store.SaveFSMSession(ctx, sess); err != nil {
		return err
	}

	name, _ := sess.ContextData["name"].(string)
	phone, _ := sess.ContextData["phone"].(string)
	email, _ := sess.ContextData["email"].(string)
	tariffTitle, _ := sess.ContextData["tariff_title"].(string)
	price := int64(0)
	if v, ok := sess.ContextData["tariff_price"].(float64); ok {
		price = int64(v)
	} else if v, ok := sess.ContextData["tariff_price"].(int64); ok {
		price = v
	}

	summaryText := fmt.Sprintf(
		"*Проверьте данные заявки:*\n\n"+
			"• Тариф: *%s*\n"+
			"• Стоимость: *%d ₽*\n"+
			"• Имя: %s\n"+
			"• Телефон: `%s`\n"+
			"• E-mail: `%s`\n"+
			"• Срок / статус: %s\n\n"+
			"Нажимая кнопку ниже, вы подтверждаете согласие с Офертой и Политикой обработки персональных данных (152-ФЗ).",
		EscapeMarkdown(tariffTitle), price/100, EscapeMarkdown(name), phone, email, EscapeMarkdown(pregStatus),
	)
	msg := OutgoingMessage{
		Text: summaryText,
		Attachments: InlineKeyboard(
			[]Button{{Type: "callback", Text: "✅ Согласна (152-ФЗ) — к оплате", Payload: "order:confirm"}},
			[]Button{
				{Type: "callback", Text: "✏️ Изменить анкету", Payload: "order:edit_data"},
				{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"},
			},
		),
	}
	if editInPlace && callbackID != "" {
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})
	}
	return e.bot.SendMessage(ctx, uid, msg)
}

func (e *Engine) finalizeOrderAndSendPayment(ctx context.Context, uid, callbackID string, sess db.BotFSMSession) error {
	name, _ := sess.ContextData["name"].(string)
	phone, _ := sess.ContextData["phone"].(string)
	email, _ := sess.ContextData["email"].(string)
	pregStatus, _ := sess.ContextData["pregnancy_status"].(string)
	tariffCode, _ := sess.ContextData["tariff_code"].(string)
	username, _ := sess.ContextData["username"].(string)

	tariff, err := e.store.GetTariffByCode(ctx, tariffCode)
	if err != nil {
		return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Notification: "Тариф не найден"})
	}
	user, err := e.store.UpsertUserByMaxOrEmail(ctx, uid, username, name, phone, email, pregStatus)
	if err != nil {
		return err
	}
	order, err := e.store.CreateOrder(ctx, user.ID, *tariff, true)
	if err != nil {
		return err
	}

	payURL, err := e.payment.CreatePaymentForOrder(ctx, *order, *user, *tariff)
	if err != nil {
		return fmt.Errorf("create payment for order %s: %w", order.OrderNumber, err)
	}

	sess.CurrentState = StatePaymentPending
	sess.DraftOrderID = &order.ID
	if err := e.store.SaveFSMSession(ctx, sess); err != nil {
		return err
	}

	msg := OutgoingMessage{
		Text: fmt.Sprintf(
			"Заказ *%s* сформирован!\n\nТариф: *%s*\nК оплате: *%d ₽*\n\nСсылка на оплату действительна 30 минут. После оплаты чек НПД («Мой налог») и ссылка для входа в личный кабинет придут сюда и на почту `%s`.",
			order.OrderNumber, EscapeMarkdown(tariff.Title), order.AmountKopecks/100, user.Email,
		),
		Attachments: InlineKeyboard(
			[]Button{{Type: "link", Text: fmt.Sprintf("💳 Оплатить %d ₽ (ЮKassa)", order.AmountKopecks/100), URL: payURL}},
			[]Button{{Type: "callback", Text: "⬅️ Главное меню", Payload: "nav:main_menu"}},
		),
	}
	return e.bot.AnswerCallback(ctx, callbackID, AnswerBody{Message: &msg})
}

func shortenTariffTitle(t db.Tariff) string {
	months := t.DurationDays / 30
	if t.DurationDays >= 365 {
		months = 12
	}
	prefix := "Самостоятельный"
	if t.HasFeedback {
		prefix = "С ОС"
	}
	return fmt.Sprintf("%s (%d мес.)", prefix, months)
}

// MarkUserPaid updates FSM session state to STATE_PAID when payment succeeds.
func MarkUserPaid(ctx context.Context, store interface {
	GetFSMSession(ctx context.Context, maxUserID string) (*db.BotFSMSession, error)
	SaveFSMSession(ctx context.Context, sess db.BotFSMSession) error
}, maxUserID string, orderID int64) {
	if maxUserID == "" {
		return
	}
	sess, err := store.GetFSMSession(ctx, maxUserID)
	if err != nil || sess == nil {
		sess = &db.BotFSMSession{UserID: maxUserID, ContextData: map[string]interface{}{}}
	}
	sess.CurrentState = StatePaid
	sess.DraftOrderID = &orderID
	sess.UpdatedAt = time.Now().UTC()
	_ = store.SaveFSMSession(ctx, *sess)
}
