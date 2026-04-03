# outbox-dispatcher

Сервис **outbox-dispatcher** периодически читает таблицу `outbox_messages` в PostgreSQL, выбирает записи в статусе `pending` (с учётом `next_retry_date`), отправляет их в виде **HTTP POST** на целевой API и обновляет статусы. Целевой путь и имя ресурса задаются в поле `metadata` (JSON), базовый URL — в переменной окружения. Для вызовов API используется **Bearer-токен** из **Keycloak** (grant `client_credentials`).

Дополнительно есть **веб-дашборд** (по умолчанию порт `8484`): статус процесса, статистика по статусам сообщений и последние ошибки.

Подробнее на английском: **[README in English](README.en.md)**.

## Быстрый старт

1. Скопируйте `.env.example` в `.env` и примените миграцию из `migrations/001_outbox.sql`.
2. Соберите и запустите: `go build ./cmd/outbox-dispatcher` (бинарник `outbox-dispatcher`, в Windows — `outbox-dispatcher.exe`) или используйте `Dockerfile`.

### Docker и кастомный `.env`

Образ сам по себе **не кладёт** файл `.env` — переменные задаются при запуске контейнера.

**Вариант 1 — подставить переменные из файла** (удобно для `.env.prod`):

```bash
docker build -t outbox-dispatcher .
docker run --rm -p 8484:8484 --env-file .env.prod outbox-dispatcher
```

Docker передаёт пары ключ/значение в процесс; отдельный файл внутри образа не нужен.

**Вариант 2 — смонтировать файл и указать флаг `-env`** (читает файл через `godotenv`, как локально):

```bash
docker run --rm -p 8484:8484 \
  -v /path/to/.env.prod:/app/.env.prod:ro \
  outbox-dispatcher -env=/app/.env.prod
```

Рабочая директория в образе — `/app` (см. `Dockerfile`).

## Конфигурация

Основные переменные: `DATABASE_URL`, `POST_BASE_URL`, схема и таблица outbox (`OUTBOX_SCHEMA`, по умолчанию `public`, и `OUTBOX_TABLE`, по умолчанию `outbox_messages`), параметры Keycloak (`KEYCLOAK_*` или `KEYCLOAK_TOKEN_URL`), расписание (`SCHEDULE_INTERVAL` или `SCHEDULE_CRON`), повторы (`MAX_RETRY_ATTEMPTS`, `RETRY_BASE_INTERVAL`, `TOKEN_RETRY_DELAY`), веб-интерфейс (`UI_LISTEN_ADDR`, `UI_DISABLE`). См. `.env.example`.

Файл с переменными по умолчанию — `.env` в текущей директории (если файла нет, он просто пропускается). Другой файл можно указать при запуске: `-env=path/to/.env` или `-env path/to/.env` — в этом случае файл должен существовать.

## Поле `metadata` в `outbox_messages`

Колонка **`metadata`** обязательна для успешной отправки: в ней должен лежать **валидный JSON** с указанием пути запроса через **`name`** и/или **`path`** (если заданы оба, используется **`path`**):

| Поле | Назначение |
|------|------------|
| **`name`** | Относительный путь: добавляется к `POST_BASE_URL` одним сегментом (слэши в значении допускаются). Пример: `"orders/create"` → путь `/orders/create`. |
| **`path`** | Абсолютный путь от корня хоста: должен начинаться с `/` (если не указан — будет добавлен). Пример: `"/v1/events"`. |

Итоговый URL: **`POST_BASE_URL`** (без завершающего `/`) **+** путь из `metadata`. Тело **POST** берётся из колонки **`payload`** (JSON).

Примеры `metadata`:

```json
{"name": "orders/create"}
```

При `POST_BASE_URL=https://api.example.com` запрос уйдёт на `https://api.example.com/orders/create`.

```json
{"path": "/v1/notifications"}
```

При `POST_BASE_URL=https://api.example.com` — на `https://api.example.com/v1/notifications`. Если в `POST_BASE_URL` уже есть префикс пути (например `https://api.example.com/api`), к нему дописывается `path` или `name` как описано выше.

Без `name` и без `path` строка при обработке получит ошибку и уйдёт в **`failed`** (некорректные `metadata`).

## Лицензия

См. файл [LICENSE](LICENSE).
