---
id: quickstart
title: Quickstart
sidebar_label: Quickstart
sidebar_position: 3
---

# Quickstart

Run `product-content` locally against real Postgres and a real Kafka broker,
then make the first calls. The request and response shapes below come from the
code (`internal/adapters/inbound/http/dto.go`, `errors.go`) and
`apis/openapi.yaml`; they are not a captured session.

## Prerequisites

- Go 1.27 (see `go.mod`), `make`, `git`.
- A Postgres you can create a database in, and ONE Kafka broker reachable at
  `localhost:9092` (the fleet's single platform broker from the
  `warehouse-infra` kind cluster; the old docker-compose Kafka is retired and
  this repo ships no compose file).
- Docker, only for `make integration` (testcontainers boots its own Postgres
  and Kafka).
- Optional: [`kcat`](https://github.com/edenhill/kcat) to publish the one
  upstream event the service needs, `curl` and `jq` for the calls.

## Build and check

```sh
make help         # lists every target
make build        # go build ./...
make check        # fmt-check, vet, build, lint, test
make check-all    # check + coverage (90% gate) + arch-test + bdd
make integration  # testcontainers Postgres/Kafka, needs only Docker
```

`make test` needs no database: the unit, `httptest` and godog suites run on
in-memory test doubles (`internal/adapters/outbound/memory`), which are never
linked into the binaries
(`TestRuntimeBinaryHasNoInMemoryAdapters`).

## Start the API

Create a database, then export the three mandatory variables (the full list
is in [Configuration](/docs/operations/configuration)):

```sh
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/product_content?sslmode=disable"
export KAFKA_BROKERS="localhost:9092"
export OFFER_AVAILABILITY_CONSUMER_GROUP="product-content-offer-availability"
go run ./cmd/api
```

Migrations (`internal/adapters/outbound/postgres/migrations/`) run at boot,
with a bounded retry. The log is JSON on stdout; look for
`postgres adapters configured`, `consumer running`, `outbox relay running` and
`http server listening` (`:8080`). Missing one of the three variables exits
with a message naming it.

```sh
curl -i http://localhost:8080/healthz    # 200, liveness only
curl -i http://localhost:8080/readyz     # 200 only while Postgres AND Kafka answer
```

Optionally start the read-only A2A agent next to it (same database, no Kafka):

```sh
export A2A_AGENT_URL="http://localhost:8093/"
A2A_ADDR=:8093 go run ./cmd/a2a
curl -s http://localhost:8093/.well-known/agent-card.json | jq .name
```

## First calls

Every state-changing POST needs an `Idempotency-Key` header (1 to 128
characters); without it the answer is `400 idempotency-key-required`. All
errors are RFC 9457 `application/problem+json`.

### 1. Build a category tree

Content must name a category of the live tree to be published. A tree
command creates the category and publishes a new tree version
(`CategoryTreePublished`):

```sh
curl -s -X POST http://localhost:8080/categories \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: qs-cat-1' \
  -d '{"name":"Home"}'
```

The response is `{"categoryId":"...","name":"Home","path":"..."}`; keep the
`categoryId`. A child category adds `"parentId":"<categoryId>"`; the tree is at
most four levels deep. Read the whole tree with `GET /categories` (`version` and
`categories`).

### 2. Make the catalogue item known

`POST /product-contents` is refused with `422 unknown-catalog-item` until the
service has seen `CatalogItemCreated` for that id on
`retailnetwork.offer-availability.events` (it builds its own
`catalog_item_view` from the network's events and never calls the network).
Locally, publish the event yourself as a structured-mode CloudEvent:

```sh
echo 'item-1|{"specversion":"1.0","id":"00000000-0000-4000-8000-000000000001","source":"/retailnetwork/offer-availability","type":"com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated","datacontenttype":"application/json","data":{"catalogItemId":"item-1"}}' \
  | kcat -P -b localhost:9092 -t retailnetwork.offer-availability.events -K '|' \
      -H 'content-type=application/cloudevents+json; charset=UTF-8'
```

Only `data.catalogItemId` is read (1 to 64 characters of `A-Z a-z 0-9 . _ : -`,
starting with a letter or digit). A malformed id is dead-lettered to
`retailnetwork.offer-availability.events.dlq`.

### 3. Create, revise, publish

```sh
curl -s -X POST http://localhost:8080/product-contents \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: qs-create-1' \
  -d '{
    "catalogItemId": "item-1",
    "description": "A sturdy oak desk.",
    "bullets": ["Solid oak", "Cable tray"],
    "attributes": [{"name": "Width", "value": "140", "unit": "cm"}],
    "media": [{"mediaId": "m1", "url": "https://cdn.example.test/desk.jpg", "altText": "Oak desk", "role": "PRIMARY"}],
    "primaryCategoryId": "<categoryId from step 1>"
  }'
```

The answer is `201` with `state: "DRAFT"`, `revision: 1` and
`publishedRevision: 0`. Publishing needs a description, exactly one `PRIMARY`
medium and a category of the tree, otherwise `422 incomplete-content`:

```sh
curl -s -X POST 'http://localhost:8080/product-contents/item-1:revise' \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: qs-revise-1' \
  -d '{"description":"A sturdy oak desk, 140 cm.","bullets":[],"attributes":[],"media":[{"mediaId":"m1","url":"https://cdn.example.test/desk.jpg","altText":"Oak desk","role":"PRIMARY"}],"primaryCategoryId":"<categoryId>"}'
curl -s -X POST 'http://localhost:8080/product-contents/item-1:publish' -H 'Idempotency-Key: qs-publish-1'
curl -s 'http://localhost:8080/product-contents/item-1' | jq .state
```

`revise` replaces the whole body (it is not a patch) and takes a new
revision. After `publish` the outbox relay sends `ProductContentPublished`
to `retailnetwork.product-content.events` within about
`OUTBOX_RELAY_INTERVAL` (1 s):

```sh
kcat -C -b localhost:9092 -t retailnetwork.product-content.events -o beginning -e
```

Replaying a call with the same `Idempotency-Key` returns the first response
with the header `Idempotency-Replayed`; the same key with a different body is
`409 idempotency-key-reused`.

### 4. Retire

```sh
curl -s -X POST 'http://localhost:8080/product-contents/item-1:retire' -H 'Idempotency-Key: qs-retire-1'
```

`retire` is for PUBLISHED content (it raises `ProductContentRetired`);
`DELETE .../draft` retires a DRAFT without an event. RETIRED is terminal.

## Where to go next

- [Configuration](/docs/operations/configuration) and [Runbook](/docs/operations/runbook)
- [Architecture](/docs/overview/architecture) and [Use cases](/docs/ddd/use-cases)
- [API reference](/docs/api-reference) and [Testing](/docs/development/testing)
