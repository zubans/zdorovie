package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestLoadWorkerConfig_Defaults(t *testing.T) {
	for _, key := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "POSTGRES_HOST", "POSTGRES_PORT", "MINIO_ENDPOINT"} {
		t.Setenv(key, "")
	}

	cfg := loadWorkerConfig()
	want := WorkerConfig{
		RedisHost:    "redis",
		RedisPort:    "6379",
		RedisPass:    "",
		PostgresHost: "postgres",
		PostgresPort: "5432",
		MinIOHost:    "minio:9000",
	}
	if cfg != want {
		t.Errorf("unexpected defaults: got %+v, want %+v", cfg, want)
	}
}

func TestLoadWorkerConfig_FromEnv(t *testing.T) {
	t.Setenv("REDIS_HOST", "redis.local")
	t.Setenv("REDIS_PORT", "6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("POSTGRES_HOST", "db.local")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("MINIO_ENDPOINT", "s3.local:9000")

	cfg := loadWorkerConfig()
	want := WorkerConfig{
		RedisHost:    "redis.local",
		RedisPort:    "6380",
		RedisPass:    "secret",
		PostgresHost: "db.local",
		PostgresPort: "5433",
		MinIOHost:    "s3.local:9000",
	}
	if cfg != want {
		t.Errorf("unexpected config: got %+v, want %+v", cfg, want)
	}
}

// fakeRedis answers each RESP command with the next reply from replies
// and returns the host and port it listens on.
func fakeRedis(t *testing.T, replies ...string) (string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for _, reply := range replies {
			if err := readCommand(reader); err != nil {
				return
			}
			if _, err := conn.Write([]byte(reply)); err != nil {
				return
			}
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	return host, port
}

// readCommand reads one RESP array command: "*N" and then N pairs "$len" + value.
func readCommand(r *bufio.Reader) error {
	header, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(header), "*%d", &count); err != nil {
		return err
	}
	for i := 0; i < count*2; i++ {
		if _, err := r.ReadString('\n'); err != nil {
			return err
		}
	}
	return nil
}

func TestCheckRedis_Pong(t *testing.T) {
	host, port := fakeRedis(t, "+PONG\r\n")
	if err := checkRedis(host, port, ""); err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestCheckRedis_ErrorReply(t *testing.T) {
	host, port := fakeRedis(t, "-ERR unknown command\r\n")
	err := checkRedis(host, port, "")
	if err == nil {
		t.Fatal("expected error on -ERR reply")
	}
	if !strings.Contains(err.Error(), "bad ping response") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckRedis_AuthOK(t *testing.T) {
	host, port := fakeRedis(t, "+OK\r\n", "+PONG\r\n")
	if err := checkRedis(host, port, "secret"); err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestCheckRedis_AuthFailed(t *testing.T) {
	host, port := fakeRedis(t, "-WRONGPASS invalid username-password pair\r\n")
	err := checkRedis(host, port, "wrong")
	if err == nil {
		t.Fatal("expected error on wrong password")
	}
	if !strings.Contains(err.Error(), "redis auth failed") || !strings.Contains(err.Error(), "WRONGPASS") {
		t.Errorf("expected auth error with redis reply, got %v", err)
	}
}

func TestCheckRedis_Unreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()

	if err := checkRedis(host, port, ""); err == nil {
		t.Error("expected error for closed port")
	}
}
