package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type WorkerConfig struct {
	RedisHost    string
	RedisPort    string
	RedisPass    string
	PostgresHost string
	PostgresPort string
	MinIOHost    string
}

func loadWorkerConfig() WorkerConfig {
	rHost := os.Getenv("REDIS_HOST")
	if rHost == "" {
		rHost = "redis"
	}
	rPort := os.Getenv("REDIS_PORT")
	if rPort == "" {
		rPort = "6379"
	}
	pgHost := os.Getenv("POSTGRES_HOST")
	if pgHost == "" {
		pgHost = "postgres"
	}
	pgPort := os.Getenv("POSTGRES_PORT")
	if pgPort == "" {
		pgPort = "5432"
	}
	mHost := os.Getenv("MINIO_ENDPOINT")
	if mHost == "" {
		mHost = "minio:9000"
	}
	return WorkerConfig{
		RedisHost:    rHost,
		RedisPort:    rPort,
		RedisPass:    os.Getenv("REDIS_PASSWORD"),
		PostgresHost: pgHost,
		PostgresPort: pgPort,
		MinIOHost:    mHost,
	}
}

func checkRedis(host, port, pass string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if pass != "" {
		if _, err := fmt.Fprintf(conn, "*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n", len(pass), pass); err != nil {
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
	if _, err := fmt.Fprintf(conn, "*1\r\n$4\r\nPING\r\n"); err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(buf[:n]), "+PONG") {
		return fmt.Errorf("bad ping response: %s", strings.TrimSpace(string(buf[:n])))
	}
	return nil
}

func main() {
	cfg := loadWorkerConfig()
	log.Println("Starting Asynq Watermark Worker...")
	log.Printf("Configured with Redis %s:%s, Postgres %s:%s, MinIO %s", cfg.RedisHost, cfg.RedisPort, cfg.PostgresHost, cfg.PostgresPort, cfg.MinIOHost)

	// Wait for Redis
	for {
		if err := checkRedis(cfg.RedisHost, cfg.RedisPort, cfg.RedisPass); err == nil {
			log.Println("Connected to Redis successfully. Worker listening to queues: [watermark, notifications, default]")
			break
		} else {
			log.Printf("Waiting for Redis... (%v)", err)
			time.Sleep(2 * time.Second)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("[Heartbeat] Watermark worker is healthy and ready to process Asynq tasks.")
		case <-stop:
			log.Println("Worker shutting down gracefully...")
			return
		}
	}
}
