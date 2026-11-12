package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockChecker struct {
	dbErr    error
	redisErr error
}

func (m mockChecker) CheckDB(ctx context.Context) error {
	return m.dbErr
}

func (m mockChecker) CheckRedis(ctx context.Context) error {
	return m.redisErr
}

func TestHealthz_Healthy(t *testing.T) {
	cfg := Config{AppEnv: "test"}
	checker := mockChecker{dbErr: nil, redisErr: nil}
	mux := SetupRouter(cfg, checker)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("expected status ok in body, got %s", body)
	}
	if !strings.Contains(body, `"db":"connected"`) {
		t.Errorf("expected db connected, got %s", body)
	}
	if !strings.Contains(body, `"redis":"connected"`) {
		t.Errorf("expected redis connected, got %s", body)
	}
}

func TestHealthz_Degraded(t *testing.T) {
	cfg := Config{AppEnv: "test"}
	checker := mockChecker{dbErr: errors.New("connection refused"), redisErr: nil}
	mux := SetupRouter(cfg, checker)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"degraded"`) {
		t.Errorf("expected status degraded in body, got %s", body)
	}
	if !strings.Contains(body, `"db":"error"`) {
		t.Errorf("expected db error in body, got %s", body)
	}
	if strings.Contains(body, "connection refused") {
		t.Errorf("error details must not leak into response, got %s", body)
	}
	if !strings.Contains(body, `"redis":"connected"`) {
		t.Errorf("expected redis connected, got %s", body)
	}
}

func TestMaxWebhook_Auth(t *testing.T) {
	cfg := Config{
		AppEnv:    "test",
		MaxSecret: "test_secret_123",
	}
	checker := mockChecker{}
	mux := SetupRouter(cfg, checker)

	t.Run("Method Not Allowed on GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/max", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	t.Run("Unauthorized on missing or invalid secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/max", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
		}

		reqInvalid := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/max", strings.NewReader(`{}`))
		reqInvalid.Header.Set("X-Max-Bot-Api-Secret", "wrong_secret")
		recInvalid := httptest.NewRecorder()
		mux.ServeHTTP(recInvalid, reqInvalid)
		if recInvalid.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", recInvalid.Code)
		}
	})

	t.Run("Authorized with valid secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/max", strings.NewReader(`{"update_type":"message_created","timestamp":1}`))
		req.Header.Set("X-Max-Bot-Api-Secret", "test_secret_123")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"processed":true`) {
			t.Errorf("expected processed:true, got %s", rec.Body.String())
		}
	})

	t.Run("Bad Request on empty update_type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/max", strings.NewReader(`{"event":"test"}`))
		req.Header.Set("X-Max-Bot-Api-Secret", "test_secret_123")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Bad Request on malformed JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/max", strings.NewReader(`{not json`))
		req.Header.Set("X-Max-Bot-Api-Secret", "test_secret_123")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestDefaultChecker_NoDatabaseCredentials(t *testing.T) {
	checker, err := NewDefaultChecker(context.Background(), Config{AppEnv: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer checker.Close()

	if checker.pool != nil {
		t.Errorf("expected no pool without credentials")
	}
	if err := checker.CheckDB(context.Background()); err == nil {
		t.Errorf("expected CheckDB error without credentials")
	}
}

func TestDefaultChecker_PoolCreatedOnce(t *testing.T) {
	cfg := Config{
		AppEnv:       "test",
		PostgresHost: "127.0.0.1",
		PostgresPort: "1",
		PostgresDB:   "db",
		PostgresUser: "user",
		PostgresPass: "pass",
	}
	checker, err := NewDefaultChecker(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pool creation must not require a reachable database: %v", err)
	}
	defer checker.Close()

	pool := checker.pool
	if pool == nil {
		t.Fatal("expected pool to be created")
	}
	for i := 0; i < 2; i++ {
		if err := checker.CheckDB(context.Background()); err == nil {
			t.Errorf("expected CheckDB error for unreachable database")
		}
	}
	if checker.pool != pool {
		t.Errorf("expected the same pool to be reused between checks")
	}
}

func TestYooKassaWebhook(t *testing.T) {
	cfg := Config{
		AppEnv:       "test",
		YooShopID:    "test_shop",
		YooSecretKey: "test_key",
	}
	checker := mockChecker{}
	mux := SetupRouter(cfg, checker)

	t.Run("Method Not Allowed on GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/yukassa", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
		}
	})

	t.Run("Service Unavailable when credentials missing", func(t *testing.T) {
		unconfiguredMux := SetupRouter(Config{AppEnv: "test"}, checker)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/yukassa", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		unconfiguredMux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got %d", rec.Code)
		}
	})

	t.Run("Successful receipt with credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/yukassa", strings.NewReader(`{"type":"notification"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"received":true`) {
			t.Errorf("expected received:true, got %s", rec.Body.String())
		}
	})
}
