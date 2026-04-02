package webui

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"strings"
	"time"

	"outbox-dispatcher/internal/config"
	"outbox-dispatcher/internal/logger"
)

// Start launches the dashboard on listenAddr (e.g. ":8484"). Stops when ctx is cancelled.
// Pass empty listenAddr to skip starting the HTTP server.
func Start(ctx context.Context, listenAddr string, db *sql.DB, cfg *config.Config, startedAt time.Time) {
	if strings.TrimSpace(listenAddr) == "" {
		return
	}
	mux := http.NewServeMux()
	h := &handler{db: db, cfg: cfg, startedAt: startedAt}
	mux.HandleFunc("/", h.page)
	mux.HandleFunc("/healthz", h.healthz)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.L.Printf("web dashboard shutdown: %v", err)
		}
	}()
	go func() {
		logger.L.Printf("web dashboard listening on %s", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L.Printf("web dashboard: %v", err)
		}
	}()
}

type handler struct {
	db        *sql.DB
	cfg       *config.Config
	startedAt time.Time
}

func (h *handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := pingDB(r.Context(), h.db); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unhealthy: db: " + err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *handler) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	pingErr := pingDB(ctx, h.db)
	pingOK := pingErr == nil
	var dbErr string
	if pingErr != nil {
		dbErr = "БД: " + pingErr.Error()
	}
	stats, total, errStats := loadStats(ctx, h.db, h.cfg.OutboxTable)
	if errStats != nil {
		if dbErr != "" {
			dbErr += "; "
		}
		dbErr += "статистика: " + errStats.Error()
		stats, total = nil, 0
	}
	errRows, errErr := loadRecentErrors(ctx, h.db, h.cfg.OutboxTable, 50)
	if errErr != nil {
		if dbErr != "" {
			dbErr += "; "
		}
		dbErr += "список ошибок: " + errErr.Error()
		errRows = nil
	}
	pageOK := pingOK && errStats == nil && errErr == nil

	data := struct {
		Uptime        string
		DBOK          bool
		DBErr         string
		Scheduler     string
		Table         string
		PostBase      string
		KeycloakRealm string
		Stats         []StatusRow
		Total         int64
		Errors        []ErrorRow
		GeneratedAt   string
	}{
		Uptime:        formatSince(h.startedAt),
		DBOK:          pageOK,
		DBErr:         dbErr,
		Scheduler:     SchedulerSummary(h.cfg),
		Table:         h.cfg.OutboxTable,
		PostBase:      h.cfg.PostBaseURL,
		KeycloakRealm: h.cfg.KeycloakRealm,
		Stats:         stats,
		Total:         total,
		Errors:        errRows,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTpl.Execute(w, data); err != nil {
		logger.L.Printf("web dashboard template: %v", err)
	}
}

var dashboardTpl = template.Must(template.New("dash").Parse(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>outbox-dispatcher</title>
<style>
:root { --bg:#0f1419; --card:#1a2332; --text:#e7e9ea; --muted:#8b98a5; --accent:#1d9bf0; --err:#f4212e; --ok:#00ba7c; }
* { box-sizing: border-box; }
body { font-family: system-ui, Segoe UI, Roboto, sans-serif; background: var(--bg); color: var(--text); margin: 0; padding: 1rem 1.25rem 2rem; line-height: 1.45; }
h1 { font-size: 1.25rem; font-weight: 600; margin: 0 0 1rem; }
h2 { font-size: 0.95rem; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; margin: 1.5rem 0 0.6rem; }
.grid { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); }
.card { background: var(--card); border-radius: 8px; padding: 0.85rem 1rem; }
.card strong { display: block; font-size: 0.75rem; color: var(--muted); font-weight: 500; margin-bottom: 0.25rem; }
.badge { display: inline-block; padding: 0.15rem 0.45rem; border-radius: 4px; font-size: 0.8rem; }
.badge.ok { background: rgba(0,186,124,.15); color: var(--ok); }
.badge.bad { background: rgba(244,33,46,.15); color: var(--err); }
table { width: 100%; border-collapse: collapse; font-size: 0.85rem; background: var(--card); border-radius: 8px; overflow: hidden; }
th, td { text-align: left; padding: 0.5rem 0.65rem; border-bottom: 1px solid rgba(255,255,255,.06); vertical-align: top; }
th { color: var(--muted); font-weight: 600; font-size: 0.72rem; text-transform: uppercase; }
tr:last-child td { border-bottom: none; }
.mono { font-family: ui-monospace, Consolas, monospace; font-size: 0.8rem; word-break: break-word; }
.footer { margin-top: 1.25rem; font-size: 0.8rem; color: var(--muted); }
a { color: var(--accent); }
.empty { color: var(--muted); padding: 0.75rem; }
</style>
</head>
<body>
<h1>outbox-dispatcher</h1>

<div class="grid">
  <div class="card"><strong>Статус</strong>
    {{if .DBOK}}<span class="badge ok">работает</span>{{else}}<span class="badge bad">проблема</span>{{end}}
  </div>
  <div class="card"><strong>Время работы</strong>{{.Uptime}}</div>
  <div class="card"><strong>Планировщик</strong><span class="mono">{{.Scheduler}}</span></div>
  <div class="card"><strong>Таблица</strong><span class="mono">{{.Table}}</span></div>
  <div class="card"><strong>POST_BASE_URL</strong><span class="mono">{{.PostBase}}</span></div>
  <div class="card"><strong>Realm (Keycloak)</strong>{{if .KeycloakRealm}}<span class="mono">{{.KeycloakRealm}}</span>{{else}}<span class="muted">—</span>{{end}}</div>
</div>

{{if .DBErr}}
<p style="color:var(--err); font-size:0.9rem;">{{.DBErr}}</p>
{{end}}

<h2>Статистика по статусам</h2>
{{if .Stats}}
<p style="margin:0 0 0.5rem; font-size:0.9rem;">Всего записей: <strong>{{.Total}}</strong></p>
<table>
  <thead><tr><th>Статус</th><th>Количество</th></tr></thead>
  <tbody>
  {{range .Stats}}
    <tr><td class="mono">{{.Status}}</td><td>{{.Count}}</td></tr>
  {{end}}
  </tbody>
</table>
{{else}}
<p class="empty">Нет данных (или ошибка запроса).</p>
{{end}}

<h2>Последние ошибки</h2>
{{if .Errors}}
<table>
  <thead>
    <tr>
      <th>ID</th>
      <th>message_id</th>
      <th>Тип / aggregate</th>
      <th>Статус</th>
      <th>Повторы</th>
      <th>Ошибка</th>
      <th>След. повтор</th>
    </tr>
  </thead>
  <tbody>
  {{range .Errors}}
    <tr>
      <td class="mono">{{.ID}}</td>
      <td class="mono">{{.MessageID}}</td>
      <td class="mono">{{if .MessageType}}{{.MessageType}}{{else}}—{{end}}<br><span style="color:var(--muted)">{{.AggregateID}}</span></td>
      <td class="mono">{{.Status}}</td>
      <td>{{.RetryCount}}</td>
      <td class="mono">{{.ErrorDetails}}</td>
      <td class="mono">{{if .NextRetry.Valid}}{{.NextRetry.Time.UTC.Format "2006-01-02 15:04:05"}}{{else}}—{{end}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{else}}
<p class="empty">Записей с ошибками не найдено.</p>
{{end}}

<p class="footer">Обновление страницы каждые 30 с · сгенерировано {{.GeneratedAt}} UTC · <a href="/healthz">/healthz</a></p>
</body>
</html>
`))
