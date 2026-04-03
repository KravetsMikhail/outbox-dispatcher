# outbox-dispatcher

**outbox-dispatcher** is a PostgreSQL **outbox** worker that periodically reads the `outbox_messages` table, picks rows in `pending` status (respecting `next_retry_date`), sends each payload as an **HTTP POST** to your API, and updates row statuses. The target path is taken from the `metadata` JSON field; the base URL is set via environment variables. API calls use a **Bearer token** obtained from **Keycloak** with the **client credentials** grant.

A small **web dashboard** (default port **8484**) shows process health, per-status counts, and recent errors.

**[Russian README](README.md)** — Russian version.

## Quick start

1. Copy `.env.example` to `.env` and apply the migration in `migrations/001_outbox.sql`.
2. Build and run: `go build ./cmd/outbox-dispatcher` (binary `outbox-dispatcher`, on Windows `outbox-dispatcher.exe`) or use the `Dockerfile`.

## Configuration

Main variables: `DATABASE_URL`, `POST_BASE_URL`, Keycloak settings (`KEYCLOAK_*` or `KEYCLOAK_TOKEN_URL`), schedule (`SCHEDULE_INTERVAL` or `SCHEDULE_CRON`), retries (`MAX_RETRY_ATTEMPTS`, `RETRY_BASE_INTERVAL`, `TOKEN_RETRY_DELAY`), and the web UI (`UI_LISTEN_ADDR`, `UI_DISABLE`). See `.env.example`.

By default the process loads `.env` in the working directory (a missing file is ignored). To use another file, pass `-env=path/to/.env` or `-env path/to/.env` — that file must exist.

## License

See [LICENSE](LICENSE).
