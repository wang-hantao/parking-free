// Package config loads configuration from environment variables, with
// optional .env file support for local development.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the full configuration tree.
type Config struct {
	HTTP      HTTPConfig
	Postgres  PostgresConfig
	Logging   LoggingConfig
	Stockholm StockholmConfig
	Ingest    IngestConfig
}

type HTTPConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type PostgresConfig struct {
	DSN string
}

type LoggingConfig struct {
	Level  string // "debug" | "info" | "warn" | "error"
	Format string // "json" | "text"
}

type StockholmConfig struct {
	APIKey  string
	BaseURL string
}

type IngestConfig struct {
	Timeout time.Duration
}

// Load reads configuration from environment variables. It optionally
// loads a .env file if one exists in the current working directory.
func Load() (Config, error) {
	_ = godotenv.Load() // ignore "no such file" — env-only is fine

	cfg := Config{
		HTTP: HTTPConfig{
			Addr:         envStr("HTTP_ADDR", ":8080"),
			ReadTimeout:  envDur("HTTP_READ_TIMEOUT", 10*time.Second),
			WriteTimeout: envDur("HTTP_WRITE_TIMEOUT", 15*time.Second),
		},
		Postgres: PostgresConfig{
			DSN: envStr("PG_DSN", ""),
		},
		Logging: LoggingConfig{
			Level:  envStr("LOG_LEVEL", "info"),
			Format: envStr("LOG_FORMAT", "json"),
		},
		Stockholm: StockholmConfig{
			APIKey:  envStr("STOCKHOLM_API_KEY", ""),
			BaseURL: envStr("STOCKHOLM_API_BASE", "https://openparking.stockholm.se/LTF-Tolken/v1"),
		},
		Ingest: IngestConfig{
			Timeout: envDur("INGEST_TIMEOUT", 2*time.Minute),
		},
	}
	return cfg, nil
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
		// Fall through to default on parse error rather than panic;
		// caller can validate explicitly.
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a valid duration; using default %s\n", key, v, def)
	}
	return def
}
