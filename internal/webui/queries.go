package webui

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"outbox-dispatcher/internal/config"
)

type StatusRow struct {
	Status string
	Count  int64
}

type ErrorRow struct {
	ID           int64
	AggregateID  string
	MessageID    string
	MessageType  string
	Status       string
	RetryCount   int
	ErrorDetails string
	NextRetry    sql.NullTime
	UpdatedAt    sql.NullTime
}

func pingDB(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}

func loadStats(ctx context.Context, db *sql.DB, table string) ([]StatusRow, int64, error) {
	q := fmt.Sprintf(`SELECT status, COUNT(*) FROM %s GROUP BY status ORDER BY status`, quoteIdent(table))
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []StatusRow
	var total int64
	for rows.Next() {
		var s StatusRow
		if err := rows.Scan(&s.Status, &s.Count); err != nil {
			return nil, 0, err
		}
		out = append(out, s)
		total += s.Count
	}
	return out, total, rows.Err()
}

func loadRecentErrors(ctx context.Context, db *sql.DB, table string, limit int) ([]ErrorRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := fmt.Sprintf(`
		SELECT id,
		       COALESCE(aggregate_id, ''),
		       message_id::text,
		       COALESCE(message_type, ''),
		       status,
		       retry_count,
		       COALESCE(error_details, ''),
		       next_retry_date,
		       updated_at
		FROM %s
		WHERE (error_details IS NOT NULL AND TRIM(error_details) <> '')
		   OR status = $1
		ORDER BY COALESCE(updated_at, created_at) DESC NULLS LAST
		LIMIT $2
	`, quoteIdent(table))
	rows, err := db.QueryContext(ctx, q, "failed", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErrorRow
	for rows.Next() {
		var e ErrorRow
		if err := rows.Scan(
			&e.ID,
			&e.AggregateID,
			&e.MessageID,
			&e.MessageType,
			&e.Status,
			&e.RetryCount,
			&e.ErrorDetails,
			&e.NextRetry,
			&e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SchedulerSummary returns a short human-readable scheduler description.
func SchedulerSummary(cfg *config.Config) string {
	if cfg.CronExpr != "" {
		return "cron " + cfg.CronExpr
	}
	return "interval " + cfg.PollInterval.String()
}

func formatSince(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}
