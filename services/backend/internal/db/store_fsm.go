package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetFSMSession loads a MAX user's FSM session or returns nil if none exists yet.
func (s *Store) GetFSMSession(ctx context.Context, maxUserID string) (*BotFSMSession, error) {
	var sess BotFSMSession
	var rawCtx []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT user_id, current_state, draft_order_id, COALESCE(context_data, '{}'::jsonb), COALESCE(last_callback_id,''), updated_at
		FROM bot_fsm_sessions WHERE user_id = $1`, maxUserID,
	).Scan(&sess.UserID, &sess.CurrentState, &sess.DraftOrderID, &rawCtx, &sess.LastCallbackID, &sess.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	sess.ContextData = make(map[string]interface{})
	if len(rawCtx) > 0 {
		_ = json.Unmarshal(rawCtx, &sess.ContextData)
	}
	return &sess, nil
}

// SaveFSMSession upserts a MAX user's FSM session state.
func (s *Store) SaveFSMSession(ctx context.Context, sess BotFSMSession) error {
	if sess.ContextData == nil {
		sess.ContextData = map[string]interface{}{}
	}
	raw, err := json.Marshal(sess.ContextData)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO bot_fsm_sessions (user_id, current_state, draft_order_id, context_data, last_callback_id, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			current_state = EXCLUDED.current_state,
			draft_order_id = EXCLUDED.draft_order_id,
			context_data = EXCLUDED.context_data,
			last_callback_id = EXCLUDED.last_callback_id,
			updated_at = NOW()`,
		sess.UserID, sess.CurrentState, sess.DraftOrderID, string(raw), sess.LastCallbackID,
	)
	return err
}
