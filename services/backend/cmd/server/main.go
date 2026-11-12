package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	AppEnv       string
	Port         string
	PostgresHost string
	PostgresPort string
	PostgresDB   string
	PostgresUser string
	PostgresPass string
	RedisHost    string
	RedisPort    string
	RedisPass    string
	MaxSecret    string
	YooShopID    string
	YooSecretKey string
}

func (c Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PostgresUser, c.PostgresPass, c.PostgresHost, c.PostgresPort, c.PostgresDB)
}

func loadConfig() Config {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "local"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	pgHost := os.Getenv("POSTGRES_HOST")
	if pgHost == "" {
		pgHost = "postgres"
	}
	pgPort := os.Getenv("POSTGRES_PORT")
	if pgPort == "" {
		pgPort = "5432"
	}
	rHost := os.Getenv("REDIS_HOST")
	if rHost == "" {
		rHost = "redis"
	}
	rPort := os.Getenv("REDIS_PORT")
	if rPort == "" {
		rPort = "6379"
	}
	return Config{
		AppEnv:       appEnv,
		Port:         port,
		PostgresHost: pgHost,
		PostgresPort: pgPort,
		PostgresDB:   os.Getenv("POSTGRES_DB"),
		PostgresUser: os.Getenv("POSTGRES_USER"),
		PostgresPass: os.Getenv("POSTGRES_PASSWORD"),
		RedisHost:    rHost,
		RedisPort:    rPort,
		RedisPass:    os.Getenv("REDIS_PASSWORD"),
		MaxSecret:    os.Getenv("MAX_WEBHOOK_SECRET"),
		YooShopID:    os.Getenv("YOO_SHOP_ID"),
		YooSecretKey: os.Getenv("YOO_SECRET_KEY"),
	}
}

type Checker interface {
	CheckDB(ctx context.Context) error
	CheckRedis(ctx context.Context) error
}

// DefaultChecker checks real dependencies. The PostgreSQL pool is created once
// at startup and reused by every /healthz call; it is closed on shutdown.
type DefaultChecker struct {
	cfg  Config
	pool *pgxpool.Pool
}

// NewDefaultChecker creates the shared PostgreSQL pool. pgxpool connects lazily,
// so the pool is created even if the database is not reachable yet.
// Without database credentials the pool is not created and CheckDB reports an error.
func NewDefaultChecker(ctx context.Context, cfg Config) (*DefaultChecker, error) {
	checker := &DefaultChecker{cfg: cfg}
	if cfg.PostgresDB == "" && cfg.PostgresUser == "" {
		return checker, nil
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.PostgresDSN())
	if err != nil {
		return nil, fmt.Errorf("postgres config parse error: %w", err)
	}
	poolCfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres pool creation error: %w", err)
	}
	checker.pool = pool
	return checker, nil
}

// Close releases the PostgreSQL pool.
func (d *DefaultChecker) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
}

func (d *DefaultChecker) CheckDB(ctx context.Context) error {
	if d.pool == nil {
		return fmt.Errorf("database credentials not configured")
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var one int
	err := d.pool.QueryRow(ctxTimeout, "SELECT 1").Scan(&one)
	if err != nil {
		return fmt.Errorf("postgres SELECT 1 query failed: %w", err)
	}
	if one != 1 {
		return fmt.Errorf("unexpected query result: %d", one)
	}
	return nil
}

func (d *DefaultChecker) CheckRedis(ctx context.Context) error {
	addr := net.JoinHostPort(d.cfg.RedisHost, d.cfg.RedisPort)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if d.cfg.RedisPass != "" {
		_, err = fmt.Fprintf(conn, "*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n", len(d.cfg.RedisPass), d.cfg.RedisPass)
		if err != nil {
			return err
		}
		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(buf[:n]), "+OK") {
			return fmt.Errorf("redis auth failed: %s", strings.TrimSpace(string(buf[:n])))
		}
	}

	_, err = fmt.Fprintf(conn, "*1\r\n$4\r\nPING\r\n")
	if err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(buf[:n]), "+PONG") {
		return fmt.Errorf("unexpected ping response: %s", strings.TrimSpace(string(buf[:n])))
	}
	return nil
}

func SetupRouter(cfg Config, checker Checker) http.Handler {
	mux := http.NewServeMux()

	// Healthcheck: real DB SELECT 1 check and Redis PING
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		dbErr := checker.CheckDB(r.Context())
		redisErr := checker.CheckRedis(r.Context())

		// Error details (hosts, user names, server replies) go to the log only:
		// /healthz is reachable from outside through nginx.
		dbStatus := "connected"
		if dbErr != nil {
			dbStatus = "error"
			log.Printf("healthz: postgres check failed: %v", dbErr)
		}

		redisStatus := "connected"
		if redisErr != nil {
			redisStatus = "error"
			log.Printf("healthz: redis check failed: %v", redisErr)
		}

		resp := map[string]interface{}{
			"status":    "ok",
			"db":        dbStatus,
			"redis":     redisStatus,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		if dbErr != nil || redisErr != nil {
			resp["status"] = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_ = json.NewEncoder(w).Encode(resp)
	})

	// MAX Messenger Webhook handler
	handleMaxWebhook := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if cfg.MaxSecret == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		receivedSecret := r.Header.Get("X-Max-Bot-Api-Secret")
		if subtle.ConstantTimeCompare([]byte(receivedSecret), []byte(cfg.MaxSecret)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var event struct {
			UpdateType string `json:"update_type"`
			Timestamp  int64  `json:"timestamp"`
		}

		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		if event.UpdateType == "" {
			http.Error(w, "Invalid event", http.StatusBadRequest)
			return
		}

		log.Printf("[MAX Webhook] type=%q", event.UpdateType)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","processed":true}`))
	}

	mux.HandleFunc("/api/v1/webhooks/max", handleMaxWebhook)
	mux.HandleFunc("/webhooks/max", handleMaxWebhook)

	// YooKassa Webhook handler
	handleYooWebhook := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if cfg.YooSecretKey == "" || cfg.YooShopID == "" {
			log.Printf("[YooKassa Webhook] YooKassa credentials not configured")
			http.Error(w, "Service Unavailable: YooKassa credentials not configured", http.StatusServiceUnavailable)
			return
		}

		// TODO: Verify YooKassa sender IP addresses (185.71.76.0/27, 185.71.77.0/27, 77.75.153.0/25, 77.75.156.11, 77.75.156.35)
		// TODO: Parse event notification and perform GET https://api.yookassa.ru/v3/payments/{id} for idempotent verification

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var notification struct {
			Type   string `json:"type"`
			Event  string `json:"event"`
			Object struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"object"`
		}

		if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		log.Printf("[YooKassa Webhook] type=%q event=%q payment_id=%q status=%q",
			notification.Type, notification.Event, notification.Object.ID, notification.Object.Status)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","received":true}`))
	}
	mux.HandleFunc("/api/v1/webhooks/yukassa", handleYooWebhook)
	mux.HandleFunc("/webhooks/yukassa", handleYooWebhook)
	mux.HandleFunc("/webhooks/yookassa", handleYooWebhook)

	// API tariffs placeholder
	mux.HandleFunc("/api/v1/tariffs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","message":"Use PostgreSQL for tariffs list"}`))
	})

	return mux
}

func main() {
	cfg := loadConfig()
	if cfg.MaxSecret == "" && cfg.AppEnv != "test" {
		log.Fatalf("Fatal: MAX_WEBHOOK_SECRET must be configured")
	}

	checker, err := NewDefaultChecker(context.Background(), cfg)
	if err != nil {
		log.Fatalf("Fatal: %v", err)
	}
	defer checker.Close()

	mux := SetupRouter(cfg, checker)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Backend API server started on port %s (env: %s)", cfg.Port, cfg.AppEnv)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server listen error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down backend server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Backend server stopped")
}
