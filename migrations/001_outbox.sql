-- Outbox messages (PostgreSQL)
CREATE TABLE IF NOT EXISTS outbox_messages (
    id BIGSERIAL PRIMARY KEY,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    aggregate_id TEXT,
    message_id UUID NOT NULL DEFAULT gen_random_uuid(),
    message_type VARCHAR(255),
    payload JSONB NOT NULL,
    metadata JSONB,
    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_date TIMESTAMPTZ,
    error_details TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_messages_poll
    ON outbox_messages (status, next_retry_date, id);

-- metadata example: {"name": "orders/create"}  -> POST {POST_BASE_URL}/orders/create
-- or {"path": "/v1/orders"} -> POST {POST_BASE_URL}/v1/orders
