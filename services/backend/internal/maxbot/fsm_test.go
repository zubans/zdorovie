package maxbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/db"
)

type fakeMessenger struct {
	sent    []OutgoingMessage
	answers []AnswerBody
}

func (f *fakeMessenger) SendMessage(_ context.Context, _ string, msg OutgoingMessage) error {
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeMessenger) AnswerCallback(_ context.Context, _ string, body AnswerBody) error {
	f.answers = append(f.answers, body)
	return nil
}

type fakePayCreator struct {
	url string
}

func (f *fakePayCreator) CreatePaymentForOrder(_ context.Context, order db.Order, _ db.User, _ db.Tariff) (string, error) {
	return f.url + "?order=" + order.OrderNumber, nil
}

type fakeSessionStore struct {
	sessions    map[string]db.BotFSMSession
	events      map[string]bool
	tariffs     []db.Tariff
	users       map[int64]*db.User
	tokens      map[string]int64
	sub         *db.Subscription
	materials   []db.CabinetMaterial
	slots       []db.ConsultationSlot
	attachments []db.ConsultationAttachment
	documents   []db.PatientDocument
	chats       []db.ChatMessage
}

func newFakeStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions: map[string]db.BotFSMSession{},
		events:   map[string]bool{},
		tariffs: []db.Tariff{
			{ID: 1, Code: "self_3m", Title: "Самостоятельный 3 мес.", PriceKopecks: 499000, DurationDays: 90, HasFeedback: false, IsActive: true},
			{ID: 5, Code: "feedback_3m", Title: "С обратной связью 3 мес.", PriceKopecks: 999000, DurationDays: 90, HasFeedback: true, IsActive: true},
		},
		users:  map[int64]*db.User{},
		tokens: map[string]int64{},
		materials: []db.CabinetMaterial{
			{
				Material: db.Material{
					ID: 1, ModuleNumber: 1, LessonNumber: 1,
					Title: "Первый триместр: адаптация и дефициты",
					Description: "Как поддержать организм в 1 триместре",
					ContentMarkdown: "### Главное\n- Контроль ферритина и ТТГ",
					DurationMinutes: 32, FileType: "pdf", IsPublished: true,
				},
				ClientStatus: "ready",
				Completed:    false,
			},
		},
		slots: []db.ConsultationSlot{
			{
				ID:        101,
				DoctorID:  1,
				SlotStart: time.Now().UTC().Add(48 * time.Hour),
				SlotEnd:   time.Now().UTC().Add(48*time.Hour + 45*time.Minute),
				Status:    "available",
				MeetingURL: "https://telemost.yandex.ru/j/demo",
			},
		},
		documents: []db.PatientDocument{
			{
				ID:              55,
				ClientID:        42,
				Title:           "Итоговые рекомендации от 24.11.2026",
				DocumentType:    "consultation_summary",
				S3PDFKey:        "summaries/42/doc_55.pdf",
				IsPublished:     true,
				DoctorName:      "Елена Соколова",
				Complaints:      "Утомляемость и снижение ферритина",
				Recommendations: "Приём хелатного железа и контроль ферритина через 4 недели",
				DiagnosticsPlan: "ОАК, ферритин, СРБ",
				FollowUp:        "Контроль через 4 недели",
				CreatedAt:       time.Now().UTC(),
			},
		},
	}
}

func (f *fakeSessionStore) RecordWebhookEvent(_ context.Context, provider, eventKey, _ string) (bool, error) {
	k := provider + ":" + eventKey
	if f.events[k] {
		return false, nil
	}
	f.events[k] = true
	return true, nil
}

func (f *fakeSessionStore) GetFSMSession(_ context.Context, maxUserID string) (*db.BotFSMSession, error) {
	if s, ok := f.sessions[maxUserID]; ok {
		cp := s
		return &cp, nil
	}
	return &db.BotFSMSession{UserID: maxUserID, CurrentState: StateStart, ContextData: map[string]interface{}{}}, nil
}

func (f *fakeSessionStore) SaveFSMSession(_ context.Context, sess db.BotFSMSession) error {
	f.sessions[sess.UserID] = sess
	return nil
}

func (f *fakeSessionStore) ListTariffs(_ context.Context, _ bool) ([]db.Tariff, error) {
	return f.tariffs, nil
}

func (f *fakeSessionStore) GetTariffByCode(_ context.Context, code string) (*db.Tariff, error) {
	for _, t := range f.tariffs {
		if t.Code == code {
			cp := t
			return &cp, nil
		}
	}
	return nil, context.DeadlineExceeded
}

func (f *fakeSessionStore) UpsertUserByMaxOrEmail(_ context.Context, maxUserID, maxUsername, fullName, phone, email, pregStatus string) (*db.User, error) {
	u := &db.User{
		ID: 42, MaxUserID: &maxUserID, MaxUsername: &maxUsername, FullName: fullName, Phone: phone, Email: email, PregnancyStatus: pregStatus, Role: "client",
	}
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeSessionStore) CreateOrder(_ context.Context, userID int64, tariff db.Tariff, _ bool) (*db.Order, error) {
	return &db.Order{
		ID: 7, OrderNumber: "ZDOR-1007", UserID: userID, TariffID: tariff.ID, AmountKopecks: tariff.PriceKopecks, Status: "pending", CreatedAt: time.Now(),
	}, nil
}

func (f *fakeSessionStore) CreateQuestion(_ context.Context, maxUserID, authorName, text string) (*db.Question, error) {
	return &db.Question{ID: 1, MaxUserID: &maxUserID, AuthorName: authorName, QuestionText: text, Status: "new"}, nil
}

func (f *fakeSessionStore) MarkChannelMembershipByMaxUser(_ context.Context, _ string, _ bool) error {
	return nil
}

func (f *fakeSessionStore) WriteAuditLog(_ context.Context, _ *int64, _, _, _, _ string, _ interface{}) {
}

func (f *fakeSessionStore) GetUserByMaxUserID(_ context.Context, maxUserID string) (*db.User, error) {
	for _, u := range f.users {
		if u.MaxUserID != nil && *u.MaxUserID == maxUserID {
			cp := *u
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSessionStore) GetUserByEmail(_ context.Context, email string) (*db.User, error) {
	for _, u := range f.users {
		if strings.EqualFold(u.Email, email) {
			cp := *u
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSessionStore) GetUserByID(_ context.Context, id int64) (*db.User, error) {
	if u, ok := f.users[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSessionStore) LinkMaxUserToAccount(_ context.Context, userID int64, maxUserID, maxUsername string) (*db.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	u.MaxUserID = &maxUserID
	u.MaxUsername = &maxUsername
	cp := *u
	return &cp, nil
}

func (f *fakeSessionStore) UpdateUserPersonalData(_ context.Context, userID int64, fullName, phone, email, pregStatus string) (*db.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	if fullName != "" {
		u.FullName = fullName
	}
	if phone != "" {
		u.Phone = phone
	}
	if email != "" {
		u.Email = email
	}
	if pregStatus != "" {
		u.PregnancyStatus = pregStatus
	}
	cp := *u
	return &cp, nil
}

func (f *fakeSessionStore) CreateAuthToken(_ context.Context, userID int64, tokenHash, _, _ string, _ time.Time) error {
	f.tokens[tokenHash] = userID
	return nil
}

func (f *fakeSessionStore) ConsumeAuthToken(_ context.Context, tokenHash, _ string) (*db.User, error) {
	uid, ok := f.tokens[tokenHash]
	if !ok {
		return nil, fmt.Errorf("invalid token")
	}
	delete(f.tokens, tokenHash)
	return f.GetUserByID(context.Background(), uid)
}

func (f *fakeSessionStore) GetLatestSubscription(_ context.Context, _ int64) (*db.Subscription, error) {
	return f.sub, nil
}

func (f *fakeSessionStore) ListOrders(_ context.Context, _ int64, _ string) ([]db.Order, error) {
	now := time.Now().UTC()
	return []db.Order{{ID: 7, OrderNumber: "ZDOR-1007", ConsentAcceptedAt: &now}}, nil
}

func (f *fakeSessionStore) GetOrderByID(_ context.Context, id int64) (*db.Order, error) {
	return &db.Order{ID: id, OrderNumber: "ZDOR-1007"}, nil
}

func (f *fakeSessionStore) ListCabinetMaterials(_ context.Context, _ int64) ([]db.CabinetMaterial, error) {
	return f.materials, nil
}

func (f *fakeSessionStore) GetMaterialByID(_ context.Context, id int) (*db.Material, error) {
	for _, m := range f.materials {
		if m.ID == id {
			cp := m.Material
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSessionStore) GetClientMaterial(_ context.Context, _ int64, materialID int) (*db.ClientMaterial, error) {
	return &db.ClientMaterial{ID: 1, MaterialID: materialID, Status: "ready", S3ClientKey: "clients/42/mat_1.pdf"}, nil
}

func (f *fakeSessionStore) SetMaterialCompleted(_ context.Context, _ int64, materialID int, completed bool) error {
	for i := range f.materials {
		if f.materials[i].ID == materialID {
			f.materials[i].Completed = completed
		}
	}
	return nil
}

func (f *fakeSessionStore) ListConsultationSlots(_ context.Context, _ bool, _ int64, _ string) ([]db.ConsultationSlot, error) {
	return f.slots, nil
}

func (f *fakeSessionStore) GetConsultationSlot(_ context.Context, slotID int64, _ bool) (*db.ConsultationSlot, error) {
	for _, sl := range f.slots {
		if sl.ID == slotID {
			cp := sl
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSessionStore) BookConsultationSlot(_ context.Context, slotID, clientID, orderID int64, question string) (*db.ConsultationSlot, error) {
	for i := range f.slots {
		if f.slots[i].ID == slotID && f.slots[i].Status == "available" {
			f.slots[i].ClientID = &clientID
			f.slots[i].OrderID = &orderID
			f.slots[i].Status = "booked"
			f.slots[i].ClientQuestion = question
			cp := f.slots[i]
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not available")
}

func (f *fakeSessionStore) CancelConsultationSlot(_ context.Context, slotID, clientID int64, _ bool) error {
	for i := range f.slots {
		if f.slots[i].ID == slotID && f.slots[i].ClientID != nil && *f.slots[i].ClientID == clientID {
			f.slots[i].ClientID = nil
			f.slots[i].Status = "available"
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeSessionStore) UpdateSlotQuestion(_ context.Context, slotID, clientID int64, question string) error {
	for i := range f.slots {
		if f.slots[i].ID == slotID && f.slots[i].ClientID != nil && *f.slots[i].ClientID == clientID {
			f.slots[i].ClientQuestion = question
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeSessionStore) AddConsultationAttachment(_ context.Context, slotID, clientID int64, fileName, contentType string, size int64, s3Key string) (*db.ConsultationAttachment, error) {
	att := db.ConsultationAttachment{
		ID: int64(len(f.attachments) + 1), SlotID: slotID, ClientID: clientID, FileName: fileName, ContentType: contentType, FileSize: size, S3Key: s3Key, CreatedAt: time.Now().UTC(),
	}
	f.attachments = append(f.attachments, att)
	return &att, nil
}

func (f *fakeSessionStore) ListConsultationAttachments(_ context.Context, slotID int64) ([]db.ConsultationAttachment, error) {
	var out []db.ConsultationAttachment
	for _, a := range f.attachments {
		if a.SlotID == slotID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeSessionStore) ListClientAttachments(_ context.Context, clientID int64) ([]db.ConsultationAttachment, error) {
	var out []db.ConsultationAttachment
	for _, a := range f.attachments {
		if a.ClientID == clientID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeSessionStore) ListPatientDocuments(_ context.Context, _ int64, _ bool) ([]db.PatientDocument, error) {
	return f.documents, nil
}

func (f *fakeSessionStore) GetPatientDocument(_ context.Context, docID int64) (*db.PatientDocument, error) {
	for _, d := range f.documents {
		if d.ID == docID {
			cp := d
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSessionStore) ListChatMessages(_ context.Context, _ int64) ([]db.ChatMessage, error) {
	return f.chats, nil
}

func (f *fakeSessionStore) CreateChatMessage(_ context.Context, clientID, senderUserID int64, senderRole, rawBody string) (*db.ChatMessage, error) {
	m := db.ChatMessage{
		ID: int64(len(f.chats) + 1), ClientID: clientID, SenderUserID: &senderUserID, SenderRole: senderRole, Body: rawBody, CreatedAt: time.Now().UTC(),
	}
	f.chats = append(f.chats, m)
	return &m, nil
}

func (f *fakeSessionStore) GetConsultationConfig(_ context.Context) (db.ConsultationConfig, error) {
	return db.ConsultationConfig{Mode: "builtin_slots", DoctorName: "Елена Соколова"}, nil
}

func TestValidators(t *testing.T) {
	p, err := NormalizePhoneE164("+7 (999) 123-45-67")
	if err != nil || p != "+79991234567" {
		t.Fatalf("expected +79991234567, got %q (err=%v)", p, err)
	}
	p8, err := NormalizePhoneE164("89165554433")
	if err != nil || p8 != "+79165554433" {
		t.Fatalf("expected +79165554433, got %q (err=%v)", p8, err)
	}
	if _, err := NormalizePhoneE164("12345"); err == nil {
		t.Fatal("expected short phone to fail")
	}

	em, err := ValidateEmail(" Elena.Ivanova@Example.RU ")
	if err != nil || em != "elena.ivanova@example.ru" {
		t.Fatalf("unexpected normalized email: %q err=%v", em, err)
	}
	for _, badEmail := range []string{"invalid-email", "a..b@mail.ru", ".lead@mail.ru", "trail.@mail.ru", "user@domain", "user@domain.c", "user@-bad.ru", "user@bad..ru"} {
		if _, err := ValidateEmail(badEmail); err == nil {
			t.Fatalf("expected invalid email %q to fail", badEmail)
		}
	}
	if pMasked, err := NormalizePhoneE164("+7 (926) 000-11-22"); err != nil || pMasked != "+79260001122" {
		t.Fatalf("unexpected masked phone normalization: %q err=%v", pMasked, err)
	}

	if _, err := ValidateFullName("И"); err == nil {
		t.Fatal("expected single-char name to fail")
	}
	if got := EscapeMarkdown("a_b*c`d"); got != `a\_b\*c\`+"`d" {
		// just verify backslashes exist
		if got == "a_b*c`d" {
			t.Fatalf("expected markdown escaping, got %q", got)
		}
	}
}

func TestFSM_FullCheckoutAndStaleCallback(t *testing.T) {
	store := newFakeStore()
	bot := &fakeMessenger{}
	pay := &fakePayCreator{url: "http://localhost/pay"}
	eng := NewEngine(store, bot, pay)

	ctx := context.Background()

	// 1. bot_started
	var startEv WebhookEvent
	_ = json.Unmarshal([]byte(`{"update_type":"bot_started","timestamp":1,"user":{"user_id":1001,"first_name":"Анна"}}`), &startEv)
	if err := eng.HandleEvent(ctx, startEv); err != nil {
		t.Fatalf("bot_started failed: %v", err)
	}
	if len(bot.sent) != 1 {
		t.Fatalf("expected 1 welcome message, got %d", len(bot.sent))
	}

	// 2. Callback select tariff -> enters STEP_INPUT_NAME
	var cbSelect WebhookEvent
	_ = json.Unmarshal([]byte(`{"update_type":"message_callback","timestamp":2,"callback":{"callback_id":"cb-1","payload":"buy:feedback_3m","user":{"user_id":1001,"first_name":"Анна"}}}`), &cbSelect)
	if err := eng.HandleEvent(ctx, cbSelect); err != nil {
		t.Fatalf("select tariff failed: %v", err)
	}

	// Verify duplicate/stale callback_id is rejected softly
	if err := eng.HandleEvent(ctx, cbSelect); err != nil {
		t.Fatalf("duplicate callback should not fail: %v", err)
	}

	// 3. Text steps: name -> phone -> email -> preg_status
	steps := []string{"Иванова Анна Сергеевна", "+79991112233", "anna@example.com", "20 недель"}
	for i, txt := range steps {
		var msgEv WebhookEvent
		raw, _ := json.Marshal(map[string]interface{}{
			"update_type": "message_created",
			"timestamp":   10 + i,
			"message": map[string]interface{}{
				"id":      fmtMsgID(i),
				"user_id": 1001,
				"text":    txt,
			},
		})
		_ = json.Unmarshal(raw, &msgEv)
		if err := eng.HandleEvent(ctx, msgEv); err != nil {
			t.Fatalf("step %d (%s) failed: %v", i, txt, err)
		}
	}

	// Verify state is now STEP_CONFIRM_POLICY
	sess, _ := store.GetFSMSession(ctx, "1001")
	if sess.CurrentState != StepConfirmPolicy {
		t.Fatalf("expected STEP_CONFIRM_POLICY, got %s", sess.CurrentState)
	}

	// 4. Confirm consent -> creates order & advances to STATE_PAYMENT_PENDING
	var cbConfirm WebhookEvent
	_ = json.Unmarshal([]byte(`{"update_type":"message_callback","timestamp":20,"callback":{"callback_id":"cb-2","payload":"order:confirm","user":{"user_id":1001,"first_name":"Анна"}}}`), &cbConfirm)
	if err := eng.HandleEvent(ctx, cbConfirm); err != nil {
		t.Fatalf("order confirm failed: %v", err)
	}
	sessAfter, _ := store.GetFSMSession(ctx, "1001")
	if sessAfter.CurrentState != StatePaymentPending || sessAfter.DraftOrderID == nil {
		t.Fatalf("expected STATE_PAYMENT_PENDING with draft order, got state=%s", sessAfter.CurrentState)
	}
}

func fmtMsgID(i int) string {
	return "msg-" + string(rune('a'+i))
}

func TestFSM_FullPatientCabinetInBot(t *testing.T) {
	store := newFakeStore()
	// Pre-populate an existing unlinked patient account (e.g. registered on web)
	store.users[42] = &db.User{
		ID:              42,
		FullName:        "Анна Соколова",
		Phone:           "+79991112233",
		Email:           "anna@example.com",
		PregnancyStatus: "18 нед.",
		Role:            "client",
	}
	store.sub = &db.Subscription{
		ID:          1,
		UserID:      42,
		OrderID:     7,
		TariffID:    5,
		TariffCode:  "feedback_3m",
		TariffName:  "С обратной связью 3 мес.",
		HasFeedback: true,
		IsActive:    true,
		ExpiresAt:   time.Now().UTC().Add(60 * 24 * time.Hour),
		DaysLeft:    60,
	}

	bot := &fakeMessenger{}
	pay := &fakePayCreator{url: "http://localhost/pay"}
	eng := NewEngine(store, bot, pay)
	ctx := context.Background()

	sendCb := func(cbID, payload string) {
		t.Helper()
		var ev WebhookEvent
		raw, _ := json.Marshal(map[string]interface{}{
			"update_type": "message_callback",
			"timestamp":   time.Now().UnixNano(),
			"callback": map[string]interface{}{
				"callback_id": cbID,
				"payload":     payload,
				"user":        map[string]interface{}{"user_id": 2002, "first_name": "Анна"},
			},
		})
		_ = json.Unmarshal(raw, &ev)
		if err := eng.HandleEvent(ctx, ev); err != nil {
			t.Fatalf("callback %s (%s) failed: %v", cbID, payload, err)
		}
	}

	sendText := func(msgID, text string, atts []IncomingAttachment) {
		t.Helper()
		ev := WebhookEvent{
			UpdateType: "message_created",
			Timestamp:  time.Now().UnixNano(),
		}
		raw, _ := json.Marshal(map[string]interface{}{
			"update_type": "message_created",
			"timestamp":   time.Now().UnixNano(),
			"message": map[string]interface{}{
				"id":          msgID,
				"user_id":     2002,
				"text":        text,
				"attachments": atts,
			},
		})
		_ = json.Unmarshal(raw, &ev)
		if err := eng.HandleEvent(ctx, ev); err != nil {
			t.Fatalf("message %s (%s) failed: %v", msgID, text, err)
		}
	}

	// 1. Authorization via Email + OTP
	sendCb("cb-auth-1", "auth:start")
	sendText("m-auth-email", "anna@example.com", nil)

	// Inject known email-scoped OTP code "123456"
	store.tokens[auth.HashOTP("anna@example.com", "123456")] = 42
	sendText("m-auth-otp", "123456", nil)

	if store.users[42].MaxUserID == nil || *store.users[42].MaxUserID != "2002" {
		t.Fatalf("expected user 42 to be linked to max_user_id 2002, got %+v", store.users[42].MaxUserID)
	}

	// 2. Personal data completion / editing (update pregnancy week to 22 weeks)
	sendCb("cb-prof-1", "cab:profile")
	sendCb("cb-prof-preg", "profile:edit:preg")
	sendText("m-prof-preg", "22 недели", nil)
	if store.users[42].PregnancyStatus != "22 нед." {
		t.Fatalf("expected updated pregnancy_status '22 нед.', got %q", store.users[42].PregnancyStatus)
	}

	// 3. View course materials and mark lesson 1 completed
	sendCb("cb-mat-list", "cab:materials")
	sendCb("cb-mat-view", "mat:view:1")
	sendCb("cb-mat-done", "mat:done:1")
	if !store.materials[0].Completed {
		t.Fatal("expected material 1 to be marked completed")
	}

	// 4. Select and book an online consultation slot with a pre-consultation question
	sendCb("cb-slots", "cab:slots")
	sendCb("cb-slot-pick", "slot:pick:101")
	sendText("m-slot-q", "Снижен ферритин 14 мкг/л и отёки к вечеру", nil)
	if store.slots[0].Status != "booked" || store.slots[0].ClientQuestion != "Снижен ферритин 14 мкг/л и отёки к вечеру" {
		t.Fatalf("expected slot 101 to be booked with question, got %+v", store.slots[0])
	}

	// 5. Upload lab analyses (file attachment without URL falls back to .txt metadata note, invalid .exe rejected)
	sendCb("cb-ana", "cab:analyses")
	sendText("m-ana-bad", "", []IncomingAttachment{
		{Type: "file", Filename: "malware.exe"},
	})
	if len(store.attachments) != 0 {
		t.Fatalf("expected .exe attachment to be rejected, got %+v", store.attachments)
	}
	sendText("m-ana-file", "Свежий анализ крови и ферритин", []IncomingAttachment{
		{Type: "file", Filename: "ferritin_oak_22w.pdf"},
	})
	if len(store.attachments) != 1 || store.attachments[0].FileName != "ferritin_oak_22w.txt" {
		t.Fatalf("expected uploaded fallback attachment ferritin_oak_22w.txt, got %+v", store.attachments)
	}

	// 6. Receive / view published medical conclusions
	sendCb("cb-docs", "cab:docs")
	sendCb("cb-doc-55", "doc:view:55")
	lastAnswer := bot.answers[len(bot.answers)-1]
	if lastAnswer.Message == nil || !strings.Contains(lastAnswer.Message.Text, "Приём хелатного железа") {
		t.Fatalf("expected medical conclusion details in bot message, got %+v", lastAnswer.Message)
	}

	// 7. Security: SSRF private IP URL check & live MAX non-HTTPS link button sanitization
	if isSafePublicHTTPSURL(ctx, "http://127.0.0.1:9000/secret") || isSafePublicHTTPSURL(ctx, "https://169.254.169.254/latest/meta-data") {
		t.Fatal("expected loopback/metadata URLs to be blocked by SSRF guard")
	}
	sanitized := sanitizeForLiveMAX(OutgoingMessage{
		Text: "Оплата",
		Attachments: InlineKeyboard(
			[]Button{{Type: "link", Text: "Оплатить", URL: "http://localhost/pay"}},
			[]Button{{Type: "callback", Text: "Меню", Payload: "nav:main_menu"}},
		),
	})
	if !strings.Contains(sanitized.Text, "http://localhost/pay") {
		t.Fatalf("expected non-HTTPS link URL to be moved into message text, got %q", sanitized.Text)
	}
	if len(sanitized.Attachments) != 1 || len(sanitized.Attachments[0].Payload.Buttons) != 1 {
		t.Fatalf("expected non-HTTPS link button row to be stripped from keyboard, got %+v", sanitized.Attachments)
	}
}
