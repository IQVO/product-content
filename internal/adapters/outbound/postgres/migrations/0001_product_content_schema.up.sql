-- product-content schema (ADR 0002, 0003). Content holds no PII and no price.

CREATE TABLE product_contents (
    catalog_item_id    text        PRIMARY KEY,
    state              text        NOT NULL CHECK (state IN ('DRAFT','PUBLISHED','RETIRED')),
    retired_reason     text        CHECK (retired_reason IN ('MANUAL','DISCARDED','DISCONTINUED')),
    description        text        NOT NULL DEFAULT '',
    bullets            jsonb       NOT NULL DEFAULT '[]'::jsonb,
    attributes         jsonb       NOT NULL DEFAULT '[]'::jsonb,
    media              jsonb       NOT NULL DEFAULT '[]'::jsonb,
    primary_category_id text       NOT NULL DEFAULT '',
    revision           integer     NOT NULL CHECK (revision >= 1),
    published_revision integer     NOT NULL DEFAULT 0 CHECK (published_revision >= 0 AND published_revision <= revision),
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL,
    published_at       timestamptz,
    retired_at         timestamptz,
    version            bigint      NOT NULL CHECK (version >= 1),
    CHECK ((state = 'RETIRED') = (retired_reason IS NOT NULL)),
    CHECK ((state = 'RETIRED') = (retired_at IS NOT NULL))
);

-- The one category tree, stored whole (~10^3 categories): one row, one version.
CREATE TABLE category_trees (
    tree_id    text        PRIMARY KEY,
    version    bigint      NOT NULL CHECK (version >= 1),
    categories jsonb       NOT NULL DEFAULT '[]'::jsonb,
    updated_at timestamptz NOT NULL
);

-- CatalogItemView: the event-fed local read model of offer-availability's items.
CREATE TABLE catalog_item_view (
    catalog_item_id text        PRIMARY KEY,
    state           text        NOT NULL CHECK (state IN ('ACTIVE','DISCONTINUED')),
    updated_at      timestamptz NOT NULL
);

-- Infrastructure tables: inbox, idempotency keys, transactional outbox.
CREATE TABLE processed_events (
    event_id     text        PRIMARY KEY,
    event_type   text        NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_keys (
    route        text        NOT NULL,
    key          text        NOT NULL,
    request_hash text        NOT NULL,
    status       integer     NOT NULL,
    content_type text        NOT NULL,
    body         bytea       NOT NULL,
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (route, key)
);
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);

CREATE TABLE outbox_events (
    id           bigserial   PRIMARY KEY,
    event_id     text        NOT NULL UNIQUE,
    topic        text        NOT NULL,
    event_type   text        NOT NULL,
    subject      text        NOT NULL,
    key          bytea       NOT NULL,
    dataschema   text        NOT NULL,
    value        bytea       NOT NULL,
    headers      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts     integer     NOT NULL DEFAULT 0,
    last_error   text
);
CREATE INDEX outbox_events_unpublished_idx ON outbox_events (id) WHERE published_at IS NULL;
