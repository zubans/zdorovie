package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/maxbot"
)

func (s *Server) handleMagicAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil || strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Не передан токен входа")
		return
	}

	u, err := s.store.ConsumeAuthToken(r.Context(), auth.HashToken(strings.TrimSpace(req.Token)), "magic_link")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "Ссылка недействительна, уже использована или устарела. Войдите по коду на Email.")
		return
	}

	s.issueSessionResponse(w, r, u, "auth.magic_login")
}

func (s *Server) handleOTPRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	email, err := maxbot.ValidateEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_email", err.Error())
		return
	}

	u, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		// Prevent email enumeration while preserving clarity if not registered
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"sent":    true,
			"message": "Если аккаунт с таким email существует, мы отправили 6-значный код входа.",
		})
		return
	}

	code, hash, err := auth.GenerateOTPForEmail(u.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "otp_error", "Не удалось сгенерировать код")
		return
	}
	if err := s.store.CreateAuthToken(r.Context(), u.ID, hash, "otp", u.Email, time.Now().UTC().Add(15*time.Minute)); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось сохранить код")
		return
	}

	subject := "Код для входа в кабинет «Больше чем здоровье»"
	body := fmt.Sprintf("Ваш одноразовый код для входа: %s\nКод действителен 15 минут.", code)
	if s.yoo != nil {
		s.yoo.DeliverEmail(u.Email, subject, body)
	} else if s.mailer != nil {
		_ = s.mailer.Send(u.Email, subject, body)
	}

	resp := map[string]interface{}{
		"sent":    true,
		"message": "6-значный одноразовый код отправлен на " + u.Email,
	}
	if s.cfg.AppEnv != "production" && s.cfg.EmailMode == "mock" && s.cfg.AppDebug {
		resp["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)
	if email == "" || len(code) != 6 {
		writeError(w, http.StatusBadRequest, "bad_request", "Укажите email и 6-значный код")
		return
	}

	u, err := s.store.ConsumeAuthToken(r.Context(), auth.HashOTP(email, code), "otp")
	if err != nil || !strings.EqualFold(u.Email, email) {
		writeError(w, http.StatusUnauthorized, "invalid_otp", "Неверный или просроченный код")
		return
	}

	s.issueSessionResponse(w, r, u, "auth.otp_login")
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	u, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil || (u.Role != "admin" && u.Role != "doctor") || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль")
		return
	}

	s.issueSessionResponse(w, r, u, "auth.admin_login")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sub, _ := s.store.GetLatestSubscription(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":         u,
		"subscription": sub,
	})
}

func (s *Server) issueSessionResponse(w http.ResponseWriter, r *http.Request, u *db.User, auditAction string) {
	jwtStr, err := auth.IssueJWT(s.cfg.JWTSecret, u.ID, u.Role, u.Email, 7*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "jwt_error", "Ошибка выдачи сессии")
		return
	}
	auth.SetSessionCookie(w, r, jwtStr, 7*24*3600)
	s.store.WriteAuditLog(r.Context(), &u.ID, u.Role, auditAction, "user", fmt.Sprintf("%d", u.ID), nil)
	sub, _ := s.store.GetLatestSubscription(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": jwtStr,
		"user":         u,
		"subscription": sub,
	})
}
