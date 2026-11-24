package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/smtp"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"zdorovie/worker/internal/watermark"
)

const (
	TaskPDFWatermark = "pdf:watermark"
	TaskEmailSend    = "email:send"
)

// WorkerConfig holds core service settings loaded from environment variables.
type WorkerConfig struct {
	RedisHost    string
	RedisPort    string
	RedisPass    string
	PostgresHost string
	PostgresPort string
	MinIOHost    string
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadWorkerConfig() WorkerConfig {
	return WorkerConfig{
		RedisHost:    envOrDefault("REDIS_HOST", "redis"),
		RedisPort:    envOrDefault("REDIS_PORT", "6379"),
		RedisPass:    os.Getenv("REDIS_PASSWORD"),
		PostgresHost: envOrDefault("POSTGRES_HOST", "postgres"),
		PostgresPort: envOrDefault("POSTGRES_PORT", "5432"),
		MinIOHost:    envOrDefault("MINIO_ENDPOINT", "minio:9000"),
	}
}

func checkRedis(host, port, pass string) error {
	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(conn)
	if pass != "" {
		fmt.Fprintf(conn, "*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n", len(pass), pass)
		line, err := r.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "+OK") {
			return fmt.Errorf("redis auth failed: %s", strings.TrimSpace(line))
		}
	}
	fmt.Fprintf(conn, "*1\r\n$4\r\nPING\r\n")
	line, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "+PONG") {
		return fmt.Errorf("bad ping response: %s", strings.TrimSpace(line))
	}
	return nil
}

type watermarkPayload struct {
	ClientMaterialID int64  `json:"client_material_id"`
	UserID           int64  `json:"user_id"`
	OrderID          int64  `json:"order_id"`
	OrderNumber      string `json:"order_number"`
	MaterialID       int    `json:"material_id"`
	FullName         string `json:"full_name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	MasterKey        string `json:"master_key"`
	ClientKey        string `json:"client_key"`
}

type emailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type workerRuntime struct {
	pool         *pgxpool.Pool
	s3           *minio.Client
	bucketMaster string
	bucketClient string
	emailMode    string
	smtpHost     string
	smtpPort     string
	smtpUser     string
	smtpPass     string
	smtpFrom     string
}

func (rt *workerRuntime) handleWatermarkTask(ctx context.Context, t *asynq.Task) error {
	var p watermarkPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal watermark payload: %w", asynq.SkipRetry)
	}
	log.Printf("[pdf:watermark] processing client_material=%d user=%d order=%s key=%s", p.ClientMaterialID, p.UserID, p.OrderNumber, p.MasterKey)

	_, _ = rt.pool.Exec(ctx, `UPDATE client_materials SET status = 'processing', error_message = NULL WHERE id = $1`, p.ClientMaterialID)

	markFailed := func(err error) error {
		_, _ = rt.pool.Exec(ctx, `UPDATE client_materials SET status = 'failed', error_message = $2 WHERE id = $1`, p.ClientMaterialID, err.Error())
		return err
	}

	obj, err := rt.s3.GetObject(ctx, rt.bucketMaster, p.MasterKey, minio.GetObjectOptions{})
	if err != nil {
		return markFailed(fmt.Errorf("download master PDF %s/%s: %w", rt.bucketMaster, p.MasterKey, err))
	}
	masterBytes, err := io.ReadAll(obj)
	obj.Close()
	if err != nil {
		return markFailed(fmt.Errorf("read master PDF bytes: %w", err))
	}

	stamped, err := watermark.StampPDF(masterBytes, watermark.Input{
		FullName:    p.FullName,
		Email:       p.Email,
		Phone:       p.Phone,
		OrderNumber: p.OrderNumber,
		IssuedAt:    time.Now().UTC(),
	})
	if err != nil {
		return markFailed(fmt.Errorf("stamp watermark: %w", err))
	}

	_, err = rt.s3.PutObject(ctx, rt.bucketClient, p.ClientKey, bytes.NewReader(stamped), int64(len(stamped)), minio.PutObjectOptions{
		ContentType: "application/pdf",
	})
	if err != nil {
		return markFailed(fmt.Errorf("upload stamped PDF to %s/%s: %w", rt.bucketClient, p.ClientKey, err))
	}

	_, err = rt.pool.Exec(ctx, `UPDATE client_materials SET status = 'ready', error_message = NULL, generated_at = NOW() WHERE id = $1`, p.ClientMaterialID)
	if err != nil {
		return err
	}
	log.Printf("[pdf:watermark] completed client_material=%d -> %s/%s (%d bytes)", p.ClientMaterialID, rt.bucketClient, p.ClientKey, len(stamped))
	return nil
}

func (rt *workerRuntime) handleEmailTask(ctx context.Context, t *asynq.Task) error {
	var p emailPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal email payload: %w", asynq.SkipRetry)
	}
	if rt.emailMode != "smtp" || rt.smtpHost == "" {
		log.Printf("[email:send Mock] to=%s subject=%q", p.To, p.Subject)
		return nil
	}
	addr := net.JoinHostPort(rt.smtpHost, rt.smtpPort)
	var auth smtp.Auth
	if rt.smtpUser != "" {
		auth = smtp.PlainAuth("", rt.smtpUser, rt.smtpPass, rt.smtpHost)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		rt.smtpFrom, p.To, p.Subject, p.Body)
	return smtp.SendMail(addr, auth, rt.smtpFrom, []string{p.To}, []byte(msg))
}

func main() {
	cfg := loadWorkerConfig()
	concurrency, _ := strconv.Atoi(envOrDefault("WORKER_CONCURRENCY", "4"))
	if concurrency <= 0 {
		concurrency = 4
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		envOrDefault("POSTGRES_USER", "zdorovie_user"),
		envOrDefault("POSTGRES_PASSWORD", "zdorovie_pass"),
		cfg.PostgresHost,
		cfg.PostgresPort,
		envOrDefault("POSTGRES_DB", "zdorovie_db"),
	)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(rootCtx, dsn)
	if err != nil {
		log.Fatalf("connect postgres failed: %v", err)
	}
	defer pool.Close()

	s3Client, err := minio.New(cfg.MinIOHost, &minio.Options{
		Creds:  credentials.NewStaticV4(envOrDefault("MINIO_ROOT_USER", "minioadmin"), envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123"), ""),
		Secure: strings.EqualFold(envOrDefault("MINIO_USE_SSL", "false"), "true"),
		Region: "us-east-1",
	})
	if err != nil {
		log.Fatalf("connect minio failed: %v", err)
	}

	rt := &workerRuntime{
		pool:         pool,
		s3:           s3Client,
		bucketMaster: envOrDefault("MINIO_BUCKET_MASTER", "zdorovie-master"),
		bucketClient: envOrDefault("MINIO_BUCKET_CLIENT", envOrDefault("MINIO_BUCKET_CLIENT_MATERIALS", "zdorovie-client-materials")),
		emailMode:    strings.ToLower(envOrDefault("EMAIL_MODE", "mock")),
		smtpHost:     os.Getenv("SMTP_HOST"),
		smtpPort:     envOrDefault("SMTP_PORT", "587"),
		smtpUser:     os.Getenv("SMTP_USER"),
		smtpPass:     envOrDefault("SMTP_PASS", os.Getenv("SMTP_PASSWORD")),
		smtpFrom:     envOrDefault("SMTP_FROM", "noreply@zdorovie.local"),
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     net.JoinHostPort(cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPass,
		},
		asynq.Config{
			Concurrency: concurrency,
			Queues: map[string]int{
				"watermark":     6,
				"notifications": 3,
				"default":       1,
			},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskPDFWatermark, rt.handleWatermarkTask)
	mux.HandleFunc(TaskEmailSend, rt.handleEmailTask)

	log.Printf("Starting Asynq PDF Watermarking & Notification Worker (redis=%s:%s minio=%s concurrency=%d)",
		cfg.RedisHost, cfg.RedisPort, cfg.MinIOHost, concurrency)

	go func() {
		if err := srv.Run(mux); err != nil {
			log.Fatalf("asynq server error: %v", err)
		}
	}()

	<-rootCtx.Done()
	log.Println("Shutting down Asynq worker gracefully...")
	srv.Shutdown()
}
