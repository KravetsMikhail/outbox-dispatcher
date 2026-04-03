package main

import (
	"context"
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

	httpClient := &http.Client{Timeout: 60 * time.Second}
	kc := token.NewKeycloak(httpClient, cfg.KeycloakTokenURL, cfg.KeycloakClientID, cfg.KeycloakClientSecret, cfg.KeycloakScope)

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
			StaleProcessingRecovery:   cfg.StaleProcessingRecovery,
			VerbosePoll:               cfg.PollLog,
		}
		if err := outbox.ProcessPending(cctx, db, cfg.QualifiedOutboxTable(), cfg.PostBaseURL, kc, httpClient, opts); err != nil {
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
