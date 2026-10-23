CREATE TABLE IF NOT EXISTS outbox_messages (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    dedupe_key      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 10,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until    TIMESTAMPTZ,
    locked_by       TEXT,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    CONSTRAINT outbox_messages_status_check CHECK (
        status IN ('pending', 'processing', 'delivered', 'failed', 'dead')
    ),
    CONSTRAINT outbox_messages_attempts_non_negative_check CHECK (attempts >= 0),
    CONSTRAINT outbox_messages_max_attempts_positive_check CHECK (max_attempts > 0),
    CONSTRAINT outbox_messages_dedupe_key_unique UNIQUE (dedupe_key)
);

COMMENT ON TABLE outbox_messages IS 'Durable outbox for post-commit side effects such as queue publishes, restriction counters, notifications, and user blocking';
COMMENT ON COLUMN outbox_messages.aggregate_type IS 'Business aggregate type, e.g. blockchain_tx, deposit_request, withdrawal_request, transfer_request';
COMMENT ON COLUMN outbox_messages.aggregate_id IS 'Business aggregate id (integer or external string identifier such as tx hash)';
COMMENT ON COLUMN outbox_messages.event_type IS 'Outbox effect type, e.g. blockchain.transaction.event, restriction.counter.update, notification.push, user.block';
COMMENT ON COLUMN outbox_messages.dedupe_key IS 'Stable idempotency key for this side effect; duplicate inserts are ignored by the unique constraint';
COMMENT ON COLUMN outbox_messages.payload IS 'JSON payload consumed by the outbox worker handler';
COMMENT ON COLUMN outbox_messages.status IS 'pending, processing, delivered, failed, or dead';
COMMENT ON COLUMN outbox_messages.next_attempt_at IS 'Earliest time the worker may retry this message';
COMMENT ON COLUMN outbox_messages.locked_until IS 'Worker lease expiration used to recover messages after worker crashes';

CREATE INDEX IF NOT EXISTS outbox_messages_ready_idx
    ON outbox_messages(next_attempt_at, id)
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS outbox_messages_processing_expired_idx
    ON outbox_messages(locked_until, id)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS outbox_messages_aggregate_idx
    ON outbox_messages(aggregate_type, aggregate_id, created_at);

CREATE INDEX IF NOT EXISTS outbox_messages_event_status_idx
    ON outbox_messages(event_type, status, created_at);

CREATE INDEX IF NOT EXISTS outbox_messages_dead_idx
    ON outbox_messages(created_at)
    WHERE status = 'dead';

CREATE INDEX IF NOT EXISTS outbox_messages_delivered_gc_idx
    ON outbox_messages(delivered_at)
    WHERE status = 'delivered';
