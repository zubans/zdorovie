package httpapi

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"zdorovie/backend/internal/maxbot"
)

func (s *Server) handleListTariffs(w http.ResponseWriter, r *http.Request) {
	tariffs, err := s.store.ListTariffs(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить тарифы")
		return
	}
	cfg, _ := s.store.GetConsultationConfig(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tariffs":              tariffs,
		"consultation_config":  cfg,
	})
}

func (s *Server) handlePublicConsultationSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetConsultationConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить настройки")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePublicCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TariffCode      string `json:"tariff_code"`
		FullName        string `json:"full_name"`
		Phone           string `json:"phone"`
		Email           string `json:"email"`
		PregnancyStatus string `json:"pregnancy_status"`
		ConsentAccepted bool   `json:"consent_accepted"`
	}
	if err := decodeJSON(r, &req, 128<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	if !req.ConsentAccepted {
		writeError(w, http.StatusBadRequest, "consent_required", "Необходимо согласие с политикой обработки персональных данных и офертой")
		return
	}
	name, err := maxbot.ValidateFullName(req.FullName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error())
		return
	}
	phone, err := maxbot.NormalizePhoneE164(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_phone", err.Error())
		return
	}
	email, err := maxbot.ValidateEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_email", err.Error())
		return
	}
	preg := strings.TrimSpace(req.PregnancyStatus)
	if preg == "" {
		preg = "Планирую беременность"
	}

	tariff, err := s.store.GetTariffByCode(r.Context(), req.TariffCode)
	if err != nil || !tariff.IsActive {
		writeError(w, http.StatusBadRequest, "invalid_tariff", "Выбранный тариф не найден или неактивен")
		return
	}

	user, err := s.store.UpsertUserByMaxOrEmail(r.Context(), "", "", name, phone, email, preg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Ошибка создания профиля")
		return
	}
	order, err := s.store.CreateOrder(r.Context(), user.ID, *tariff, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Ошибка создания заказа")
		return
	}
	payURL, err := s.yoo.CreatePaymentForOrder(r.Context(), *order, *user, *tariff)
	if err != nil {
		writeError(w, http.StatusBadGateway, "payment_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"order":       order,
		"payment_url": payURL,
	})
}

func (s *Server) handleMockPayPage(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AppEnv == "production" && !s.cfg.AppDebug {
		http.NotFound(w, r)
		return
	}
	paymentID := r.URL.Query().Get("payment_id")
	orderIDStr := r.URL.Query().Get("order_id")
	orderID, _ := strconv.ParseInt(orderIDStr, 10, 64)
	ord, err := s.store.GetOrderByID(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Заказ не найден")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html lang="ru"><head><meta charset="utf-8"><title>Симулятор оплаты ЮKassa (Mock)</title>
<style>body{font-family:system-ui,sans-serif;background:#faf8f5;color:#1e293b;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.card{background:#fff;border-radius:16px;padding:32px;max-width:460px;box-shadow:0 10px 30px rgba(0,0,0,.08)}
.btn{display:inline-block;padding:12px 20px;border-radius:10px;border:none;cursor:pointer;font-size:15px;font-weight:600;margin-right:8px}
.ok{background:#2d6a4f;color:#fff}.cancel{background:#e2e8f0;color:#334155}</style></head>
<body><div class="card">
<h2>Тестовая оплата ЮKassa (НПД)</h2>
<p>Заказ: <b>%s</b></p>
<p>Тариф: <b>%s</b></p>
<p>Сумма: <b>%d ₽</b> (Без НДС, фискализация НПД)</p>
<p>Платёж: <code>%s</code></p>
<form method="POST" action="/api/v1/dev/mock-pay/confirm">
<input type="hidden" name="payment_id" value="%s"/>
<input type="hidden" name="order_id" value="%d"/>
<input type="hidden" name="action" value="succeeded"/>
<button type="submit" class="btn ok">✅ Подтвердить оплату</button>
</form><br/>
<form method="POST" action="/api/v1/dev/mock-pay/confirm">
<input type="hidden" name="payment_id" value="%s"/>
<input type="hidden" name="order_id" value="%d"/>
<input type="hidden" name="action" value="canceled"/>
<button type="submit" class="btn cancel">Отменить платёж</button>
</form>
</div></body></html>`,
		html.EscapeString(ord.OrderNumber),
		html.EscapeString(ord.TariffTitle),
		ord.AmountKopecks/100,
		html.EscapeString(paymentID),
		html.EscapeString(paymentID), ord.ID,
		html.EscapeString(paymentID), ord.ID,
	)
}

func (s *Server) handleMockPayConfirm(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AppEnv == "production" && !s.cfg.AppDebug {
		http.NotFound(w, r)
		return
	}
	var paymentID, action string
	var orderID int64

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			PaymentID string `json:"payment_id"`
			OrderID   int64  `json:"order_id"`
			Action    string `json:"action"`
		}
		if err := decodeJSON(r, &body, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
			return
		}
		paymentID = body.PaymentID
		orderID = body.OrderID
		action = body.Action
	} else {
		_ = r.ParseForm()
		paymentID = r.FormValue("payment_id")
		orderID, _ = strconv.ParseInt(r.FormValue("order_id"), 10, 64)
		action = r.FormValue("action")
	}
	if action == "" {
		action = "succeeded"
	}

	if action == "canceled" {
		if err := s.store.MarkOrderCanceled(r.Context(), paymentID, orderID); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if wantsJSON(r) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
			return
		}
		http.Redirect(w, r, "/cabinet/login?canceled=1", http.StatusSeeOther)
		return
	}

	res, err := s.yoo.ProcessSuccess(r.Context(), paymentID, orderID, fmt.Sprintf("https://lknpd.nalog.ru/mock-receipt/%d", orderID), []byte(`{"mock":true,"status":"succeeded"}`))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "payment_confirm_error", err.Error())
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":       "succeeded",
			"already_paid": res.AlreadyPaid,
			"order":        res.Order,
		})
		return
	}
	http.Redirect(w, r, "/cabinet/login?paid="+res.Order.OrderNumber, http.StatusSeeOther)
}

func (s *Server) handleDevOutbox(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AppEnv == "production" || !s.cfg.AppDebug {
		http.NotFound(w, r)
		return
	}
	userID := r.URL.Query().Get("max_user_id")
	var maxMsgs interface{}
	if s.bot != nil {
		maxMsgs = s.bot.RecentSent(userID)
	}
	var emails interface{}
	if s.mailer != nil {
		emails = s.mailer.Recent()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"max_messages": maxMsgs,
		"emails":       emails,
	})
}
