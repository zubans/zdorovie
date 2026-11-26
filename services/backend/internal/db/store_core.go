package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// BootstrapAdmin ensures the default admin/doctor user exists with configured password & API token hashes.
func (s *Store) BootstrapAdmin(ctx context.Context, email, passwordHash, apiTokenHash string) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return 0, fmt.Errorf("admin email is empty")
	}
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO users (full_name, phone, email, pregnancy_status, role, password_hash, api_token_hash)
		VALUES ($1, $2, $3, $4, 'admin', $5, NULLIF($6, ''))
		ON CONFLICT (email) DO UPDATE SET
			role = CASE WHEN users.role = 'client' THEN 'admin' ELSE users.role END,
			password_hash = CASE WHEN COALESCE(users.password_hash, '') = '' THEN EXCLUDED.password_hash ELSE users.password_hash END,
			api_token_hash = CASE WHEN COALESCE(users.api_token_hash, '') = '' THEN EXCLUDED.api_token_hash ELSE users.api_token_hash END,
			updated_at = NOW()
		RETURNING id`,
		"Елена Соколова (Врач)", "+79000000000", email, "doctor", passwordHash, apiTokenHash,
	).Scan(&id)
	return id, err
}

// ListTariffs returns tariffs; when activeOnly is true, returns only is_active=true.
func (s *Store) ListTariffs(ctx context.Context, activeOnly bool) ([]Tariff, error) {
	q := `SELECT id, code, title, COALESCE(description,''), price_kopecks, duration_days, has_feedback, is_active, created_at FROM tariffs`
	if activeOnly {
		q += ` WHERE is_active = TRUE`
	}
	q += ` ORDER BY has_feedback ASC, duration_days ASC, id ASC`

	rows, err := s.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Tariff
	for rows.Next() {
		var t Tariff
		if err := rows.Scan(&t.ID, &t.Code, &t.Title, &t.Description, &t.PriceKopecks, &t.DurationDays, &t.HasFeedback, &t.IsActive, &t.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, rows.Err()
}

// GetTariffByCode loads one tariff by code.
func (s *Store) GetTariffByCode(ctx context.Context, code string) (*Tariff, error) {
	var t Tariff
	err := s.Pool.QueryRow(ctx, `
		SELECT id, code, title, COALESCE(description,''), price_kopecks, duration_days, has_feedback, is_active, created_at
		FROM tariffs WHERE code = $1`, code,
	).Scan(&t.ID, &t.Code, &t.Title, &t.Description, &t.PriceKopecks, &t.DurationDays, &t.HasFeedback, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTariffByID loads one tariff by ID.
func (s *Store) GetTariffByID(ctx context.Context, id int) (*Tariff, error) {
	var t Tariff
	err := s.Pool.QueryRow(ctx, `
		SELECT id, code, title, COALESCE(description,''), price_kopecks, duration_days, has_feedback, is_active, created_at
		FROM tariffs WHERE id = $1`, id,
	).Scan(&t.ID, &t.Code, &t.Title, &t.Description, &t.PriceKopecks, &t.DurationDays, &t.HasFeedback, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTariff updates tariff pricing, duration, feedback flag, or active status.
func (s *Store) UpdateTariff(ctx context.Context, id int, title, description string, priceKopecks int64, durationDays int, hasFeedback, isActive bool) (*Tariff, error) {
	var t Tariff
	err := s.Pool.QueryRow(ctx, `
		UPDATE tariffs
		SET title = $2, description = $3, price_kopecks = $4, duration_days = $5, has_feedback = $6, is_active = $7
		WHERE id = $1
		RETURNING id, code, title, COALESCE(description,''), price_kopecks, duration_days, has_feedback, is_active, created_at`,
		id, title, description, priceKopecks, durationDays, hasFeedback, isActive,
	).Scan(&t.ID, &t.Code, &t.Title, &t.Description, &t.PriceKopecks, &t.DurationDays, &t.HasFeedback, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpsertUserByMaxOrEmail creates or updates a client profile coming from MAX bot or checkout.
func (s *Store) UpsertUserByMaxOrEmail(ctx context.Context, maxUserID, maxUsername, fullName, phone, email, pregStatus string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var maxIDPtr, maxUserPtr *string
	if strings.TrimSpace(maxUserID) != "" {
		v := strings.TrimSpace(maxUserID)
		maxIDPtr = &v
	}
	if strings.TrimSpace(maxUsername) != "" {
		v := strings.TrimSpace(maxUsername)
		maxUserPtr = &v
	}

	var u User
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO users (max_user_id, max_username, full_name, phone, email, pregnancy_status, role)
		VALUES ($1, $2, $3, $4, $5, $6, 'client')
		ON CONFLICT (email) DO UPDATE SET
			max_user_id = CASE
				WHEN users.role = 'client' AND users.max_user_id IS NOT NULL AND users.max_user_id = EXCLUDED.max_user_id THEN users.max_user_id
				ELSE users.max_user_id
			END,
			max_username = CASE
				WHEN users.role = 'client' AND users.max_user_id IS NOT NULL AND users.max_user_id = EXCLUDED.max_user_id THEN COALESCE(EXCLUDED.max_username, users.max_username)
				ELSE users.max_username
			END,
			full_name = CASE
				WHEN users.role = 'client' AND (EXCLUDED.max_user_id IS NULL OR users.max_user_id = EXCLUDED.max_user_id) THEN EXCLUDED.full_name
				ELSE users.full_name
			END,
			phone = CASE
				WHEN users.role = 'client' AND (EXCLUDED.max_user_id IS NULL OR users.max_user_id = EXCLUDED.max_user_id) THEN EXCLUDED.phone
				ELSE users.phone
			END,
			pregnancy_status = CASE
				WHEN users.role = 'client' AND (EXCLUDED.max_user_id IS NULL OR users.max_user_id = EXCLUDED.max_user_id) THEN EXCLUDED.pregnancy_status
				ELSE users.pregnancy_status
			END,
			updated_at = NOW()
		RETURNING id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at`,
		maxIDPtr, maxUserPtr, fullName, phone, email, pregStatus,
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByID fetches a user by primary key.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at
		FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByEmail fetches a user by lowercased email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at
		FROM users WHERE LOWER(email) = LOWER($1)`, strings.TrimSpace(email),
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByMaxUserID fetches a user by their MAX messenger user_id.
func (s *Store) GetUserByMaxUserID(ctx context.Context, maxUserID string) (*User, error) {
	maxUserID = strings.TrimSpace(maxUserID)
	if maxUserID == "" {
		return nil, pgx.ErrNoRows
	}
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at
		FROM users WHERE max_user_id = $1`, maxUserID,
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// LinkMaxUserToAccount attaches a MAX user_id and username to an existing user account (clearing any stale binding on another row).
func (s *Store) LinkMaxUserToAccount(ctx context.Context, userID int64, maxUserID, maxUsername string) (*User, error) {
	maxUserID = strings.TrimSpace(maxUserID)
	maxUsername = strings.TrimSpace(maxUsername)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if maxUserID != "" {
		_, _ = tx.Exec(ctx, `UPDATE users SET max_user_id = NULL, updated_at = NOW() WHERE max_user_id = $1 AND id <> $2`, maxUserID, userID)
	}
	var maxIDPtr, maxUserPtr *string
	if maxUserID != "" {
		maxIDPtr = &maxUserID
	}
	if maxUsername != "" {
		maxUserPtr = &maxUsername
	}
	var u User
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET max_user_id = COALESCE($2, max_user_id),
		    max_username = COALESCE($3, max_username),
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at`,
		userID, maxIDPtr, maxUserPtr,
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateUserPersonalData updates non-empty personal profile fields (full_name, phone, email, pregnancy_status).
func (s *Store) UpdateUserPersonalData(ctx context.Context, userID int64, fullName, phone, email, pregStatus string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		UPDATE users
		SET full_name = CASE WHEN $2 <> '' THEN $2 ELSE full_name END,
		    phone = CASE WHEN $3 <> '' THEN $3 ELSE phone END,
		    email = CASE WHEN $4 <> '' THEN LOWER($4) ELSE email END,
		    pregnancy_status = CASE WHEN $5 <> '' THEN $5 ELSE pregnancy_status END,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at`,
		userID, strings.TrimSpace(fullName), strings.TrimSpace(phone), strings.TrimSpace(email), strings.TrimSpace(pregStatus),
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByAPITokenHash looks up a doctor/admin by SHA-256 token hash.
func (s *Store) GetUserByAPITokenHash(ctx context.Context, tokenHash string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at
		FROM users WHERE api_token_hash = $1 AND role IN ('doctor', 'admin')`, tokenHash,
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetDefaultDoctorID returns the ID of the first doctor or admin user.
func (s *Store) GetDefaultDoctorID(ctx context.Context) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		SELECT id FROM users WHERE role IN ('doctor', 'admin') ORDER BY CASE WHEN role = 'doctor' THEN 0 ELSE 1 END, id ASC LIMIT 1`,
	).Scan(&id)
	return id, err
}

// CreateAuthToken stores a hashed single-use Magic Link or OTP token.
func (s *Store) CreateAuthToken(ctx context.Context, userID int64, tokenHash, tokenType, targetEmail string, expiresAt time.Time) error {
	if tokenType == "otp" {
		_, _ = s.Pool.Exec(ctx, `UPDATE auth_tokens SET used_at = NOW() WHERE user_id = $1 AND token_type = 'otp' AND used_at IS NULL`, userID)
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO auth_tokens (user_id, token_hash, token_type, target_email, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (token_hash) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			token_type = EXCLUDED.token_type,
			target_email = EXCLUDED.target_email,
			expires_at = EXCLUDED.expires_at,
			used_at = NULL`,
		userID, tokenHash, tokenType, strings.ToLower(strings.TrimSpace(targetEmail)), expiresAt,
	)
	return err
}

// ConsumeAuthToken validates and marks a single-use token as used atomically.
func (s *Store) ConsumeAuthToken(ctx context.Context, tokenHash, tokenType string) (*User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var userID int64
	err = tx.QueryRow(ctx, `
		UPDATE auth_tokens
		SET used_at = NOW()
		WHERE token_hash = $1
		  AND token_type = $2
		  AND used_at IS NULL
		  AND expires_at > NOW()
		RETURNING user_id`,
		tokenHash, tokenType,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired token")
		}
		return nil, err
	}

	var u User
	err = tx.QueryRow(ctx, `
		SELECT id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, COALESCE(password_hash,''), created_at, updated_at
		FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.MaxUserID, &u.MaxUsername, &u.FullName, &u.Phone, &u.Email, &u.PregnancyStatus, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &u, nil
}

// RecordWebhookEvent returns true if the event is new and recorded, false if already processed.
func (s *Store) RecordWebhookEvent(ctx context.Context, provider, eventKey, eventType string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO webhook_events (provider, event_key, event_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, event_key) DO NOTHING`,
		provider, eventKey, eventType,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// GetConsultationConfig fetches platform consultation settings.
func (s *Store) GetConsultationConfig(ctx context.Context) (ConsultationConfig, error) {
	cfg := ConsultationConfig{Mode: "builtin_slots"}
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key = 'consultation_config'`).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Mode == "" {
		cfg.Mode = "builtin_slots"
	}
	return cfg, nil
}

// SaveConsultationConfig updates platform consultation settings.
func (s *Store) SaveConsultationConfig(ctx context.Context, cfg ConsultationConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO platform_settings (key, value, updated_at)
		VALUES ('consultation_config', $1::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		string(raw),
	)
	return err
}

// CreateQuestion stores a user question submitted via MAX bot.
func (s *Store) CreateQuestion(ctx context.Context, maxUserID, authorName, text string) (*Question, error) {
	var maxPtr *string
	if strings.TrimSpace(maxUserID) != "" {
		v := strings.TrimSpace(maxUserID)
		maxPtr = &v
	}
	var q Question
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO questions (max_user_id, user_id, author_name, question_text, status)
		VALUES (CAST($1 AS VARCHAR), (SELECT id FROM users WHERE max_user_id = CAST($1 AS VARCHAR) LIMIT 1), $2, $3, 'new')
		RETURNING id, max_user_id, user_id, COALESCE(author_name,''), question_text, COALESCE(answer_text,''), status, created_at, answered_at`,
		maxPtr, authorName, text,
	).Scan(&q.ID, &q.MaxUserID, &q.UserID, &q.AuthorName, &q.QuestionText, &q.AnswerText, &q.Status, &q.CreatedAt, &q.AnsweredAt)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// ListQuestions returns MAX bot user inquiries enriched with patient CRM card details.
func (s *Store) ListQuestions(ctx context.Context) ([]Question, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT q.id, q.max_user_id, q.user_id, COALESCE(q.author_name,''), q.question_text, COALESCE(q.answer_text,''), q.status, q.created_at, q.answered_at,
		       u.id, COALESCE(u.full_name,''), COALESCE(u.pregnancy_status,''),
		       COALESCE(t.title,''), (sub.id IS NOT NULL AND sub.is_active AND sub.expires_at > NOW())
		FROM questions q
		LEFT JOIN users u ON (u.id = q.user_id OR (q.user_id IS NULL AND q.max_user_id IS NOT NULL AND u.max_user_id = q.max_user_id))
		LEFT JOIN LATERAL (
			SELECT id, tariff_id, is_active, expires_at
			FROM subscriptions WHERE user_id = u.id ORDER BY is_active DESC, expires_at DESC LIMIT 1
		) sub ON TRUE
		LEFT JOIN tariffs t ON t.id = sub.tariff_id
		ORDER BY q.created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []Question{}
	for rows.Next() {
		var q Question
		if err := rows.Scan(
			&q.ID, &q.MaxUserID, &q.UserID, &q.AuthorName, &q.QuestionText, &q.AnswerText, &q.Status, &q.CreatedAt, &q.AnsweredAt,
			&q.MatchedUserID, &q.ClientName, &q.ClientPregStatus, &q.TariffTitle, &q.SubActive,
		); err != nil {
			return nil, err
		}
		res = append(res, q)
	}
	return res, rows.Err()
}

// GetQuestionByID returns a single MAX bot inquiry enriched with matched patient info.
func (s *Store) GetQuestionByID(ctx context.Context, id int64) (*Question, error) {
	var q Question
	err := s.Pool.QueryRow(ctx, `
		SELECT q.id, q.max_user_id, q.user_id, COALESCE(q.author_name,''), q.question_text, COALESCE(q.answer_text,''), q.status, q.created_at, q.answered_at,
		       u.id, COALESCE(u.full_name,''), COALESCE(u.pregnancy_status,''),
		       COALESCE(t.title,''), (sub.id IS NOT NULL AND sub.is_active AND sub.expires_at > NOW())
		FROM questions q
		LEFT JOIN users u ON (u.id = q.user_id OR (q.user_id IS NULL AND q.max_user_id IS NOT NULL AND u.max_user_id = q.max_user_id))
		LEFT JOIN LATERAL (
			SELECT id, tariff_id, is_active, expires_at
			FROM subscriptions WHERE user_id = u.id ORDER BY is_active DESC, expires_at DESC LIMIT 1
		) sub ON TRUE
		LEFT JOIN tariffs t ON t.id = sub.tariff_id
		WHERE q.id = $1`, id,
	).Scan(
		&q.ID, &q.MaxUserID, &q.UserID, &q.AuthorName, &q.QuestionText, &q.AnswerText, &q.Status, &q.CreatedAt, &q.AnsweredAt,
		&q.MatchedUserID, &q.ClientName, &q.ClientPregStatus, &q.TariffTitle, &q.SubActive,
	)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// AnswerQuestion updates a question's answer and marks it answered.
func (s *Store) AnswerQuestion(ctx context.Context, id int64, answer string) (*Question, error) {
	var q Question
	err := s.Pool.QueryRow(ctx, `
		UPDATE questions
		SET answer_text = $2, status = 'answered', answered_at = NOW()
		WHERE id = $1
		RETURNING id, max_user_id, user_id, COALESCE(author_name,''), question_text, COALESCE(answer_text,''), status, created_at, answered_at`,
		id, answer,
	).Scan(&q.ID, &q.MaxUserID, &q.UserID, &q.AuthorName, &q.QuestionText, &q.AnswerText, &q.Status, &q.CreatedAt, &q.AnsweredAt)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// WriteAuditLog records an administrative or security event.
func (s *Store) WriteAuditLog(ctx context.Context, actorID *int64, actorRole, action, entityType, entityID string, details interface{}) {
	raw, _ := json.Marshal(details)
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, actor_role, action, entity_type, entity_id, details)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		actorID, actorRole, action, entityType, entityID, string(raw),
	)
}
