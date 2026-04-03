package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/robfig/cron/v3"

	"outbox-dispatcher/internal/appstatus"
	"outbox-dispatcher/internal/config"
	"outbox-dispatcher/internal/logger"
	"outbox-dispatcher/internal/outbox"
	"outbox-dispatcher/internal/token"
	"outbox-dispatcher/internal/webui"
)

func main() {
	logger.Init("[outbox-dispatcher]")

	cfg, err := config.Load()
	if err != nil {
		logger.L.Fatalf("config: %v", err)
	}
	if cfg.KeycloakTLSInsecure {
		logger.L.Printf("warning: KEYCLOAK_TLS_INSECURE_SKIP_VERIFY enabled (Keycloak token requests only; do not use in production)")
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.L.Fatalf("db open: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		logger.L.Fatalf("db ping: %v", err)
	}

	apiHTTP := &http.Client{Timeout: 60 * time.Second}
	kcHTTP := keycloakHTTPClient(cfg)
	kc := token.NewKeycloak(kcHTTP, cfg.KeycloakTokenURL, cfg.KeycloakClientID, cfg.KeycloakClientSecret, cfg.KeycloakScope, cfg.KeycloakUserAgent)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startedAt := time.Now()
	webui.Start(ctx, cfg.UIListenAddr, db, cfg, startedAt)

	run := func() {
		cctx, ccancel := context.WithTimeout(ctx, 5*time.Minute)
		defer ccancel()
		opts := outbox.ProcessOptions{
			MaxRetryAttempts:          cfg.MaxRetryAttempts,
			RetryBaseInterval:         cfg.RetryBaseInterval,
			TokenRetryDelay:           cfg.TokenRetryDelay,
			RetryMaxBackoff:           cfg.RetryMaxBackoff,
			StaleProcessingRecovery:   cfg.StaleProcessingRecovery,
			VerbosePoll:               cfg.PollLog,
		}
		if err := outbox.ProcessPending(cctx, db, cfg.QualifiedOutboxTable(), cfg.PostBaseURL, kc, apiHTTP, opts); err != nil {
			appstatus.SetProcessError(err)
			logger.L.Printf("process pending: %v", err)
		} else {
			appstatus.SetProcessError(nil)
		}
	}

	if cfg.CronExpr != "" {
		c := cron.New()
		if _, err := c.AddFunc(cfg.CronExpr, run); err != nil {
			logger.L.Fatalf("cron: %v", err)
		}
		run()
		c.Start()
		logger.L.Printf("scheduler: cron %q", cfg.CronExpr)
		defer c.Stop()
	} else {
		logger.L.Printf("scheduler: interval %s", cfg.PollInterval)
		if !cfg.PollLog {
			logger.L.Printf("poll: quiet when outbox has no pending rows (no Keycloak call); set POLL_LOG=1 to log each cycle")
		}
		t := time.NewTicker(cfg.PollInterval)
		defer t.Stop()
		go func() {
			for {
				if cfg.PollLog {
					logger.L.Printf("poll: cycle started")
				}
				t0 := time.Now()
				run()
				if cfg.PollLog {
					logger.L.Printf("poll: cycle finished in %v", time.Since(t0))
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.L.Printf("shutdown")
	cancel()
}

// keycloakHTTPClient is only for the token endpoint (TLS options do not affect POST_BASE_URL).
func keycloakHTTPClient(cfg *config.Config) *http.Client {
	c := &http.Client{Timeout: 60 * time.Second}
	if !cfg.KeycloakTLSInsecure {
		return c
	}
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return c
	}
	t2 := tr.Clone()
	if t2.TLSClientConfig == nil {
		t2.TLSClientConfig = &tls.Config{}
	} else {
		t2.TLSClientConfig = t2.TLSClientConfig.Clone()
	}
	t2.TLSClientConfig.InsecureSkipVerify = true
	c.Transport = t2
	return c
}
