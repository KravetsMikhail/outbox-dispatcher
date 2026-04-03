package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"outbox-dispatcher/internal/logger"
)

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusFailed     = "failed"
)

const errDetailsMaxLen = 8000

// ProcessOptions configures retry behaviour for ProcessPending.
type ProcessOptions struct {
	MaxRetryAttempts  int           // after this many failures (retry_count), row becomes failed; 0 = unlimited
	RetryBaseInterval time.Duration // base for exponential backoff between HTTP retries
	TokenRetryDelay   time.Duration // delay before retry when token is missing / Keycloak error (0 = eligible on next poll)
	// StaleProcessingRecovery resets status=processing rows whose updated_at is older than this; 0 = disabled.
	StaleProcessingRecovery time.Duration
	// VerbosePoll logs when no rows are claimed (explains next_retry_date / processing).
	VerbosePoll bool
}

type Row struct {
	ID         int64
	Payload    []byte
	Metadata   sql.NullString
	RetryCount int
}

type TokenGetter interface {
	BearerToken(ctx context.Context) (string, error)
}

// VerifyTable checks that qualifiedTable exists and is readable (SELECT COUNT(*)).
// qualifiedTable must be a PostgreSQL-qualified identifier, e.g. "public"."outbox_messages".
func VerifyTable(ctx context.Context, db *sql.DB, qualifiedTable string) error {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, qualifiedTable)
	var n int64
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return fmt.Errorf("%s: %w", qualifiedTable, err)
	}
	return nil
}

// ProcessPending claims pending rows (processing), POSTs payload, updates status.
// qualifiedTable must be a PostgreSQL-qualified table name, e.g. "public"."outbox_messages" (see pqname.QualifiedTable).
func ProcessPending(ctx context.Context, db *sql.DB, qualifiedTable, baseURL string, tok TokenGetter, client *http.Client, opts ProcessOptions) error {
	if client == nil {
		client = http.DefaultClient
	}
	nRecovered, err := recoverStaleProcessing(ctx, db, qualifiedTable, opts.StaleProcessingRecovery)
	if err != nil {
		return fmt.Errorf("recover stale processing: %w", err)
	}
	if nRecovered > 0 {
		logger.L.Printf("outbox: reset %d stale processing row(s) (OUTBOX_STALE_PROCESSING_AFTER)", nRecovered)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Use PostgreSQL NOW() for eligibility and updated_at so scheduling matches the DB clock
	// (avoids app↔DB clock skew and timezone surprises vs passing Go time as parameters).
	q := fmt.Sprintf(`
		UPDATE %s AS o
		SET status = $1, updated_at = NOW()
		FROM (
			SELECT id FROM %s
			WHERE status = $2
			  AND (next_retry_date IS NULL OR next_retry_date <= NOW())
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT 50
		) AS sub
		WHERE o.id = sub.id
		RETURNING o.id, o.payload, o.metadata, o.retry_count
	`, qualifiedTable, qualifiedTable)

	rows, err := tx.QueryContext(ctx, q, StatusProcessing, StatusPending)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.Payload, &r.Metadata, &r.RetryCount); err != nil {
			return err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if opts.VerbosePoll {
		if len(list) == 0 {
			logger.L.Printf("process pending: claimed 0 rows — eligible only if status=pending AND (next_retry_date IS NULL OR next_retry_date <= NOW() in DB); after Keycloak errors rows wait TOKEN_RETRY_DELAY; crash mid-dispatch leaves status=processing (use OUTBOX_STALE_PROCESSING_AFTER)")
		} else {
			logger.L.Printf("process pending: claimed %d row(s)", len(list))
		}
	}

	for _, r := range list {
		if err := dispatchOne(ctx, db, qualifiedTable, baseURL, tok, client, r, opts); err != nil {
			logger.L.Printf("row id=%d: %v", r.ID, err)
		}
	}
	return nil
}

func dispatchOne(ctx context.Context, db *sql.DB, qualifiedTable, baseURL string, tok TokenGetter, client *http.Client, r Row, opts ProcessOptions) error {
	target, err := buildTargetURL(baseURL, r.Metadata)
	if err != nil {
		return markFailed(ctx, db, qualifiedTable, r.ID, truncateErr(err.Error(), errDetailsMaxLen))
	}
	var t string
	if tok != nil {
		var err error
		t, err = tok.BearerToken(ctx)
		if err != nil {
			return requeueTokenIssue(ctx, db, qualifiedTable, r.ID, fmt.Sprintf("keycloak: %v", err), opts.TokenRetryDelay)
		}
	}
	if strings.TrimSpace(t) == "" {
		return requeueTokenIssue(ctx, db, qualifiedTable, r.ID, "no bearer token", opts.TokenRetryDelay)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(r.Payload)))
	if err != nil {
		return markFailed(ctx, db, qualifiedTable, r.ID, truncateErr(err.Error(), errDetailsMaxLen))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t)

	resp, err := client.Do(req)
	if err != nil {
		return recordHTTPFailure(ctx, db, qualifiedTable, r, opts, fmt.Sprintf("post: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := markSent(ctx, db, qualifiedTable, r.ID); err != nil {
			return err
		}
		logger.L.Printf("row id=%d: sent %s -> %d", r.ID, target, resp.StatusCode)
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := fmt.Sprintf("http %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	return recordHTTPFailure(ctx, db, qualifiedTable, r, opts, msg)
}

func markSent(ctx context.Context, db *sql.DB, qualifiedTable string, id int64) error {
	q := fmt.Sprintf(`
		UPDATE %s
		SET status = $1, error_details = NULL, next_retry_date = NULL, updated_at = NOW()
		WHERE id = $2
	`, qualifiedTable)
	_, err := db.ExecContext(ctx, q, StatusSent, id)
	return err
}

func markFailed(ctx context.Context, db *sql.DB, qualifiedTable string, id int64, details string) error {
	q := fmt.Sprintf(`
		UPDATE %s
		SET status = $1, error_details = $2, next_retry_date = NULL, updated_at = NOW()
		WHERE id = $3
	`, qualifiedTable)
	_, err := db.ExecContext(ctx, q, StatusFailed, truncateErr(details, errDetailsMaxLen), id)
	return err
}

func requeueTokenIssue(ctx context.Context, db *sql.DB, qualifiedTable string, id int64, details string, delay time.Duration) error {
	// next_retry_date = NOW() + delay using DB clock (matches claim predicate next_retry_date <= NOW())
	q := fmt.Sprintf(`
		UPDATE %s
		SET status = $1, error_details = $2,
		    next_retry_date = NOW() + ($3 * INTERVAL '1 second'), updated_at = NOW()
		WHERE id = $4
		RETURNING next_retry_date
	`, qualifiedTable)
	var next sql.NullTime
	err := db.QueryRowContext(ctx, q,
		StatusPending,
		truncateErr(details, errDetailsMaxLen),
		delay.Seconds(),
		id,
	).Scan(&next)
	if err != nil {
		return err
	}
	nextStr := "?"
	if next.Valid {
		nextStr = next.Time.UTC().Format(time.RFC3339)
	}
	logger.L.Printf("row id=%d: back to pending (token issue), next_retry_date=%s: %s",
		id, nextStr, truncateErr(details, 500))
	return nil
}

func recoverStaleProcessing(ctx context.Context, db *sql.DB, qualifiedTable string, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	q := fmt.Sprintf(`
		UPDATE %s
		SET status = $1, updated_at = NOW(), next_retry_date = NULL
		WHERE status = $2
		  AND (updated_at < NOW() - ($3 * INTERVAL '1 second') OR updated_at IS NULL)
	`, qualifiedTable)
	res, err := db.ExecContext(ctx, q, StatusPending, StatusProcessing, olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func recordHTTPFailure(ctx context.Context, db *sql.DB, qualifiedTable string, r Row, opts ProcessOptions, details string) error {
	details = truncateErr(details, errDetailsMaxLen)
	newCount := r.RetryCount + 1
	if opts.MaxRetryAttempts > 0 && newCount > opts.MaxRetryAttempts {
		q := fmt.Sprintf(`
			UPDATE %s
			SET status = $1, retry_count = $2, error_details = $3, next_retry_date = NULL, updated_at = NOW()
			WHERE id = $4
		`, qualifiedTable)
		_, err := db.ExecContext(ctx, q, StatusFailed, newCount, details, r.ID)
		if err != nil {
			return err
		}
		logger.L.Printf("row id=%d: failed after %d attempt(s): %s", r.ID, newCount, details)
		return nil
	}
	backoff := retryBackoff(newCount, opts.RetryBaseInterval)
	q := fmt.Sprintf(`
		UPDATE %s
		SET status = $1, retry_count = $2, error_details = $3,
		    next_retry_date = NOW() + ($4 * INTERVAL '1 second'), updated_at = NOW()
		WHERE id = $5
		RETURNING next_retry_date
	`, qualifiedTable)
	var next sql.NullTime
	err := db.QueryRowContext(ctx, q, StatusPending, newCount, details, backoff.Seconds(), r.ID).Scan(&next)
	if err != nil {
		return err
	}
	nextStr := "?"
	if next.Valid {
		nextStr = next.Time.UTC().Format(time.RFC3339)
	}
	logger.L.Printf("row id=%d: retry %d scheduled at %s: %s", r.ID, newCount, nextStr, details)
	return nil
}

func retryBackoff(retryCount int, base time.Duration) time.Duration {
	if base <= 0 {
		base = 30 * time.Second
	}
	// exponential: base * 2^(retryCount-1), capped at 1h
	exp := 1
	for i := 1; i < retryCount && i < 12; i++ {
		exp *= 2
		if exp > 65536 {
			break
		}
	}
	d := base * time.Duration(exp)
	if d < 5*time.Second {
		d = 5 * time.Second
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

func truncateErr(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func buildTargetURL(baseURL string, meta sql.NullString) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid POST_BASE_URL")
	}
	base := strings.TrimRight(u.String(), "/")
	if !meta.Valid || strings.TrimSpace(meta.String) == "" {
		return "", fmt.Errorf("metadata is required")
	}
	var m struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(meta.String), &m); err != nil {
		return "", fmt.Errorf("metadata json: %w", err)
	}
	var path string
	switch {
	case strings.TrimSpace(m.Path) != "":
		path = strings.TrimSpace(m.Path)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	case strings.TrimSpace(m.Name) != "":
		path = "/" + strings.Trim(strings.TrimSpace(m.Name), "/")
	default:
		return "", fmt.Errorf("metadata must contain \"name\" or \"path\"")
	}
	return base + path, nil
}
