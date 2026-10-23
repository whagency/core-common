-- Dead-man's-switch guarding account-service ledger postings.
--
-- A row is written OUTSIDE the business transaction, just before and just after
-- the ledger call, and cleared INSIDE it. If the business transaction rolls back,
-- the clearing rolls back with it and the row survives — that surviving row is
-- the record that money moved with nothing to show for it, and the resolver
-- reverses the posting.
--
-- It cannot be a column on the owning aggregate's table: the business transaction
-- typically holds SELECT ... FOR UPDATE on that row, so an autocommit write to it
-- from a second connection would block on a lock the same request already holds.
CREATE TABLE IF NOT EXISTS ledger_compensations (
    id BIGINT generated always as identity PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    biz_type SMALLINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    journal_id BIGINT,
    status SMALLINT NOT NULL,
    resolved_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE ledger_compensations IS 'Dead-man''s-switch guarding account-service ledger postings. A row surviving past its grace period means the posting has no committed local counterpart and must be reversed. Schema owned by the shared common module.';
COMMENT ON COLUMN ledger_compensations.aggregate_type IS 'Owning local aggregate kind, e.g. pay_order. Opaque to the shared package; handed back to the service Verifier unchanged.';
COMMENT ON COLUMN ledger_compensations.aggregate_id IS 'Owning local aggregate id. Deliberately TEXT: the shared table cannot know whether a service keys on UUID or BIGINT.';
COMMENT ON COLUMN ledger_compensations.biz_type IS 'JournalBizType passed to MakeTransaction. Needed to look the journal up — account-service derives the stored key from type and key together — and used as the reversal metric label.';
COMMENT ON COLUMN ledger_compensations.idempotency_key IS 'Key passed to MakeTransaction. Resolves to the journal when journal_id was never recorded.';
COMMENT ON COLUMN ledger_compensations.journal_id IS 'NULL while status is pending: the ledger call has not returned yet.';
COMMENT ON COLUMN ledger_compensations.status IS '1=pending, 2=posted, 3=settled, 4=void, 5=reversal_requested.';
COMMENT ON COLUMN ledger_compensations.resolved_reason IS 'Why the resolver closed the row. Operator-facing; not read by code.';

-- One row per ledger call. Service keys are derived from the aggregate, so a
-- retried business flow reuses the row rather than adding a second switch for the
-- same posting.
CREATE UNIQUE INDEX IF NOT EXISTS ledger_comp_idem_key_uq ON ledger_compensations(idempotency_key);

-- The resolver only ever scans unresolved rows. The partial index keeps it
-- proportional to the number of open switches, not to total transaction volume.
CREATE INDEX IF NOT EXISTS ledger_comp_open_idx
    ON ledger_compensations(status, updated_at)
    WHERE status IN (1, 2); -- pending, posted

CREATE INDEX IF NOT EXISTS ledger_comp_aggregate_idx ON ledger_compensations(aggregate_type, aggregate_id);
