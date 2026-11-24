package db

import (
	"context"
	"errors"
	"strings"
)

// ErrEmptyChatMessage indicates blank or overly long chat message body.
var ErrEmptyChatMessage = errors.New("текст сообщения не может быть пустым или длиннее 4000 символов")

// ListChatMessages returns chronological internal chat history for a patient.
func (s *Store) ListChatMessages(ctx context.Context, clientID int64) ([]ChatMessage, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT cm.id, cm.client_id, cm.sender_user_id, cm.sender_role,
		       COALESCE(su.full_name, CASE WHEN cm.sender_role = 'client' THEN 'Пациентка' ELSE 'Врач' END),
		       cm.body, cm.read_by_client, cm.read_by_doctor, cm.created_at
		FROM chat_messages cm
		LEFT JOIN users su ON su.id = cm.sender_user_id
		WHERE cm.client_id = $1
		ORDER BY cm.created_at ASC, cm.id ASC
		LIMIT 500`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(
			&m.ID, &m.ClientID, &m.SenderUserID, &m.SenderRole,
			&m.SenderName, &m.Body, &m.ReadByClient, &m.ReadByDoctor, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, m)
	}
	return res, rows.Err()
}

// CreateChatMessage appends a message between a patient and doctor/admin.
func (s *Store) CreateChatMessage(ctx context.Context, clientID, senderUserID int64, senderRole, rawBody string) (*ChatMessage, error) {
	body := strings.TrimSpace(rawBody)
	if body == "" || len([]rune(body)) > 4000 {
		return nil, ErrEmptyChatMessage
	}
	if senderRole != "client" && senderRole != "doctor" && senderRole != "admin" {
		senderRole = "client"
	}

	readByClient := senderRole == "client"
	readByDoctor := senderRole != "client"

	var m ChatMessage
	err := s.Pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO chat_messages (client_id, sender_user_id, sender_role, body, read_by_client, read_by_doctor)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, client_id, sender_user_id, sender_role, body, read_by_client, read_by_doctor, created_at
		)
		SELECT i.id, i.client_id, i.sender_user_id, i.sender_role,
		       COALESCE(u.full_name, CASE WHEN i.sender_role = 'client' THEN 'Пациентка' ELSE 'Врач' END),
		       i.body, i.read_by_client, i.read_by_doctor, i.created_at
		FROM inserted i
		LEFT JOIN users u ON u.id = i.sender_user_id`,
		clientID, senderUserID, senderRole, body, readByClient, readByDoctor,
	).Scan(
		&m.ID, &m.ClientID, &m.SenderUserID, &m.SenderRole,
		&m.SenderName, &m.Body, &m.ReadByClient, &m.ReadByDoctor, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkChatReadByClient marks doctor/admin messages as read by the patient.
func (s *Store) MarkChatReadByClient(ctx context.Context, clientID int64) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE chat_messages
		SET read_by_client = TRUE
		WHERE client_id = $1 AND read_by_client = FALSE`, clientID)
	return err
}

// MarkChatReadByDoctor marks patient messages as read by doctor/admin.
func (s *Store) MarkChatReadByDoctor(ctx context.Context, clientID int64) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE chat_messages
		SET read_by_doctor = TRUE
		WHERE client_id = $1 AND sender_role = 'client' AND read_by_doctor = FALSE`, clientID)
	return err
}

// CountUnreadForClient returns unread doctor replies waiting for a specific patient.
func (s *Store) CountUnreadForClient(ctx context.Context, clientID int64) (int, error) {
	var cnt int
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM chat_messages
		WHERE client_id = $1 AND sender_role <> 'client' AND read_by_client = FALSE`, clientID).Scan(&cnt)
	return cnt, err
}

// CountUnreadForDoctor returns total unread client messages and count of patients with unread messages.
func (s *Store) CountUnreadForDoctor(ctx context.Context) (total int, clients int, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT client_id)
		FROM chat_messages
		WHERE sender_role = 'client' AND read_by_doctor = FALSE`).Scan(&total, &clients)
	return total, clients, err
}
