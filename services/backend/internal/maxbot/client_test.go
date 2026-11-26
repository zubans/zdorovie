package maxbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
)

type pollTestStore struct {
	mu       sync.Mutex
	events   map[string]bool
	sessions map[string]db.BotFSMSession
}

func newPollTestStore() *pollTestStore {
	return &pollTestStore{
		events:   make(map[string]bool),
		sessions: make(map[string]db.BotFSMSession),
	}
}

func (s *pollTestStore) RecordWebhookEvent(_ context.Context, provider, eventKey, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := provider + ":" + eventKey
	if s.events[k] {
		return false, nil
	}
	s.events[k] = true
	return true, nil
}

func (s *pollTestStore) GetFSMSession(_ context.Context, maxUserID string) (*db.BotFSMSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[maxUserID]
	if !ok {
		return nil, nil
	}
	cp := sess
	return &cp, nil
}

func (s *pollTestStore) SaveFSMSession(_ context.Context, sess db.BotFSMSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.UserID] = sess
	return nil
}

func (s *pollTestStore) ListTariffs(context.Context, bool) ([]db.Tariff, error) {
	return nil, nil
}

func (s *pollTestStore) GetTariffByCode(context.Context, string) (*db.Tariff, error) {
	return nil, nil
}

func (s *pollTestStore) UpsertUserByMaxOrEmail(context.Context, string, string, string, string, string, string) (*db.User, error) {
	return &db.User{}, nil
}

func (s *pollTestStore) CreateOrder(context.Context, int64, db.Tariff, bool) (*db.Order, error) {
	return &db.Order{}, nil
}

func (s *pollTestStore) CreateQuestion(context.Context, string, string, string) (*db.Question, error) {
	return &db.Question{}, nil
}

func (s *pollTestStore) MarkChannelMembershipByMaxUser(context.Context, string, bool) error {
	return nil
}

type noopPayCreator struct{}

func (noopPayCreator) CreatePaymentForOrder(context.Context, db.Order, db.User, db.Tariff) (string, error) {
	return "http://localhost/pay", nil
}

func TestClientIsLive(t *testing.T) {
	cases := []struct {
		mode  string
		token string
		want  bool
	}{
		{mode: "mock", token: "real_token", want: false},
		{mode: "live", token: "", want: false},
		{mode: "live", token: "test_max_bot_token", want: false},
		{mode: "live", token: "real_token_123", want: true},
		{mode: "LIVE", token: "real_token_123", want: true},
	}
	for _, tc := range cases {
		c := NewClient(config.Config{MaxMode: tc.mode, MaxBotToken: tc.token})
		if got := c.IsLive(); got != tc.want {
			t.Errorf("IsLive(mode=%q, token=%q) = %v, want %v", tc.mode, tc.token, got, tc.want)
		}
	}
}

func TestClientLiveMethodsAndPolling(t *testing.T) {
	const token = "live_test_token_xyz"
	var pollCalls atomic.Int32
	var sentMessages atomic.Int32
	var gotMarkers []string
	var markerMu sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/me":
			_, _ = w.Write([]byte(`{"user_id":580983135,"first_name":"Doc_Dina","username":"se15001878_bot","is_bot":true}`))

		case r.Method == http.MethodGet && r.URL.Path == "/subscriptions":
			_, _ = w.Write([]byte(`{"subscriptions":[]}`))

		case r.Method == http.MethodGet && r.URL.Path == "/updates":
			callNum := pollCalls.Add(1)
			markerMu.Lock()
			gotMarkers = append(gotMarkers, r.URL.Query().Get("marker"))
			markerMu.Unlock()

			switch callNum {
			case 1:
				_, _ = w.Write([]byte(`{
					"updates": [
						{"update_type":"bot_started","timestamp":1773489200000,"user":{"user_id":991001,"first_name":"Анна","last_name":"Иванова"}}
					],
					"marker": 501
				}`))
			case 2:
				_, _ = w.Write([]byte(`{
					"updates": [
						{"update_type":"message_created","timestamp":1773489205000,"message":{"sender":{"user_id":991001,"first_name":"Анна"},"body":{"mid":"mid.123","text":"/start"}}}
					],
					"marker": 502
				}`))
			default:
				cancel()
				_, _ = w.Write([]byte(`{"updates":[],"marker":502}`))
			}

		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			if r.URL.Query().Get("user_id") != "991001" {
				t.Errorf("unexpected user_id query: %q", r.URL.RawQuery)
			}
			var payload OutgoingMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode outgoing message: %v", err)
			}
			if payload.Format != "markdown" || strings.TrimSpace(payload.Text) == "" {
				t.Errorf("invalid outgoing message payload: %+v", payload)
			}
			sentMessages.Add(1)
			_, _ = w.Write([]byte(`{"message":{"body":{"mid":"mid.out"}}}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := config.Config{
		MaxMode:     "live",
		MaxAPIURL:   srv.URL,
		MaxBotToken: token,
	}
	client := NewClient(cfg)

	profile, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe failed: %v", err)
	}
	if profile.UserID != 580983135 || profile.Username != "se15001878_bot" {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	subs, err := client.GetSubscriptions(context.Background())
	if err != nil || len(subs) != 0 {
		t.Fatalf("GetSubscriptions failed: subs=%+v err=%v", subs, err)
	}

	eng := NewEngine(newPollTestStore(), client, noopPayCreator{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.StartPolling(ctx, eng)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartPolling did not exit after context cancellation")
	}

	if got := sentMessages.Load(); got != 2 {
		t.Fatalf("expected 2 outbound welcome messages from polling loop, got %d", got)
	}
	markerMu.Lock()
	defer markerMu.Unlock()
	if len(gotMarkers) < 2 || gotMarkers[0] != "" || gotMarkers[1] != "501" {
		t.Fatalf("unexpected marker progression: %v", gotMarkers)
	}
}
