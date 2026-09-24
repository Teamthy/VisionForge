// Package config loads configuration from environment variables with sane
// defaults for local development.  Values are loaded once at startup.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the full runtime configuration for API and worker processes.
type Config struct {
	Environment string
	LogLevel    string

	HTTP HTTPConfig
	DB   DBConfig
	Redis RedisConfig
	Storage StorageConfig
	Auth  AuthConfig
	Worker WorkerConfig
	ML    MLConfig
	Web   WebConfig
	Otel  OtelConfig
	Limits LimitsConfig
	RateLimit RateLimitConfig
}

// HTTPConfig holds HTTP server settings.
type HTTPConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	BaseURL      string
	CORSOrigins  []string
}

// DBConfig holds PostgreSQL settings.
type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	QueryTimeout    time.Duration
}

// DSN returns the PostgreSQL connection string.
func (d DBConfig) DSN() string {
	pw := ""
	if d.Password != "" {
		pw = ":" + d.Password
	}
	sslmode := d.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	return fmt.Sprintf("postgres://%s%s@%s:%d/%s?sslmode=%s",
		d.User, pw, d.Host, d.Port, d.Name, sslmode)
}

// RedisConfig holds Redis settings.
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

// Addr returns the host:port.
func (r RedisConfig) Addr() string { return fmt.Sprintf("%s:%d", r.Host, r.Port) }

// StorageConfig holds object storage settings.
type StorageConfig struct {
	Backend       string // "minio" | "s3"
	Endpoint      string
	AccessKey     string
	SecretKey     string
	Region        string
	Bucket        string
	ForcePathStyle bool
	PresignExpiry time.Duration
}

// AuthConfig holds JWT / password settings.
type AuthConfig struct {
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	BcryptCost    int
}

// WorkerConfig holds worker runtime settings.
type WorkerConfig struct {
	Concurrency   int
	PollInterval  time.Duration
	MaxAttempts   int
	DeadTTL       time.Duration
	QueueName     string
	MetricsP      int // Prometheus/health port exposed by the worker process
}

// MetricsPort returns the worker's /metrics + /healthz listen port.
func (w WorkerConfig) MetricsPort() int {
	if w.MetricsP > 0 {
		return w.MetricsP
	}
	return 8081
}

// MLConfig holds Python ML service settings.
type MLConfig struct {
	ServiceURL    string
	ArtifactsDir  string
	Device        string
	MaxImageDim   int
	MaxImageBytes int64
	MaxInferenceMs int64
}

// WebConfig holds Next.js / web-app settings (server-side usage).
type WebConfig struct {
	Port           int
	NextPublicAPI  string
	NextAuthSecret string
	NextAuthURL    string
}

// OtelConfig holds OpenTelemetry settings.
type OtelConfig struct {
	OTLPEndpoint string
	SamplerArg   float64
	Enabled      bool
}

// LimitsConfig holds per-resource hard limits.
type LimitsConfig struct {
	MaxImageUploadBytes int64
	MaxVideoUploadBytes int64
	MaxRequestBodyBytes int64
	MaxConcurrentJobs   int
	MaxQueueDepth       int
	MaxInferenceMs      int64
}

// RateLimitConfig holds per-endpoint rate limits.
type RateLimitConfig struct {
	AuthPerMin    int
	APIRequestPerMin int
	UploadPerMin  int
}

// Load reads configuration from environment, applying defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Environment: getEnv("ENVIRONMENT", "development"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
	}

	cfg.HTTP = HTTPConfig{
		Host:         getEnv("API_HOST", "0.0.0.0"),
		Port:         getEnvInt("API_PORT", 8080),
		ReadTimeout:  getEnvDuration("API_READ_TIMEOUT", 30*time.Second),
		WriteTimeout: getEnvDuration("API_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:  getEnvDuration("API_IDLE_TIMEOUT", 120*time.Second),
		BaseURL:      getEnv("API_BASE_URL", "http://localhost:8080"),
		CORSOrigins:  getEnvList("API_CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),
	}

	cfg.DB = DBConfig{
		Host:            getEnv("POSTGRES_HOST", "localhost"),
		Port:            getEnvInt("POSTGRES_PORT", 5432),
		User:            getEnv("POSTGRES_USER", "visionforge"),
		Password:        getEnv("POSTGRES_PASSWORD", ""),
		Name:            getEnv("POSTGRES_DB", "visionforge"),
		SSLMode:         getEnv("POSTGRES_SSLMODE", "disable"),
		MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
		ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		QueryTimeout:    getEnvDuration("DB_QUERY_TIMEOUT", 10*time.Second),
	}

	// DATABASE_URL overrides parts if present.
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		// best-effort: leave it to the driver but keep host/port for health checks
		_ = dsn
	}

	cfg.Redis = RedisConfig{
		Host:     getEnv("REDIS_HOST", "localhost"),
		Port:     getEnvInt("REDIS_PORT", 6379),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       getEnvInt("REDIS_DB", 0),
	}

	cfg.Storage = StorageConfig{
		Backend:        getEnv("STORAGE_BACKEND", "minio"),
		Endpoint:       getEnv("S3_ENDPOINT", "http://localhost:9000"),
		AccessKey:      getEnv("S3_ACCESS_KEY", "visionforge"),
		SecretKey:      getEnv("S3_SECRET_KEY", "change_me_in_dev"),
		Region:         getEnv("S3_REGION", "us-east-1"),
		Bucket:         getEnv("S3_BUCKET", "visionforge"),
		ForcePathStyle: getEnvBool("S3_FORCE_PATH_STYLE", true),
		PresignExpiry:  getEnvDuration("S3_PRESIGN_EXPIRY", 15*time.Minute),
	}

	cfg.Auth = AuthConfig{
		JWTSecret:     getEnv("JWT_SECRET", "dev-only-insecure-secret-change-me-please-32b"),
		JWTAccessTTL:  getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL: getEnvDuration("JWT_REFRESH_TTL", 720*time.Hour),
		BcryptCost:    getEnvInt("BCRYPT_COST", 12),
	}

	cfg.Worker = WorkerConfig{
		Concurrency:  getEnvInt("WORKER_CONCURRENCY", 4),
		PollInterval: getEnvDuration("WORKER_POLL_INTERVAL", 1*time.Second),
		MaxAttempts:  getEnvInt("WORKER_MAX_ATTEMPTS", 5),
		DeadTTL:      getEnvDuration("WORKER_DEAD_LEQUEUE_TTL", 720*time.Hour),
		QueueName:    getEnv("WORKER_QUEUE_NAME", "visionforge:jobs"),
		MetricsP:     getEnvInt("WORKER_METRICS_PORT", 8081),
	}

	cfg.ML = MLConfig{
		ServiceURL:      getEnv("ML_SERVICE_URL", "http://localhost:8090"),
		ArtifactsDir:    getEnv("ML_ARTIFACTS_DIR", "./artifacts"),
		Device:          getEnv("ML_DEVICE", "cpu"),
		MaxImageDim:     getEnvInt("ML_MAX_IMAGE_DIMENSION", 4096),
		MaxImageBytes:   getEnvInt64("ML_MAX_IMAGE_BYTES", 20*1024*1024),
		MaxInferenceMs:  getEnvInt64("ML_MAX_INFERENCE_MS", 60000),
	}

	cfg.Web = WebConfig{
		Port:          getEnvInt("WEB_PORT", 3000),
		NextPublicAPI: getEnv("NEXT_PUBLIC_API_URL", "http://localhost:8080/api/v1"),
		NextAuthSecret: getEnv("NEXTAUTH_SECRET", "dev-secret"),
		NextAuthURL:   getEnv("NEXTAUTH_URL", "http://localhost:3000"),
	}

	cfg.Otel = OtelConfig{
		OTLPEndpoint: getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		SamplerArg:   getEnvFloat("OTEL_TRACES_SAMPLER_ARG", 1.0),
		Enabled:      os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "",
	}

	cfg.Limits = LimitsConfig{
		MaxImageUploadBytes: getEnvInt64("MAX_IMAGE_UPLOAD_BYTES", 20*1024*1024),
		MaxVideoUploadBytes: getEnvInt64("MAX_VIDEO_UPLOAD_BYTES", 200*1024*1024),
		MaxRequestBodyBytes: getEnvInt64("MAX_REQUEST_BODY_BYTES", 250*1024*1024),
		MaxConcurrentJobs:   getEnvInt("MAX_CONCURRENT_JOBS", 50),
		MaxQueueDepth:       getEnvInt("MAX_QUEUE_DEPTH", 1000),
		MaxInferenceMs:      getEnvInt64("MAX_INFERENCE_MS", 60000),
	}

	cfg.RateLimit = RateLimitConfig{
		AuthPerMin:        getEnvInt("RATE_LIMIT_AUTH_PER_MIN", 10),
		APIRequestPerMin:  getEnvInt("RATE_LIMIT_API_PER_MIN", 120),
		UploadPerMin:      getEnvInt("RATE_LIMIT_UPLOAD_PER_MIN", 30),
	}

	// Validate critical secrets in production.
	if cfg.Environment == "production" {
		if cfg.Auth.JWTSecret == "" || strings.Contains(cfg.Auth.JWTSecret, "dev-only") {
			return nil, fmt.Errorf("JWT_SECRET must be set in production")
		}
	}

	return cfg, nil
}

// ── env helpers ──────────────────────────────────────────

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func getEnvInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return i
}

func getEnvFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func getEnvList(key string, def []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}
