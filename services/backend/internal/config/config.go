package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime settings for the backend server.
type Config struct {
	AppEnv        string
	AppDebug      bool
	Port          string
	PublicBaseURL string

	PostgresHost string
	PostgresPort string
	PostgresDB   string
	PostgresUser string
	PostgresPass string

	RedisHost string
	RedisPort string
	RedisPass string

	MinIOEndpoint            string
	MinIOPublicEndpoint      string
	MinIOUseSSL              bool
	MinIORootUser            string
	MinIORootPassword        string
	MinIOBucketMaster        string
	MinIOBucketClient        string
	MinIOBucketConsultations string

	MaxMode          string
	MaxAPIURL        string
	MaxBotToken      string
	MaxWebhookSecret string

	YooMode      string
	YooVerifyIP  bool
	YooShopID    string
	YooSecretKey string
	YooReturnURL string

	JWTSecret           string
	MagicLinkBaseURL    string
	MagicLinkTTLMinutes int
	AdminEmail          string
	AdminPassword       string
	DoctorAPIToken      string

	EmailMode string
	SMTPHost  string
	SMTPPort  string
	SMTPUser  string
	SMTPPass  string
	SMTPFrom  string

	LLMProvider       string
	YandexGPTAPIKey   string
	YandexGPTFolderID string
	GigaChatAuthKey   string
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// Load reads Config from environment variables with safe local defaults.
func Load() Config {
	return Config{
		AppEnv:                   envOr("APP_ENV", "local"),
		AppDebug:                 envBool("APP_DEBUG", true),
		Port:                     envOr("PORT", "8080"),
		PublicBaseURL:            envOr("PUBLIC_BASE_URL", "http://localhost"),
		PostgresHost:             envOr("POSTGRES_HOST", "postgres"),
		PostgresPort:             envOr("POSTGRES_PORT", "5432"),
		PostgresDB:               os.Getenv("POSTGRES_DB"),
		PostgresUser:             os.Getenv("POSTGRES_USER"),
		PostgresPass:             os.Getenv("POSTGRES_PASSWORD"),
		RedisHost:                envOr("REDIS_HOST", "redis"),
		RedisPort:                envOr("REDIS_PORT", "6379"),
		RedisPass:                os.Getenv("REDIS_PASSWORD"),
		MinIOEndpoint:            envOr("MINIO_ENDPOINT", "minio:9000"),
		MinIOPublicEndpoint:      envOr("MINIO_PUBLIC_ENDPOINT", "http://localhost/s3"),
		MinIOUseSSL:              envBool("MINIO_USE_SSL", false),
		MinIORootUser:            envOr("MINIO_ROOT_USER", "minio_admin"),
		MinIORootPassword:        envOr("MINIO_ROOT_PASSWORD", "minio_secret_key"),
		MinIOBucketMaster:        envOr("MINIO_BUCKET_MASTER", "zdorovie-master"),
		MinIOBucketClient:        envOr("MINIO_BUCKET_CLIENT", "zdorovie-client-materials"),
		MinIOBucketConsultations: envOr("MINIO_BUCKET_CONSULTATIONS", "zdorovie-consultations"),
		MaxMode:                  envOr("MAX_MODE", "mock"),
		MaxAPIURL:                envOr("MAX_API_URL", "https://platform-api.max.ru"),
		MaxBotToken:              os.Getenv("MAX_BOT_TOKEN"),
		MaxWebhookSecret:         os.Getenv("MAX_WEBHOOK_SECRET"),
		YooMode:                  envOr("YOO_MODE", "mock"),
		YooVerifyIP:              envBool("YOO_VERIFY_IP", false),
		YooShopID:                os.Getenv("YOO_SHOP_ID"),
		YooSecretKey:             os.Getenv("YOO_SECRET_KEY"),
		YooReturnURL:             envOr("YOO_RETURN_URL", "http://localhost/cabinet/payment-success"),
		JWTSecret:                envOr("JWT_SECRET", "default_secret_key_must_be_at_least_32_characters_long_for_security"),
		MagicLinkBaseURL:         envOr("MAGIC_LINK_BASE_URL", "http://localhost/cabinet/auth/magic"),
		MagicLinkTTLMinutes:      envInt("MAGIC_LINK_TTL_MINUTES", 30),
		AdminEmail:               envOr("ADMIN_EMAIL", "doctor@zdorovie.local"),
		AdminPassword:            envOr("ADMIN_PASSWORD", "zdorovie_doctor_pass"),
		DoctorAPIToken:           os.Getenv("DOCTOR_API_TOKEN"),
		EmailMode:                envOr("EMAIL_MODE", "mock"),
		SMTPHost:                 os.Getenv("SMTP_HOST"),
		SMTPPort:                 envOr("SMTP_PORT", "587"),
		SMTPUser:                 os.Getenv("SMTP_USER"),
		SMTPPass:                 os.Getenv("SMTP_PASS"),
		SMTPFrom:                 envOr("SMTP_FROM", "noreply@zdorovie.local"),
		LLMProvider:              envOr("LLM_PROVIDER", "mock"),
		YandexGPTAPIKey:          envOr("YANDEX_GPT_API_KEY", os.Getenv("YANDEX_API_KEY")),
		YandexGPTFolderID:        envOr("YANDEX_GPT_FOLDER_ID", os.Getenv("YANDEX_FOLDER_ID")),
		GigaChatAuthKey:          os.Getenv("GIGACHAT_AUTH_KEY"),
	}
}

// PostgresDSN builds the libpq connection URL.
func (c Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PostgresUser, c.PostgresPass, c.PostgresHost, c.PostgresPort, c.PostgresDB)
}

// RedisAddr returns host:port for Redis.
func (c Config) RedisAddr() string {
	return c.RedisHost + ":" + c.RedisPort
}
