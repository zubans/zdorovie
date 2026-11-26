package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"zdorovie/backend/internal/maxbot"
)

func (s *Server) handleMaxWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.cfg.MaxWebhookSecret
	if secret != "" {
		provided := r.Header.Get("X-Max-Bot-Api-Secret")
		if len(provided) != len(secret) || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			log.Printf("webhook max: unauthorized request (secret mismatch or missing)")
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid webhook secret")
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot read webhook payload")
		return
	}

	var ev maxbot.WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON payload")
		return
	}
	if ev.UpdateType == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing update_type")
		return
	}

	// Never log raw webhook body because it may contain user contact PII (152-FZ compliance).
	log.Printf("webhook max: update_type=%s", ev.UpdateType)

	if s.fsm != nil {
		if err := s.fsm.HandleEvent(r.Context(), ev); err != nil {
			log.Printf("webhook max fsm error (update_type=%s): %v", ev.UpdateType, err)
			writeError(w, http.StatusInternalServerError, "fsm_error", "failed to process update")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "processed": true})
}

func (s *Server) handleYooKassaWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.YooMode != "mock" && (s.cfg.YooShopID == "" || s.cfg.YooSecretKey == "") {
		writeError(w, http.StatusServiceUnavailable, "unconfigured", "YooKassa credentials not configured")
		return
	}
	if s.yoo != nil && !s.yoo.IsAllowedWebhookIP(r.RemoteAddr, r.Header) {
		log.Printf("webhook yukassa: forbidden source IP")
		writeError(w, http.StatusForbidden, "forbidden", "source IP not allowed")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot read webhook payload")
		return
	}

	var meta struct {
		Type   string `json:"type"`
		Event  string `json:"event"`
		Object struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON payload")
		return
	}
	log.Printf("webhook yukassa: event=%s payment_id=%s status=%s", meta.Event, meta.Object.ID, meta.Object.Status)

	if s.yoo != nil && s.store != nil && meta.Object.ID != "" {
		if _, err := s.yoo.HandleWebhook(r.Context(), body); err != nil {
			log.Printf("webhook yukassa processing error (payment_id=%s): %v", meta.Object.ID, err)
			writeError(w, http.StatusUnprocessableEntity, "webhook_error", err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "received": true})
}
