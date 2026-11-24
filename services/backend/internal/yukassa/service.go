package yukassa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/mailer"
	"zdorovie/backend/internal/maxbot"
	"zdorovie/backend/internal/queue"
)

// YooKassa official webhook notification CIDR ranges (including IPv4 & IPv6).
var officialYooCIDRs = []string{
	"185.71.76.0/27",
	"185.71.77.0/27",
	"77.75.153.0/25",
	"77.75.156.11/32",
	"77.75.156.35/32",
	"2a02:5180::/32",
}

// Store abstracts persistence methods needed by the YooKassa payment service.
type Store interface {
	UpsertPayment(ctx context.Context, orderID int64, yooPaymentID, status string, amountKopecks int64, confirmURL, receiptURL string, expiresAt *time.Time, raw []byte) (*db.Payment, error)
	MarkPaymentSucceeded(ctx context.Context, yooPaymentID string, orderIDHint int64, receiptURL string, rawPayload json.RawMessage) (*db.PaymentSuccessResult, error)
	MarkOrderCanceled(ctx context.Context, yooPaymentID string, orderIDHint int64) error
	RecordWebhookEvent(ctx context.Context, provider, eventKey, eventType string) (bool, error)
	CreateAuthToken(ctx context.Context, userID int64, tokenHash, tokenType, targetEmail string, expiresAt time.Time) error
	ListPendingPaymentsForReconcile(ctx context.Context) ([]db.PendingPaymentForReconcile, error)
	GetFSMSession(ctx context.Context, maxUserID string) (*db.BotFSMSession, error)
	SaveFSMSession(ctx context.Context, sess db.BotFSMSession) error
	WriteAuditLog(ctx context.Context, actorUserID *int64, actorRole, action, entityType, entityID string, details interface{})
}

// YooPaymentObject models key fields of YooKassa v3 Payment JSON.
type YooPaymentObject struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Paid         bool   `json:"paid"`
	Confirmation *struct {
		Type            string `json:"type"`
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation,omitempty"`
	Amount *struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	ReceiptRegistration string            `json:"receipt_registration,omitempty"`
}

// Service implements YooKassa payment creation, NPD fiscalization, webhook processing, and reconciliation.
type Service struct {
	cfg        config.Config
	store      Store
	queue      *queue.Producer
	mailer     mailer.Sender
	bot        maxbot.Messenger
	httpClient *http.Client
	cidrs      []*net.IPNet
}

// NewService constructs a YooKassa service instance.
func NewService(cfg config.Config, store Store, q *queue.Producer, mail mailer.Sender, bot maxbot.Messenger) *Service {
	var nets []*net.IPNet
	for _, raw := range officialYooCIDRs {
		if _, n, err := net.ParseCIDR(raw); err == nil {
			nets = append(nets, n)
		}
	}
	return &Service{
		cfg:        cfg,
		store:      store,
		queue:      q,
		mailer:     mail,
		bot:        bot,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		cidrs:      nets,
	}
}

// IsAllowedWebhookIP checks remote / proxy IP against official YooKassa CIDRs when YOO_VERIFY_IP is enabled.
func (s *Service) IsAllowedWebhookIP(remoteAddr string, headers http.Header) bool {
	if !s.cfg.YooVerifyIP {
		return true
	}
	candidates := []string{}
	if realIP := strings.TrimSpace(headers.Get("X-Real-IP")); realIP != "" {
		candidates = append(candidates, realIP)
	}
	if xff := strings.TrimSpace(headers.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		candidates = append(candidates, strings.TrimSpace(parts[0]))
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	candidates = append(candidates, host)

	for _, c := range candidates {
		ip := net.ParseIP(strings.TrimSpace(c))
		if ip == nil {
			continue
		}
		for _, n := range s.cidrs {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	return false
}

// BuildCreatePaymentPayload builds the YooKassa v3 payment request body including NPD receipt.
func (s *Service) BuildCreatePaymentPayload(order db.Order, user db.User, tariff db.Tariff) map[string]interface{} {
	amountStr := fmt.Sprintf("%.2f", float64(order.AmountKopecks)/100.0)
	customer := map[string]interface{}{}
	if strings.TrimSpace(user.Email) != "" {
		customer["email"] = strings.TrimSpace(user.Email)
	}
	if strings.TrimSpace(user.Phone) != "" {
		customer["phone"] = strings.TrimPrefix(strings.TrimSpace(user.Phone), "+")
	}
	if strings.TrimSpace(user.FullName) != "" {
		customer["full_name"] = strings.TrimSpace(user.FullName)
	}

	return map[string]interface{}{
		"amount": map[string]string{
			"value":    amountStr,
			"currency": "RUB",
		},
		"capture":     true,
		"description": fmt.Sprintf("Оплата тарифа «%s» — заказ %s", tariff.Title, order.OrderNumber),
		"confirmation": map[string]string{
			"type":       "redirect",
			"return_url": s.cfg.YooReturnURL,
		},
		"metadata": map[string]string{
			"order_id":     strconv.FormatInt(order.ID, 10),
			"order_number": order.OrderNumber,
			"user_id":      strconv.FormatInt(user.ID, 10),
			"tariff_code":  tariff.Code,
		},
		"receipt": map[string]interface{}{
			"customer": customer,
			"items": []map[string]interface{}{
				{
					"description": fmt.Sprintf("Доступ к интенсиву «Беременность без тревоги» (%s)", tariff.Title),
					"quantity":    "1.00",
					"amount": map[string]string{
						"value":    amountStr,
						"currency": "RUB",
					},
					"vat_code":        1, // Без НДС (режим НПД / самозанятый)
					"payment_mode":    "full_prepayment",
					"payment_subject": "service",
				},
			},
		},
	}
}

// CreatePaymentForOrder creates a YooKassa payment (with NPD receipt) or a mock payment in local dev mode.
func (s *Service) CreatePaymentForOrder(ctx context.Context, order db.Order, user db.User, tariff db.Tariff) (string, error) {
	payloadMap := s.BuildCreatePaymentPayload(order, user, tariff)
	expiresAt := time.Now().UTC().Add(30 * time.Minute)

	if s.cfg.YooMode != "live" || s.cfg.YooShopID == "" || strings.HasPrefix(s.cfg.YooSecretKey, "change_me") || strings.Contains(s.cfg.YooShopID, "123456") {
		yooID := fmt.Sprintf("yoo-mock-%s-%d", order.OrderNumber, time.Now().Unix())
		confirmURL := fmt.Sprintf("%s/api/v1/dev/mock-pay?payment_id=%s&order_id=%d",
			strings.TrimRight(s.cfg.PublicBaseURL, "/"),
			url.QueryEscape(yooID),
			order.ID,
		)
		mockObj := map[string]interface{}{
			"id":     yooID,
			"status": "pending",
			"paid":   false,
			"confirmation": map[string]string{
				"type":             "redirect",
				"confirmation_url": confirmURL,
			},
			"request": payloadMap,
		}
		raw, _ := json.Marshal(mockObj)
		if _, err := s.store.UpsertPayment(ctx, order.ID, yooID, "pending", order.AmountKopecks, confirmURL, "", &expiresAt, raw); err != nil {
			return "", err
		}
		return confirmURL, nil
	}

	bodyBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.yookassa.ru/v3/payments", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(s.cfg.YooShopID, s.cfg.YooSecretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotence-Key", order.OrderNumber)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("yookassa request failed: %w", err)
	}
	defer resp.Body.Close()

	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("yookassa API error HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}

	var yooObj YooPaymentObject
	if err := json.Unmarshal(respRaw, &yooObj); err != nil {
		return "", fmt.Errorf("decode yookassa response: %w", err)
	}
	confirmURL := ""
	if yooObj.Confirmation != nil {
		confirmURL = yooObj.Confirmation.ConfirmationURL
	}
	if _, err := s.store.UpsertPayment(ctx, order.ID, yooObj.ID, yooObj.Status, order.AmountKopecks, confirmURL, "", &expiresAt, respRaw); err != nil {
		return "", err
	}
	return confirmURL, nil
}

// GetPayment verifies current payment state against YooKassa API.
func (s *Service) GetPayment(ctx context.Context, yooPaymentID string) (*YooPaymentObject, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.yookassa.ru/v3/payments/"+url.PathEscape(yooPaymentID), nil)
	if err != nil {
		return nil, nil, err
	}
	req.SetBasicAuth(s.cfg.YooShopID, s.cfg.YooSecretKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, raw, fmt.Errorf("yookassa GET payment HTTP %d", resp.StatusCode)
	}
	var obj YooPaymentObject
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, raw, err
	}
	return &obj, raw, nil
}

// HandleWebhook processes YooKassa asynchronous webhook notifications idempotently.
func (s *Service) HandleWebhook(ctx context.Context, body []byte) (*db.PaymentSuccessResult, error) {
	var envelope struct {
		Type   string           `json:"type"`
		Event  string           `json:"event"`
		Object YooPaymentObject `json:"object"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid webhook JSON: %w", err)
	}
	if envelope.Object.ID == "" {
		return nil, errors.New("missing payment object id")
	}

	eventKey := envelope.Object.ID + ":" + envelope.Event
	isNew, err := s.store.RecordWebhookEvent(ctx, "yukassa", eventKey, envelope.Event)
	if err != nil {
		return nil, err
	}
	if !isNew {
		return &db.PaymentSuccessResult{AlreadyPaid: true}, nil
	}

	rawObj := json.RawMessage(body)
	payObj := &envelope.Object

	// In live mode, verify status directly via YooKassa API to defeat spoofed payloads.
	if s.cfg.YooMode == "live" && s.cfg.YooShopID != "" && !strings.HasPrefix(s.cfg.YooSecretKey, "change_me") {
		verified, verifiedRaw, err := s.GetPayment(ctx, envelope.Object.ID)
		if err != nil {
			return nil, fmt.Errorf("verify payment via yookassa API: %w", err)
		}
		payObj = verified
		rawObj = verifiedRaw
	}

	var orderIDHint int64
	if rawOID, ok := payObj.Metadata["order_id"]; ok {
		orderIDHint, _ = strconv.ParseInt(rawOID, 10, 64)
	}

	switch {
	case envelope.Event == "payment.succeeded" || payObj.Status == "succeeded":
		return s.ProcessSuccess(ctx, payObj.ID, orderIDHint, "", rawObj)
	case envelope.Event == "payment.canceled" || payObj.Status == "canceled":
		return nil, s.store.MarkOrderCanceled(ctx, payObj.ID, orderIDHint)
	default:
		return nil, nil
	}
}

// ProcessSuccess finalizes payment confirmation, spawns PDF watermarking jobs, issues Magic Link, and sends notifications.
func (s *Service) ProcessSuccess(ctx context.Context, yooPaymentID string, orderIDHint int64, receiptURL string, raw json.RawMessage) (*db.PaymentSuccessResult, error) {
	res, err := s.store.MarkPaymentSucceeded(ctx, yooPaymentID, orderIDHint, receiptURL, raw)
	if err != nil {
		return nil, err
	}
	if res.AlreadyPaid {
		return res, nil
	}

	// 1. Enqueue PDF personalized watermarking tasks via Asynq.
	if s.queue != nil {
		for _, cm := range res.ClientMaterials {
			masterKey := res.MasterKeys[cm.MaterialID]
			if err := s.queue.EnqueueWatermark(queue.WatermarkPayload{
				ClientMaterialID: cm.ID,
				UserID:           res.User.ID,
				OrderID:          res.Order.ID,
				OrderNumber:      res.Order.OrderNumber,
				MaterialID:       cm.MaterialID,
				FullName:         res.User.FullName,
				Email:            res.User.Email,
				Phone:            res.User.Phone,
				MasterKey:        masterKey,
				ClientKey:        cm.S3ClientKey,
			}); err != nil {
				log.Printf("enqueue pdf:watermark failed for cm=%d: %v", cm.ID, err)
			}
		}
	}

	// 2. Advance MAX FSM state to STATE_PAID.
	if res.User.MaxUserID != nil && *res.User.MaxUserID != "" {
		maxbot.MarkUserPaid(ctx, s.store, *res.User.MaxUserID, res.Order.ID)
	}

	// 3. Issue single-use Magic Link.
	magicURL, err := s.IssueMagicLink(ctx, res.User)
	if err != nil {
		log.Printf("issue magic link failed for user=%d: %v", res.User.ID, err)
	}

	// 4. Send onboarding email & MAX notification.
	s.SendOnboardingNotifications(ctx, *res, magicURL)
	s.store.WriteAuditLog(ctx, &res.User.ID, "system", "order.paid", "order", strconv.FormatInt(res.Order.ID, 10), map[string]interface{}{
		"order_number": res.Order.OrderNumber,
		"tariff_code":  res.Tariff.Code,
		"materials":    len(res.ClientMaterials),
	})
	return res, nil
}

// IssueMagicLink generates, hashes, persists, and formats a single-use Magic Link URL.
func (s *Service) IssueMagicLink(ctx context.Context, user db.User) (string, error) {
	raw, hash, err := auth.GenerateToken(32)
	if err != nil {
		return "", err
	}
	ttl := time.Duration(s.cfg.MagicLinkTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 60 * time.Minute
	}
	if err := s.store.CreateAuthToken(ctx, user.ID, hash, "magic_link", user.Email, time.Now().UTC().Add(ttl)); err != nil {
		return "", err
	}
	base := strings.TrimRight(s.cfg.MagicLinkBaseURL, "/")
	if base == "" {
		base = strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/cabinet/auth/magic"
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%stoken=%s", base, sep, url.QueryEscape(raw)), nil
}

// SendOnboardingNotifications delivers Welcome Email (with Magic Link + expiry) and MAX Bot message.
func (s *Service) SendOnboardingNotifications(ctx context.Context, res db.PaymentSuccessResult, magicURL string) {
	expiryStr := res.Subscription.ExpiresAt.In(time.FixedZone("MSK", 3*3600)).Format("02.01.2006")
	subject := fmt.Sprintf("Доступ к интенсиву «Беременность без тревоги» — заказ %s", res.Order.OrderNumber)
	emailBody := fmt.Sprintf(
		"Здравствуйте, %s!\n\n"+
			"Оплата заказа %s (%s) прошла успешно. Ваш доступ активен до %s.\n\n"+
			"Одноразовая ссылка для входа в личный кабинет (действительна %d мин):\n%s\n\n"+
			"Персональные материалы (методички и чек-листы) сейчас подготавливаются и появятся в кабинете в течение нескольких секунд.\n"+
			"При необходимости вы также всегда можете войти по одноразовому коду (OTP) на email %s.\n\n"+
			"С заботой о вас,\nКоманда «Больше чем здоровье»",
		res.User.FullName, res.Order.OrderNumber, res.Tariff.Title, expiryStr, s.cfg.MagicLinkTTLMinutes, magicURL, res.User.Email,
	)

	s.DeliverEmail(res.User.Email, subject, emailBody)

	if res.User.MaxUserID != nil && *res.User.MaxUserID != "" && s.bot != nil {
		extra := "В личном кабинете уже готовятся ваши персональные PDF-методички и видеомодули."
		if res.Tariff.HasFeedback {
			extra += "\n\nВ вашем тарифе доступна запись на индивидуальные консультации с врачом, а доступ к закрытому каналу курса в MAX открывается после подтверждения заявки врачом/менеджером."
		}
		var buttons [][]maxbot.Button
		buttons = append(buttons,
			[]maxbot.Button{
				{Type: "callback", Text: "📚 Мои материалы и уроки", Payload: "cab:materials"},
				{Type: "callback", Text: "📅 Запись к врачу", Payload: "cab:slots"},
			},
			[]maxbot.Button{
				{Type: "callback", Text: "🧪 Добавить анализы", Payload: "cab:analyses"},
				{Type: "callback", Text: "📋 Мои заключения", Payload: "cab:docs"},
			},
			[]maxbot.Button{
				{Type: "callback", Text: "👤 Личные данные", Payload: "cab:profile"},
				{Type: "callback", Text: "🥑 Главное меню в боте", Payload: "nav:main_menu"},
			},
		)
		if magicURL != "" {
			buttons = append(buttons, []maxbot.Button{{Type: "link", Text: "🌐 Открыть веб-версию кабинета", URL: magicURL}})
		}
		msg := maxbot.OutgoingMessage{
			Text: fmt.Sprintf(
				"✅ *Оплата заказа %s подтверждена!*\n\nТариф: *%s*\nДоступ активен до: *%s*\n\n%s\n\nЧек НПД отправлен на `%s`.",
				res.Order.OrderNumber, maxbot.EscapeMarkdown(res.Tariff.Title), expiryStr, extra, res.User.Email,
			),
			Attachments: maxbot.InlineKeyboard(buttons...),
		}
		if err := s.bot.SendMessage(ctx, *res.User.MaxUserID, msg); err != nil {
			log.Printf("send MAX onboarding message failed: %v", err)
		}
	}
}

// DeliverEmail sends via Asynq worker for SMTP mode, or directly via mailer in mock mode so local tests see it immediately.
func (s *Service) DeliverEmail(to, subject, body string) {
	if s.cfg.EmailMode == "smtp" && s.queue != nil {
		if err := s.queue.EnqueueEmail(queue.EmailPayload{To: to, Subject: subject, Body: body}); err == nil {
			return
		}
	}
	if s.mailer != nil {
		if err := s.mailer.Send(to, subject, body); err != nil {
			log.Printf("deliver email failed to=%s: %v", to, err)
		}
	}
}

// ReconcilePending checks pending payments via YooKassa API in live mode to guarantee delivery if webhooks were missed.
func (s *Service) ReconcilePending(ctx context.Context) {
	if s.cfg.YooMode != "live" || s.cfg.YooShopID == "" || strings.HasPrefix(s.cfg.YooSecretKey, "change_me") {
		return
	}
	pending, err := s.store.ListPendingPaymentsForReconcile(ctx)
	if err != nil {
		log.Printf("reconcile query failed: %v", err)
		return
	}
	for _, ref := range pending {
		obj, raw, err := s.GetPayment(ctx, ref.YooPaymentID)
		if err != nil {
			continue
		}
		if obj.Status == "succeeded" {
			_, _ = s.ProcessSuccess(ctx, ref.YooPaymentID, ref.OrderID, "", raw)
		} else if obj.Status == "canceled" {
			_ = s.store.MarkOrderCanceled(ctx, ref.YooPaymentID, ref.OrderID)
		}
	}
}
