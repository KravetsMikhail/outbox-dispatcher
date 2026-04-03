package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds runtime settings from environment and CLI.
type Config struct {
	DatabaseURL string

	PostBaseURL string
	OutboxTable string

	CronExpr     string
	PollInterval time.Duration

	KeycloakBaseURL      string
	KeycloakRealm        string
	KeycloakTokenURL     string
	KeycloakClientID     string
	KeycloakClientSecret string

	MaxRetryAttempts  int
	RetryBaseInterval time.Duration
	TokenRetryDelay   time.Duration

	// UIListenAddr e.g. ":8484" for the dashboard (empty = disabled)
	UIListenAddr string
}

func Load() (*Config, error) {
	var (
		envFile      = flag.String("env", "", "path to env file; if set, file must exist. If omitted, loads .env when present (missing .env is ignored)")
		scheduleFlag = flag.String("schedule", "", "poll interval, e.g. 30s, 1m (overrides SCHEDULE_INTERVAL)")
		cronFlag     = flag.String("cron", "", "cron expression, e.g. */5 * * * * (overrides SCHEDULE_CRON)")
	)
	flag.Parse()

	if path := strings.TrimSpace(*envFile); path != "" {
		if err := godotenv.Load(path); err != nil {
			return nil, fmt.Errorf("env file %q: %w", path, err)
		}
	} else {
		_ = godotenv.Load(".env")
	}

	if strings.TrimSpace(*cronFlag) != "" && strings.TrimSpace(*scheduleFlag) != "" {
		return nil, errors.New("use only one of -cron or -schedule")
	}

	cfg := &Config{
		DatabaseURL:          strings.TrimSpace(os.Getenv("DATABASE_URL")),
		PostBaseURL:          strings.TrimSpace(os.Getenv("POST_BASE_URL")),
		OutboxTable:          strings.TrimSpace(os.Getenv("OUTBOX_TABLE")),
		KeycloakBaseURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("KEYCLOAK_BASE_URL")), "/"),
		KeycloakRealm:        strings.TrimSpace(os.Getenv("KEYCLOAK_REALM")),
		KeycloakTokenURL:     strings.TrimSpace(os.Getenv("KEYCLOAK_TOKEN_URL")),
		KeycloakClientID:     strings.TrimSpace(os.Getenv("KEYCLOAK_CLIENT_ID")),
		KeycloakClientSecret: strings.TrimSpace(os.Getenv("KEYCLOAK_CLIENT_SECRET")),
	}

	if cfg.OutboxTable == "" {
		cfg.OutboxTable = "outbox_messages"
	}

	cfg.MaxRetryAttempts = 10
	if s := strings.TrimSpace(os.Getenv("MAX_RETRY_ATTEMPTS")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("MAX_RETRY_ATTEMPTS: invalid value %q", s)
		}
		cfg.MaxRetryAttempts = n
	}

	cfg.RetryBaseInterval = 30 * time.Second
	if s := strings.TrimSpace(os.Getenv("RETRY_BASE_INTERVAL")); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("RETRY_BASE_INTERVAL: %w", err)
		}
		cfg.RetryBaseInterval = d
	}

	cfg.TokenRetryDelay = 30 * time.Second
	if s := strings.TrimSpace(os.Getenv("TOKEN_RETRY_DELAY")); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("TOKEN_RETRY_DELAY: %w", err)
		}
		cfg.TokenRetryDelay = d
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("UI_DISABLE")), "true") || strings.TrimSpace(os.Getenv("UI_DISABLE")) == "1" {
		cfg.UIListenAddr = ""
	} else {
		cfg.UIListenAddr = strings.TrimSpace(os.Getenv("UI_LISTEN_ADDR"))
		if cfg.UIListenAddr == "" {
			cfg.UIListenAddr = ":8484"
		}
	}

	envInterval := strings.TrimSpace(os.Getenv("SCHEDULE_INTERVAL"))
	envCron := strings.TrimSpace(os.Getenv("SCHEDULE_CRON"))

	switch {
	case strings.TrimSpace(*cronFlag) != "":
		cfg.CronExpr = strings.TrimSpace(*cronFlag)
	case strings.TrimSpace(*scheduleFlag) != "":
		d, err := time.ParseDuration(strings.TrimSpace(*scheduleFlag))
		if err != nil {
			return nil, fmt.Errorf("invalid -schedule: %w", err)
		}
		if d <= 0 {
			return nil, errors.New("-schedule must be positive")
		}
		cfg.PollInterval = d
	case envCron != "" && envInterval != "":
		return nil, errors.New("set either SCHEDULE_CRON or SCHEDULE_INTERVAL in .env, not both")
	case envCron != "":
		cfg.CronExpr = envCron
	case envInterval != "":
		d, err := time.ParseDuration(envInterval)
		if err != nil {
			return nil, fmt.Errorf("SCHEDULE_INTERVAL: %w", err)
		}
		if d <= 0 {
			return nil, errors.New("SCHEDULE_INTERVAL must be positive")
		}
		cfg.PollInterval = d
	default:
		cfg.PollInterval = 30 * time.Second
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.PostBaseURL == "" {
		return nil, errors.New("POST_BASE_URL is required")
	}
	if cfg.KeycloakClientID == "" || cfg.KeycloakClientSecret == "" {
		return nil, errors.New("KEYCLOAK_CLIENT_ID and KEYCLOAK_CLIENT_SECRET are required")
	}
	if cfg.KeycloakTokenURL == "" {
		if cfg.KeycloakBaseURL == "" || cfg.KeycloakRealm == "" {
			return nil, errors.New("set KEYCLOAK_TOKEN_URL or both KEYCLOAK_BASE_URL and KEYCLOAK_REALM")
		}
		cfg.KeycloakTokenURL = cfg.KeycloakBaseURL + "/realms/" + cfg.KeycloakRealm + "/protocol/openid-connect/token"
	}

	if cfg.CronExpr != "" && cfg.PollInterval > 0 {
		return nil, errors.New("internal: both cron and interval set")
	}

	return cfg, nil
}
