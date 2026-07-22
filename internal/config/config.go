// Package config loads and validates all runtime configuration from the
// environment. It fails fast: a misconfigured security-critical value (missing
// encryption key in production, etc.) aborts startup rather than silently
// running insecurely.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env  string
	Prod bool

	HTTPPort        string
	BodyLimitBytes  int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	DatabaseURL           string
	DatabaseReplicaURL    string
	DBMaxConns            int32
	DBMinConns            int32
	DBMaxConnLifetime     time.Duration

	RedisURL string

	CORSAllowedOrigins []string
	TrustedProxies     []string

	PIIEncryptionKey    []byte
	BlindIndexKey       []byte
	VerificationHMACKey []byte

	TurnstileSecretKey string
	TurnstileEnabled   bool

	RateGeneralPerMin int
	RateSubmitPerMin  int
	RateLookupPerMin  int

	CurrentBatch string

	// AutoMigrate applies the embedded, idempotent schema migrations on startup.
	// On by default so a fresh managed database (e.g. Render/Railway free tier,
	// where a shell/psql may be unavailable) becomes usable without a manual
	// step. Set AUTO_MIGRATE=false to manage schema out-of-band instead.
	AutoMigrate bool
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Env: getEnv("APP_ENV", "development"),
		// PaaS platforms (Vercel, Railway, Render, Fly, Heroku) inject the port to
		// bind as $PORT and route traffic there; honour it first so the deploy's
		// health check can reach us, then our own HTTP_PORT, then a local default.
		HTTPPort:           getEnv("PORT", getEnv("HTTP_PORT", "8080")),
		BodyLimitBytes:     getInt("HTTP_BODY_LIMIT_BYTES", 8<<20),
		ReadTimeout:        getSeconds("READ_TIMEOUT_SECONDS", 15),
		WriteTimeout:       getSeconds("WRITE_TIMEOUT_SECONDS", 20),
		IdleTimeout:        getSeconds("IDLE_TIMEOUT_SECONDS", 60),
		ShutdownTimeout:    getSeconds("SHUTDOWN_TIMEOUT_SECONDS", 25),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		DatabaseReplicaURL: os.Getenv("DATABASE_REPLICA_URL"),
		DBMaxConns:         int32(getInt("DB_MAX_CONNS", 25)),
		DBMinConns:         int32(getInt("DB_MIN_CONNS", 2)),
		DBMaxConnLifetime:  time.Duration(getInt("DB_MAX_CONN_LIFETIME_MINUTES", 30)) * time.Minute,
		RedisURL:           os.Getenv("REDIS_URL"),
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "")),
		TrustedProxies:     splitCSV(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1")),
		TurnstileSecretKey: os.Getenv("TURNSTILE_SECRET_KEY"),
		TurnstileEnabled:   getBool("TURNSTILE_ENABLED", true),
		RateGeneralPerMin:  getInt("RATE_LIMIT_GENERAL_PER_MIN", 120),
		RateSubmitPerMin:   getInt("RATE_LIMIT_SUBMIT_PER_MIN", 5),
		RateLookupPerMin:   getInt("RATE_LIMIT_LOOKUP_PER_MIN", 10),
		CurrentBatch:       getEnv("CURRENT_BATCH", "১ম ব্যাচ"),
		AutoMigrate:        getBool("AUTO_MIGRATE", true),
	}
	c.Prod = c.Env == "production"

	var err error
	if c.PIIEncryptionKey, err = decodeKey("PII_ENCRYPTION_KEY"); err != nil {
		return nil, fmt.Errorf("PII_ENCRYPTION_KEY: %w", err)
	}
	if c.BlindIndexKey, err = decodeKey("BLIND_INDEX_KEY"); err != nil {
		return nil, fmt.Errorf("BLIND_INDEX_KEY: %w", err)
	}
	if c.VerificationHMACKey, err = decodeKey("VERIFICATION_HMAC_KEY"); err != nil {
		return nil, fmt.Errorf("VERIFICATION_HMAC_KEY: %w", err)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.Prod {
		if len(c.CORSAllowedOrigins) == 0 {
			return errors.New("CORS_ALLOWED_ORIGINS is required in production")
		}
		if c.TurnstileEnabled && c.TurnstileSecretKey == "" {
			return errors.New("TURNSTILE_SECRET_KEY required when TURNSTILE_ENABLED=true in production")
		}
	}
	return nil
}

// ---- env helpers ------------------------------------------------------------

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getSeconds(k string, def int) time.Duration {
	return time.Duration(getInt(k, def)) * time.Second
}

// decodeKey reads a base64-encoded 32-byte key from the environment. In
// production a missing/placeholder key is fatal; in development a deterministic
// insecure fallback is derived so the server still boots locally.
func decodeKey(name string) ([]byte, error) {
	raw := os.Getenv(name)
	if raw == "" || strings.HasPrefix(raw, "CHANGE_ME") {
		if os.Getenv("APP_ENV") == "production" {
			return nil, errors.New("must be a base64-encoded 32-byte key in production")
		}
		dev := make([]byte, 32) // dev-only, insecure, deterministic
		copy(dev, []byte("dev-insecure-key::"+name))
		return dev, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
