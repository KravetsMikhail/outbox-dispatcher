# outbox-dispatcher

**outbox-dispatcher** is a PostgreSQL **outbox** worker that periodically reads the `outbox_messages` table, picks rows in `pending` status (respecting `next_retry_date`), sends each payload as an **HTTP POST** to your API, and updates row statuses. The target path is taken from the `metadata` JSON field; the base URL is set via environment variables. API calls use a **Bearer token** obtained from **Keycloak** with the **client credentials** grant.

A small **web dashboard** (default port **8484**) shows process health, per-status counts, and recent errors.

**[Russian README](README.md)** — Russian version.

## Quick start

1. Copy `.env.example` to `.env` and apply the migration in `migrations/001_outbox.sql`.
2. Build and run: `go build ./cmd/outbox-dispatcher` (binary `outbox-dispatcher`, on Windows `outbox-dispatcher.exe`) or use the `Dockerfile`.

### Docker and a custom `.env`

The image **does not embed** an `.env` file — you supply configuration when starting the container.

**Option 1 — inject variables from a file** (handy for `.env.prod`):

```bash
docker build -t outbox-dispatcher .
docker run --rm -p 8484:8484 --env-file .env.prod outbox-dispatcher
```

Docker injects key/value pairs into the process; no file has to exist inside the image.

**Option 2 — mount a file and pass `-env`** (loaded via `godotenv`, same as locally):

```bash
docker run --rm -p 8484:8484 \
  -v /absolute/path/.env.prod:/app/.env.prod:ro \
  outbox-dispatcher -env=/app/.env.prod
```

The working directory in the image is `/app` (see `Dockerfile`).

## Configuration

Main variables: `DATABASE_URL`, `POST_BASE_URL`, Keycloak settings (`KEYCLOAK_*` or `KEYCLOAK_TOKEN_URL`), schedule (`SCHEDULE_INTERVAL` or `SCHEDULE_CRON`), retries (`MAX_RETRY_ATTEMPTS`, `RETRY_BASE_INTERVAL`, `TOKEN_RETRY_DELAY`), and the web UI (`UI_LISTEN_ADDR`, `UI_DISABLE`). See `.env.example`.

By default the process loads `.env` in the working directory (a missing file is ignored). To use another file, pass `-env=path/to/.env` or `-env path/to/.env` — that file must exist.

## The `metadata` column in `outbox_messages`

The **`metadata`** column is **required** for a successful delivery: it must contain **valid JSON** that defines the HTTP path via **`name`** and/or **`path`** (if both are set, **`path`** wins):

| Field | Purpose |
|-------|---------|
| **`name`** | Relative path appended to `POST_BASE_URL` (slashes inside the value are allowed). Example: `"orders/create"` → path `/orders/create`. |
| **`path`** | Absolute path from the host root; should start with `/` (a leading slash is added if missing). Example: `"/v1/events"`. |

The final URL is **`POST_BASE_URL`** (trailing slash trimmed) **+** the path from `metadata`. The **POST** body comes from the **`payload`** column (JSON).

Example `metadata` values:

```json
{"name": "orders/create"}
```

With `POST_BASE_URL=https://api.example.com` the request goes to `https://api.example.com/orders/create`.

```json
{"path": "/v1/notifications"}
```

With `POST_BASE_URL=https://api.example.com` the request goes to `https://api.example.com/v1/notifications`. If `POST_BASE_URL` already includes a path prefix (e.g. `https://api.example.com/api`), `path` or `name` is appended as described above.

If neither `name` nor `path` is set, processing fails and the row ends in **`failed`** (invalid `metadata`).

## License

See [LICENSE](LICENSE).
