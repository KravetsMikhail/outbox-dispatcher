# outbox-dispatcher

Сервис **outbox-dispatcher** периодически читает таблицу `outbox_messages` в PostgreSQL, выбирает записи в статусе `pending` (с учётом `next_retry_date`), отправляет их в виде **HTTP POST** на целевой API и обновляет статусы. Целевой путь и имя ресурса задаются в поле `metadata` (JSON), базовый URL — в переменной окружения. Для вызовов API используется **Bearer-токен** из **Keycloak** (grant `client_credentials`).

Дополнительно есть **веб-дашборд** (по умолчанию порт `8484`): статус процесса, статистика по статусам сообщений и последние ошибки.

Подробнее на английском: **[README in English](README.en.md)**.

## Быстрый старт

1. Скопируйте `.env.example` в `.env` и примените миграцию из `migrations/001_outbox.sql`.
2. Соберите и запустите: `go build ./cmd/outbox-dispatcher` (бинарник `outbox-dispatcher`, в Windows — `outbox-dispatcher.exe`) или используйте `Dockerfile`.

## Конфигурация

Основные переменные: `DATABASE_URL`, `POST_BASE_URL`, параметры Keycloak (`KEYCLOAK_*` или `KEYCLOAK_TOKEN_URL`), расписание (`SCHEDULE_INTERVAL` или `SCHEDULE_CRON`), повторы (`MAX_RETRY_ATTEMPTS`, `RETRY_BASE_INTERVAL`, `TOKEN_RETRY_DELAY`), веб-интерфейс (`UI_LISTEN_ADDR`, `UI_DISABLE`). См. `.env.example`.

Файл с переменными по умолчанию — `.env` в текущей директории (если файла нет, он просто пропускается). Другой файл можно указать при запуске: `-env=path/to/.env` или `-env path/to/.env` — в этом случае файл должен существовать.

## Лицензия

См. файл [LICENSE](LICENSE).
