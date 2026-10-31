CREATE TABLE documents (
    id         UUID PRIMARY KEY,
    title      TEXT        NOT NULL DEFAULT 'Untitled',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Append-only log of every operation: the source of truth for document state.
-- seq is assigned per document by the sync service; op_id makes retries safe.
CREATE TABLE operations (
    doc_id     UUID        NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    seq        BIGINT      NOT NULL,
    op_id      TEXT        NOT NULL,
    payload    JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (doc_id, seq),
    UNIQUE (doc_id, op_id)
);

-- CRDT state as of seq, for fast loading and version history.
CREATE TABLE snapshots (
    doc_id     UUID        NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    seq        BIGINT      NOT NULL,
    state      BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (doc_id, seq)
);

-- Transactional outbox: written in the same transaction as the operation,
-- published to Kafka by the outbox relay.
CREATE TABLE outbox (
    id           BIGSERIAL PRIMARY KEY,
    topic        TEXT        NOT NULL,
    key          TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
