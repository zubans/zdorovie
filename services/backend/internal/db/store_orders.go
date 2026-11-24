package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateOrder inserts a pending order with a unique ZDOR-XXXX number.
func (s *Store) CreateOrder(ctx context.Context, userID int64, tariff Tariff, consentAccepted bool) (*Order, error) {
	var consentAt *time.Time
	if consentAccepted {
		now := time.Now().UTC()
		consentAt = &now
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var o Order
	// Insert with temporary order_number, then update with ZDOR-(1000+id) atomically
	tempNum := fmt.Sprintf("TMP-%d-%d", userID, time.Now().UnixNano())
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (order_number, user_id, tariff_id, amount_kopecks, status, consent_accepted_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
		RETURNING id, user_id, tariff_id, amount_kopecks, status, consent_accepted_at, channel_access_status, created_at, updated_at`,
		tempNum, userID, tariff.ID, tariff.PriceKopecks, consentAt,
	).Scan(&o.ID, &o.UserID, &o.TariffID, &o.AmountKopecks, &o.Status, &o.ConsentAcceptedAt, &o.ChannelAccessStatus, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}

	o.OrderNumber = fmt.Sprintf("ZDOR-%d", 1000+o.ID)
	_, err = tx.Exec(ctx, `UPDATE orders SET order_number = $2 WHERE id = $1`, o.ID, o.OrderNumber)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	o.TariffTitle = tariff.Title
	o.TariffCode = tariff.Code
	return &o, nil
}

// UpsertPayment stores or updates a YooKassa payment record linked to an order.
func (s *Store) UpsertPayment(ctx context.Context, orderID int64, yooPaymentID, status string, amountKopecks int64, confirmURL, receiptURL string, expiresAt *time.Time, raw []byte) (*Payment, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var p Payment
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO payments (order_id, yukassa_payment_id, status, amount_kopecks, is_npd_registered, receipt_url, confirmation_url, expires_at, raw_payload)
		VALUES ($1, $2, $3, $4, TRUE, $5, $6, $7, $8::jsonb)
		ON CONFLICT (yukassa_payment_id) DO UPDATE SET
			status = EXCLUDED.status,
			receipt_url = CASE WHEN EXCLUDED.receipt_url <> '' THEN EXCLUDED.receipt_url ELSE payments.receipt_url END,
			confirmation_url = CASE WHEN EXCLUDED.confirmation_url <> '' THEN EXCLUDED.confirmation_url ELSE payments.confirmation_url END,
			raw_payload = EXCLUDED.raw_payload
		RETURNING id, order_id, yukassa_payment_id, status, amount_kopecks, is_npd_registered, COALESCE(receipt_url,''), COALESCE(confirmation_url,''), expires_at, raw_payload, paid_at, created_at`,
		orderID, yooPaymentID, status, amountKopecks, receiptURL, confirmURL, expiresAt, string(raw),
	).Scan(&p.ID, &p.OrderID, &p.YooPaymentID, &p.Status, &p.AmountKopecks, &p.IsNPDRegistered, &p.ReceiptURL, &p.ConfirmationURL, &p.ExpiresAt, &p.RawPayload, &p.PaidAt, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PaymentSuccessResult contains entities created/updated during idempotent payment confirmation.
type PaymentSuccessResult struct {
	AlreadyPaid     bool
	Order           Order
	User            User
	Tariff          Tariff
	Subscription    Subscription
	ClientMaterials []ClientMaterial
	MasterKeys      map[int]string
}

// MarkPaymentSucceeded idempotently transitions an order to paid using SELECT ... FOR UPDATE,
// creates/extends subscription, and prepares pending client_materials rows for watermarking.
func (s *Store) MarkPaymentSucceeded(ctx context.Context, yooPaymentID string, orderIDHint int64, receiptURL string, rawPayload json.RawMessage) (*PaymentSuccessResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var orderID int64
	if yooPaymentID != "" {
		_ = tx.QueryRow(ctx, `SELECT order_id FROM payments WHERE yukassa_payment_id = $1`, yooPaymentID).Scan(&orderID)
	}
	if orderID == 0 {
		orderID = orderIDHint
	}
	if orderID == 0 {
		return nil, fmt.Errorf("order not found for payment %s", yooPaymentID)
	}

	// Row lock on order guarantees idempotency across concurrent webhooks and reconciliation.
	var ord Order
	err = tx.QueryRow(ctx, `
		SELECT id, order_number, user_id, tariff_id, amount_kopecks, status, consent_accepted_at, paid_at, channel_access_status, channel_approved_at, channel_joined_at, created_at, updated_at
		FROM orders WHERE id = $1 FOR UPDATE`, orderID,
	).Scan(&ord.ID, &ord.OrderNumber, &ord.UserID, &ord.TariffID, &ord.AmountKopecks, &ord.Status, &ord.ConsentAcceptedAt, &ord.PaidAt, &ord.ChannelAccessStatus, &ord.ChannelApprovedAt, &ord.ChannelJoinedAt, &ord.CreatedAt, &ord.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if ord.Status == "paid" {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &PaymentSuccessResult{AlreadyPaid: true, Order: ord}, nil
	}

	if len(rawPayload) == 0 {
		rawPayload = []byte("{}")
	}

	if yooPaymentID != "" {
		_, err = tx.Exec(ctx, `
			INSERT INTO payments (order_id, yukassa_payment_id, status, amount_kopecks, is_npd_registered, receipt_url, raw_payload, paid_at)
			VALUES ($1, $2, 'succeeded', $3, TRUE, $4, $5::jsonb, NOW())
			ON CONFLICT (yukassa_payment_id) DO UPDATE SET
				status = 'succeeded',
				receipt_url = CASE WHEN EXCLUDED.receipt_url <> '' THEN EXCLUDED.receipt_url ELSE payments.receipt_url END,
				raw_payload = EXCLUDED.raw_payload,
				paid_at = COALESCE(payments.paid_at, NOW())`,
			ord.ID, yooPaymentID, ord.AmountKopecks, receiptURL, string(rawPayload),
		)
		if err != nil {
			return nil, err
		}
	}

	err = tx.QueryRow(ctx, `
		UPDATE orders SET status = 'paid', paid_at = NOW(), updated_at = NOW()
		WHERE id = $1
		RETURNING paid_at, updated_at`, ord.ID,
	).Scan(&ord.PaidAt, &ord.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ord.Status = "paid"

	var user User
	err = tx.QueryRow(ctx, `
		SELECT id, max_user_id, max_username, full_name, phone, email, COALESCE(pregnancy_status,''), role, created_at, updated_at
		FROM users WHERE id = $1`, ord.UserID,
	).Scan(&user.ID, &user.MaxUserID, &user.MaxUsername, &user.FullName, &user.Phone, &user.Email, &user.PregnancyStatus, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}

	var tariff Tariff
	err = tx.QueryRow(ctx, `
		SELECT id, code, title, COALESCE(description,''), price_kopecks, duration_days, has_feedback, is_active, created_at
		FROM tariffs WHERE id = $1`, ord.TariffID,
	).Scan(&tariff.ID, &tariff.Code, &tariff.Title, &tariff.Description, &tariff.PriceKopecks, &tariff.DurationDays, &tariff.HasFeedback, &tariff.IsActive, &tariff.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Deactivate prior active subscriptions and extend from whichever is later: NOW() or previous expires_at
	baseStart := time.Now().UTC()
	var prevExpires time.Time
	err = tx.QueryRow(ctx, `
		SELECT expires_at FROM subscriptions
		WHERE user_id = $1 AND is_active = TRUE AND expires_at > NOW()
		ORDER BY expires_at DESC LIMIT 1`, ord.UserID,
	).Scan(&prevExpires)
	if err == nil && prevExpires.After(baseStart) {
		baseStart = prevExpires
	}
	_, _ = tx.Exec(ctx, `UPDATE subscriptions SET is_active = FALSE WHERE user_id = $1 AND is_active = TRUE`, ord.UserID)

	expiresAt := baseStart.Add(time.Duration(tariff.DurationDays) * 24 * time.Hour)
	var sub Subscription
	err = tx.QueryRow(ctx, `
		INSERT INTO subscriptions (user_id, order_id, tariff_id, starts_at, expires_at, is_active)
		VALUES ($1, $2, $3, NOW(), $4, TRUE)
		RETURNING id, user_id, order_id, tariff_id, starts_at, expires_at, is_active, created_at`,
		ord.UserID, ord.ID, tariff.ID, expiresAt,
	).Scan(&sub.ID, &sub.UserID, &sub.OrderID, &sub.TariffID, &sub.StartsAt, &sub.ExpiresAt, &sub.IsActive, &sub.CreatedAt)
	if err != nil {
		return nil, err
	}
	sub.TariffCode = tariff.Code
	sub.TariffName = tariff.Title
	sub.HasFeedback = tariff.HasFeedback

	// Prepare client_materials rows for all published PDF materials
	mRows, err := tx.Query(ctx, `
		SELECT id, s3_master_key FROM materials WHERE is_published = TRUE AND file_type = 'pdf' ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	type matItem struct {
		id  int
		key string
	}
	var pdfMaterials []matItem
	for mRows.Next() {
		var m matItem
		if err := mRows.Scan(&m.id, &m.key); err != nil {
			mRows.Close()
			return nil, err
		}
		pdfMaterials = append(pdfMaterials, m)
	}
	mRows.Close()
	if err := mRows.Err(); err != nil {
		return nil, err
	}

	masterKeys := make(map[int]string, len(pdfMaterials))
	var cms []ClientMaterial
	sig := fmt.Sprintf("%s (%s, %s) • %s", user.FullName, user.Email, user.Phone, ord.OrderNumber)

	for _, m := range pdfMaterials {
		masterKeys[m.id] = m.key
		clientKey := fmt.Sprintf("clients/%d/materials/%d/document_%d.pdf", user.ID, m.id, ord.ID)
		var cm ClientMaterial
		err = tx.QueryRow(ctx, `
			INSERT INTO client_materials (user_id, order_id, material_id, s3_client_key, watermark_signature, status)
			VALUES ($1, $2, $3, $4, $5, 'pending')
			ON CONFLICT (user_id, material_id) DO UPDATE SET
				order_id = EXCLUDED.order_id,
				s3_client_key = EXCLUDED.s3_client_key,
				watermark_signature = EXCLUDED.watermark_signature,
				status = 'pending',
				error_message = NULL,
				generated_at = NULL
			RETURNING id, user_id, order_id, material_id, s3_client_key, watermark_signature, status, created_at`,
			user.ID, ord.ID, m.id, clientKey, sig,
		).Scan(&cm.ID, &cm.UserID, &cm.OrderID, &cm.MaterialID, &cm.S3ClientKey, &cm.WatermarkSignature, &cm.Status, &cm.CreatedAt)
		if err != nil {
			return nil, err
		}
		cms = append(cms, cm)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &PaymentSuccessResult{
		AlreadyPaid:     false,
		Order:           ord,
		User:            user,
		Tariff:          tariff,
		Subscription:    sub,
		ClientMaterials: cms,
		MasterKeys:      masterKeys,
	}, nil
}

// MarkOrderCanceled marks a pending order and payment as canceled.
func (s *Store) MarkOrderCanceled(ctx context.Context, yooPaymentID string, orderIDHint int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if yooPaymentID != "" {
		_, _ = tx.Exec(ctx, `UPDATE payments SET status = 'canceled' WHERE yukassa_payment_id = $1 AND status <> 'succeeded'`, yooPaymentID)
		if orderIDHint == 0 {
			_ = tx.QueryRow(ctx, `SELECT order_id FROM payments WHERE yukassa_payment_id = $1`, yooPaymentID).Scan(&orderIDHint)
		}
	}
	if orderIDHint > 0 {
		_, _ = tx.Exec(ctx, `UPDATE orders SET status = 'canceled', updated_at = NOW() WHERE id = $1 AND status = 'pending'`, orderIDHint)
	}
	return tx.Commit(ctx)
}

// GetOrderByID loads an order joined with user, tariff, and latest payment details.
func (s *Store) GetOrderByID(ctx context.Context, id int64) (*Order, error) {
	var o Order
	err := s.Pool.QueryRow(ctx, `
		SELECT o.id, o.order_number, o.user_id, o.tariff_id, o.amount_kopecks, o.status,
		       o.consent_accepted_at, o.paid_at, o.channel_access_status, o.channel_approved_at, o.channel_joined_at,
		       o.created_at, o.updated_at,
		       u.full_name, u.email, u.phone, u.max_user_id,
		       t.title, t.code,
		       COALESCE(p.yukassa_payment_id,''), COALESCE(p.status,''), COALESCE(p.receipt_url,''), COALESCE(p.confirmation_url,'')
		FROM orders o
		JOIN users u ON u.id = o.user_id
		JOIN tariffs t ON t.id = o.tariff_id
		LEFT JOIN LATERAL (
			SELECT yukassa_payment_id, status, receipt_url, confirmation_url
			FROM payments WHERE order_id = o.id ORDER BY id DESC LIMIT 1
		) p ON TRUE
		WHERE o.id = $1`, id,
	).Scan(
		&o.ID, &o.OrderNumber, &o.UserID, &o.TariffID, &o.AmountKopecks, &o.Status,
		&o.ConsentAcceptedAt, &o.PaidAt, &o.ChannelAccessStatus, &o.ChannelApprovedAt, &o.ChannelJoinedAt,
		&o.CreatedAt, &o.UpdatedAt,
		&o.UserFullName, &o.UserEmail, &o.UserPhone, &o.UserMaxID,
		&o.TariffTitle, &o.TariffCode,
		&o.YooPaymentID, &o.PaymentStatus, &o.ReceiptURL, &o.ConfirmURL,
	)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ListOrders returns all orders (or filtered by userID/status) for admin & cabinet.
func (s *Store) ListOrders(ctx context.Context, userID int64, statusFilter string) ([]Order, error) {
	q := `
		SELECT o.id, o.order_number, o.user_id, o.tariff_id, o.amount_kopecks, o.status,
		       o.consent_accepted_at, o.paid_at, o.channel_access_status, o.channel_approved_at, o.channel_joined_at,
		       o.created_at, o.updated_at,
		       u.full_name, u.email, u.phone, u.max_user_id,
		       t.title, t.code,
		       COALESCE(p.yukassa_payment_id,''), COALESCE(p.status,''), COALESCE(p.receipt_url,''), COALESCE(p.confirmation_url,'')
		FROM orders o
		JOIN users u ON u.id = o.user_id
		JOIN tariffs t ON t.id = o.tariff_id
		LEFT JOIN LATERAL (
			SELECT yukassa_payment_id, status, receipt_url, confirmation_url
			FROM payments WHERE order_id = o.id ORDER BY id DESC LIMIT 1
		) p ON TRUE
		WHERE 1=1`
	var args []interface{}
	idx := 1
	if userID > 0 {
		q += fmt.Sprintf(" AND o.user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	if statusFilter != "" {
		q += fmt.Sprintf(" AND o.status = $%d", idx)
		args = append(args, statusFilter)
		idx++
	}
	q += " ORDER BY o.created_at DESC LIMIT 300"

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(
			&o.ID, &o.OrderNumber, &o.UserID, &o.TariffID, &o.AmountKopecks, &o.Status,
			&o.ConsentAcceptedAt, &o.PaidAt, &o.ChannelAccessStatus, &o.ChannelApprovedAt, &o.ChannelJoinedAt,
			&o.CreatedAt, &o.UpdatedAt,
			&o.UserFullName, &o.UserEmail, &o.UserPhone, &o.UserMaxID,
			&o.TariffTitle, &o.TariffCode,
			&o.YooPaymentID, &o.PaymentStatus, &o.ReceiptURL, &o.ConfirmURL,
		); err != nil {
			return nil, err
		}
		res = append(res, o)
	}
	return res, rows.Err()
}

// ApproveOrderChannel marks manual administrator approval of the user's MAX private channel join request.
func (s *Store) ApproveOrderChannel(ctx context.Context, orderID int64) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE orders SET channel_access_status = 'approved', channel_approved_at = NOW(), updated_at = NOW()
		WHERE id = $1`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// MarkChannelMembershipByMaxUser updates channel_access_status when MAX sends user_added/user_removed.
func (s *Store) MarkChannelMembershipByMaxUser(ctx context.Context, maxUserID string, active bool) error {
	if active {
		_, err := s.Pool.Exec(ctx, `
			UPDATE orders o
			SET channel_access_status = 'active', channel_joined_at = COALESCE(channel_joined_at, NOW()), updated_at = NOW()
			FROM users u
			WHERE o.user_id = u.id AND u.max_user_id = $1 AND o.status = 'paid'`, maxUserID)
		return err
	}
	_, err := s.Pool.Exec(ctx, `
		UPDATE orders o
		SET channel_access_status = 'removed', updated_at = NOW()
		FROM users u
		WHERE o.user_id = u.id AND u.max_user_id = $1 AND o.status = 'paid'`, maxUserID)
	return err
}

// PendingPaymentForReconcile returns pending payments created within the last 48h.
type PendingPaymentForReconcile struct {
	OrderID      int64
	YooPaymentID string
}

// ListPendingPaymentsForReconcile fetches pending orders with YooKassa IDs for periodic reconciliation.
func (s *Store) ListPendingPaymentsForReconcile(ctx context.Context) ([]PendingPaymentForReconcile, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT o.id, p.yukassa_payment_id
		FROM orders o
		JOIN payments p ON p.order_id = o.id
		WHERE o.status = 'pending' AND p.yukassa_payment_id <> '' AND o.created_at > NOW() - INTERVAL '48 hours'
		ORDER BY o.created_at ASC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []PendingPaymentForReconcile
	for rows.Next() {
		var r PendingPaymentForReconcile
		if err := rows.Scan(&r.OrderID, &r.YooPaymentID); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

// GetActiveSubscription returns the user's latest subscription (even if expired, check ExpiresAt).
func (s *Store) GetLatestSubscription(ctx context.Context, userID int64) (*Subscription, error) {
	var sub Subscription
	err := s.Pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.order_id, s.tariff_id, s.starts_at, s.expires_at, s.is_active, s.created_at,
		       t.code, t.title, t.has_feedback
		FROM subscriptions s
		JOIN tariffs t ON t.id = s.tariff_id
		WHERE s.user_id = $1
		ORDER BY s.is_active DESC, s.expires_at DESC
		LIMIT 1`, userID,
	).Scan(&sub.ID, &sub.UserID, &sub.OrderID, &sub.TariffID, &sub.StartsAt, &sub.ExpiresAt, &sub.IsActive, &sub.CreatedAt,
		&sub.TariffCode, &sub.TariffName, &sub.HasFeedback)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if sub.ExpiresAt.After(time.Now().UTC()) && sub.IsActive {
		sub.DaysLeft = int(math.Ceil(time.Until(sub.ExpiresAt).Hours() / 24))
	} else {
		sub.DaysLeft = 0
	}
	return &sub, nil
}

// UpdateClientSubscription lets an admin adjust subscription expiry or tariff manually.
func (s *Store) UpdateClientSubscription(ctx context.Context, userID int64, tariffID int, expiresAt time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var subID int64
	err = tx.QueryRow(ctx, `SELECT id FROM subscriptions WHERE user_id = $1 ORDER BY is_active DESC, expires_at DESC LIMIT 1`, userID).Scan(&subID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no subscription found for user %d", userID)
		}
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE subscriptions SET tariff_id = $2, expires_at = $3, is_active = ($3 > NOW()) WHERE id = $1`,
		subID, tariffID, expiresAt,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListClients returns CRM client rows with tariff, expiry, consultation counts, watermark progress, and unread internal chat state.
func (s *Store) ListClients(ctx context.Context, tariffCode, subStatus string) ([]ClientListRow, error) {
	q := `
		SELECT u.id, u.max_user_id, u.full_name, u.phone, u.email, COALESCE(u.pregnancy_status,''), u.created_at,
		       COALESCE(t.title,'—'), COALESCE(t.code,''), COALESCE(t.has_feedback, FALSE),
		       sub.expires_at,
		       (sub.id IS NOT NULL AND sub.is_active AND sub.expires_at > NOW()) AS sub_active,
		       (SELECT COUNT(*) FROM consultation_slots cs WHERE cs.client_id = u.id AND cs.status = 'completed') AS completed_slots,
		       (SELECT COUNT(*) FROM consultation_slots cs WHERE cs.client_id = u.id AND cs.status = 'booked') AS booked_slots,
		       (SELECT COUNT(*) FROM client_materials cm WHERE cm.user_id = u.id AND cm.status = 'ready') AS mat_ready,
		       (SELECT COUNT(*) FROM client_materials cm WHERE cm.user_id = u.id) AS mat_total,
		       COALESCE(unread_stats.unread_cnt, 0) AS unread_chat_count,
		       COALESCE(chat_all.total_cnt, 0) AS total_chat_count,
		       COALESCE(last_msg.body_preview, '') AS last_chat_preview,
		       COALESCE(last_msg.sender_role, '') AS last_chat_sender_role,
		       last_msg.created_at AS last_chat_at
		FROM users u
		LEFT JOIN LATERAL (
			SELECT id, tariff_id, expires_at, is_active
			FROM subscriptions WHERE user_id = u.id ORDER BY is_active DESC, expires_at DESC LIMIT 1
		) sub ON TRUE
		LEFT JOIN tariffs t ON t.id = sub.tariff_id
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS unread_cnt, MAX(created_at) AS last_unread_at
			FROM chat_messages
			WHERE client_id = u.id AND sender_role = 'client' AND read_by_doctor = FALSE
		) unread_stats ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS total_cnt
			FROM chat_messages
			WHERE client_id = u.id
		) chat_all ON TRUE
		LEFT JOIN LATERAL (
			SELECT LEFT(body, 110) AS body_preview, sender_role, created_at
			FROM chat_messages
			WHERE client_id = u.id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) last_msg ON TRUE
		WHERE u.role = 'client'`
	var args []interface{}
	idx := 1
	if tariffCode != "" {
		q += fmt.Sprintf(" AND t.code = $%d", idx)
		args = append(args, tariffCode)
		idx++
	}
	switch subStatus {
	case "active":
		q += " AND sub.id IS NOT NULL AND sub.is_active AND sub.expires_at > NOW()"
	case "expired":
		q += " AND (sub.id IS NULL OR NOT sub.is_active OR sub.expires_at <= NOW())"
	}
	q += " ORDER BY (COALESCE(unread_stats.unread_cnt, 0) > 0) DESC, unread_stats.last_unread_at DESC NULLS LAST, last_msg.created_at DESC NULLS LAST, u.created_at DESC LIMIT 300"

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []ClientListRow{}
	for rows.Next() {
		var r ClientListRow
		if err := rows.Scan(
			&r.ID, &r.MaxUserID, &r.FullName, &r.Phone, &r.Email, &r.PregnancyStatus, &r.CreatedAt,
			&r.TariffTitle, &r.TariffCode, &r.HasFeedback,
			&r.ExpiresAt, &r.SubActive,
			&r.CompletedSlots, &r.BookedSlots, &r.MaterialsReady, &r.MaterialsTotal,
			&r.UnreadChatCount, &r.TotalChatCount, &r.LastChatPreview, &r.LastChatSenderRole, &r.LastChatAt,
		); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

// GetAnalytics returns CRM analytics metrics.
func (s *Store) GetAnalytics(ctx context.Context) (AnalyticsSummary, error) {
	var a AnalyticsSummary
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount_kopecks),0), COUNT(*) FROM orders WHERE status = 'paid'`).
		Scan(&a.TotalRevenueKopecks, &a.PaidOrdersCount)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE status = 'pending'`).Scan(&a.PendingOrdersCount)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM subscriptions WHERE is_active = TRUE AND expires_at > NOW()`).
		Scan(&a.ActiveClientsCount)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consultation_summaries WHERE status = 'draft'`).Scan(&a.PendingReviewsCount)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_fsm_sessions`).Scan(&a.BotSessionsCount)

	if a.BotSessionsCount > 0 {
		a.ConversionPercent = math.Round((float64(a.PaidOrdersCount)/float64(a.BotSessionsCount))*1000) / 10
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT t.code, t.title, COUNT(o.id), COALESCE(SUM(o.amount_kopecks), 0)
		FROM tariffs t
		LEFT JOIN orders o ON o.tariff_id = t.id AND o.status = 'paid'
		GROUP BY t.id, t.code, t.title
		ORDER BY t.has_feedback ASC, t.duration_days ASC`)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var row TariffStatRow
		if err := rows.Scan(&row.Code, &row.Title, &row.OrdersCount, &row.RevenueKopecks); err != nil {
			return a, err
		}
		a.ByTariff = append(a.ByTariff, row)
	}
	return a, rows.Err()
}

// SubscriptionReminderTarget represents an expiring subscription due for a reminder.
type SubscriptionReminderTarget struct {
	SubscriptionID int64
	UserID         int64
	FullName       string
	Email          string
	MaxUserID      string
	ExpiresAt      time.Time
	DaysWindow     int // 14, 7, or 1
}

// FindDueSubscriptionReminders locates subscriptions crossing 14d, 7d, or 1d thresholds.
func (s *Store) FindDueSubscriptionReminders(ctx context.Context) ([]SubscriptionReminderTarget, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT s.id, s.user_id, u.full_name, u.email, COALESCE(u.max_user_id,''), s.expires_at,
		       CASE
		           WHEN s.expires_at <= NOW() + INTERVAL '1 day' AND s.reminder_1d_sent_at IS NULL THEN 1
		           WHEN s.expires_at <= NOW() + INTERVAL '7 days' AND s.expires_at > NOW() + INTERVAL '1 day' AND s.reminder_7d_sent_at IS NULL THEN 7
		           WHEN s.expires_at <= NOW() + INTERVAL '14 days' AND s.expires_at > NOW() + INTERVAL '7 days' AND s.reminder_14d_sent_at IS NULL THEN 14
		           ELSE 0
		       END AS win
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE s.is_active = TRUE AND s.expires_at > NOW() AND s.expires_at <= NOW() + INTERVAL '14 days'
		  AND (
		      (s.expires_at <= NOW() + INTERVAL '1 day' AND s.reminder_1d_sent_at IS NULL) OR
		      (s.expires_at <= NOW() + INTERVAL '7 days' AND s.expires_at > NOW() + INTERVAL '1 day' AND s.reminder_7d_sent_at IS NULL) OR
		      (s.expires_at <= NOW() + INTERVAL '14 days' AND s.expires_at > NOW() + INTERVAL '7 days' AND s.reminder_14d_sent_at IS NULL)
		  )
		LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []SubscriptionReminderTarget
	for rows.Next() {
		var r SubscriptionReminderTarget
		if err := rows.Scan(&r.SubscriptionID, &r.UserID, &r.FullName, &r.Email, &r.MaxUserID, &r.ExpiresAt, &r.DaysWindow); err != nil {
			return nil, err
		}
		if r.DaysWindow > 0 {
			res = append(res, r)
		}
	}
	return res, rows.Err()
}

// MarkSubscriptionReminderSent records that a 14d/7d/1d expiry reminder has been sent.
func (s *Store) MarkSubscriptionReminderSent(ctx context.Context, subID int64, daysWindow int) error {
	var col string
	switch daysWindow {
	case 14:
		col = "reminder_14d_sent_at"
	case 7:
		col = "reminder_7d_sent_at"
	case 1:
		col = "reminder_1d_sent_at"
	default:
		return nil
	}
	_, err := s.Pool.Exec(ctx, fmt.Sprintf(`UPDATE subscriptions SET %s = NOW() WHERE id = $1`, col), subID)
	return err
}
