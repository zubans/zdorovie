package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/maxbot"
)

func (s *Server) handleCabinetGetChat(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if r.URL.Query().Get("peek") != "1" {
		_ = s.store.MarkChatReadByClient(r.Context(), u.ID)
	}
	msgs, err := s.store.ListChatMessages(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить сообщения чата")
		return
	}
	unread, _ := s.store.CountUnreadForClient(r.Context(), u.ID)
	cCfg, _ := s.store.GetConsultationConfig(r.Context())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages":     msgs,
		"unread_count": unread,
		"doctor_name":  cCfg.DoctorName,
	})
}

func (s *Server) handleCabinetChatUnread(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	unread, err := s.store.CountUnreadForClient(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось проверить новые сообщения")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread_count": unread})
}

func (s *Server) handleCabinetSendChat(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &req, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	msg, err := s.store.CreateChatMessage(r.Context(), u.ID, u.ID, "client", req.Body)
	if err != nil {
		if errors.Is(err, db.ErrEmptyChatMessage) {
			writeError(w, http.StatusBadRequest, "empty_message", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось отправить сообщение")
		return
	}
	_ = s.store.MarkChatReadByClient(r.Context(), u.ID)
	s.store.WriteAuditLog(r.Context(), &u.ID, u.Role, "chat.client_send", "chat_message", strconv.FormatInt(msg.ID, 10), nil)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"message": msg})
}

func (s *Server) handleCabinetMarkChatRead(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := s.store.MarkChatReadByClient(r.Context(), u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminChatUnreadSummary(w http.ResponseWriter, r *http.Request) {
	total, clientsCount, err := s.store.CountUnreadForDoctor(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"unread_count":  total,
		"clients_count": clientsCount,
	})
}

func (s *Server) handleAdminGetClientChat(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID пациентки")
		return
	}
	if _, err := s.store.GetUserByID(r.Context(), clientID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Пациентка не найдена")
		return
	}
	if r.URL.Query().Get("mark_read") == "1" {
		_ = s.store.MarkChatReadByDoctor(r.Context(), clientID)
	}
	msgs, err := s.store.ListChatMessages(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить переписку")
		return
	}
	unread := 0
	for _, m := range msgs {
		if m.SenderRole == "client" && !m.ReadByDoctor {
			unread++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages":         msgs,
		"unread_by_doctor": unread,
	})
}

func (s *Server) handleAdminSendClientChat(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID пациентки")
		return
	}
	clientUser, err := s.store.GetUserByID(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Пациентка не найдена")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &req, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	role := actor.Role
	if role != "admin" {
		role = "doctor"
	}
	msg, err := s.store.CreateChatMessage(r.Context(), clientID, actor.ID, role, req.Body)
	if err != nil {
		if errors.Is(err, db.ErrEmptyChatMessage) {
			writeError(w, http.StatusBadRequest, "empty_message", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось отправить сообщение")
		return
	}
	// Answering in chat automatically acknowledges prior unread messages from this client
	_ = s.store.MarkChatReadByDoctor(r.Context(), clientID)
	if clientUser.MaxUserID != nil && *clientUser.MaxUserID != "" && s.bot != nil {
		_ = s.bot.SendMessage(r.Context(), *clientUser.MaxUserID, maxbot.OutgoingMessage{
			Text: fmt.Sprintf("👩‍⚕️ *Новое сообщение от врача (%s):*\n\n%s", maxbot.EscapeMarkdown(actor.FullName), maxbot.EscapeMarkdown(msg.Body)),
			Attachments: maxbot.InlineKeyboard(
				[]maxbot.Button{
					{Type: "callback", Text: "💬 Ответить врачу в боте", Payload: "cab:chat"},
					{Type: "callback", Text: "🥑 Главное меню", Payload: "nav:main_menu"},
				},
			),
		})
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "chat.doctor_reply", "chat_message", strconv.FormatInt(msg.ID, 10), map[string]int64{
		"client_id": clientID,
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"message":          msg,
		"unread_by_doctor": 0,
	})
}

func (s *Server) handleAdminMarkClientChatRead(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID пациентки")
		return
	}
	if err := s.store.MarkChatReadByDoctor(r.Context(), clientID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
