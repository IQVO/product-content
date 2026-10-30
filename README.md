# product-content

[![CI](https://github.com/IQVO/product-content/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/IQVO/product-content/actions/workflows/ci.yml?query=branch%3Adevelop) [![coverage gate](https://img.shields.io/badge/coverage%20gate-90%25-brightgreen)](https://github.com/IQVO/product-content/blob/develop/Makefile) [![Go](https://img.shields.io/badge/go-1.27.3-00ADD8?logo=go&logoColor=white)](https://github.com/IQVO/product-content/blob/develop/go.mod) [![release](https://img.shields.io/github/v/release/IQVO/product-content?sort=semver)](https://github.com/IQVO/product-content/releases) [![docs](https://img.shields.io/badge/docs-iqvo.github.io-blue)](https://iqvo.github.io/product-content/) [![license](https://img.shields.io/badge/license-MIT-blue)](https://github.com/IQVO/product-content/blob/develop/LICENSE)

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Product Content** (Generic, storefront tier).

Enriches the network's catalogue item with everything a product page needs and the item master does not have: description, bullets, attributes, media and the category tree. The title stays the item master's; media upload is out of scope (URLs to static assets only).

Status: built (domain, application, Postgres, Kafka, REST, A2A, BDD). This repository is part of the storefront domain: it consumes the retail network's Open Host Service routes and its published `retailnetwork.*` events; it never calls an in-org or restricted network route.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

## Documentation

This repo has no docs site of its own. The context's design lives in
[warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/) — `storefront/model/model.yaml` there is the single source of truth.

Page map:

| Area | Pages |
|---|---|
| Context pack | [Product Content context page](https://iqvo.github.io/warehouse-docs/storefront/contexts/product-content) and its 13-page ddd-crew pack (business context, ubiquitous language, core-domain chart, bounded-context canvas, context map, aggregate canvas, domain events, message flow, eventstorming, use cases, class/sequence/ER diagrams) |
| Strategic design | [Storefront strategic design](https://iqvo.github.io/warehouse-docs/storefront/strategic-design/) — subdomain classification, context map, CloudEvents event standard, model gap register |
| API reference | [REST reference generated from `apis/openapi.yaml`](https://iqvo.github.io/warehouse-docs/storefront/api-reference/rest/product-content/) |
| Decisions | [Storefront ADR index](https://iqvo.github.io/warehouse-docs/storefront/adr/), this repo's [ADR index](docs/adr/index.md) |
| In this repo | [`docs/adr/`](docs/adr/), [`docs/operations/runbook.md`](docs/operations/runbook.md), [`docs/before-first-release.md`](docs/before-first-release.md) |

## Architecture

Hexagonal / ports & adapters; dependencies point **inward only**.

```
cmd/api/                    composition root: REST API, outbox relay, the offer-availability
                            consumer and the RetireOnDiscontinue policy
cmd/a2a/                    read-only A2A agent over the same read use cases (ADR 0006)
internal/
  domain/                   pure business logic: content (the ProductContent aggregate),
                            category (the CategoryTree), shared
  application/
    ports/                  OUT interfaces only (repos, EventPublisher, Clock, metrics)
    usecases/               one struct per use case
    readmodel/              the local sibling read model (ADR 0003)
    idempotency/            idempotency-key pool
    outbox/                 transactional outbox
    views/                  read views
  adapters/
    inbound/                http (chi), kafka (the consumer), a2a, requestlog
    outbound/               postgres, kafka, outbox relay, telemetry, clock, ids,
                            memory (test doubles), repocontract
    kafka/cloudevents/      the CloudEvents 1.0 Kafka writer
  architecture/             the fitness tests below
  bootretry/                bounded Postgres/migration boot retry
  bdd/                      godog wiring for features/
```

There is no in-memory mode and no log sink: `cmd/api` needs a database and a
broker, and exits at boot naming the missing variable. The layering is
enforced as an executable test, not a convention:
`internal/architecture/architecture_test.go` (`TestHexagonalArchitecture`,
arch-go) fails the build if the domain ever imports an outer layer or an
adapter reaches sideways into a sibling; the same package adds fitness tests
for the event catalogue vs `apis/asyncapi.yaml`, the A2A adapter's read-only
rule, Kafka writer acks/topics/DLQ shutdown, the outbox relay cap, and
no-Go-files-at-root.

## Domain events

Contract: [`apis/asyncapi.yaml`](apis/asyncapi.yaml); the type catalogue is ADR 0004.

**Published** — three integration events on
`retailnetwork.product-content.events` through a **transactional outbox** (the
aggregate row and its already-encoded events are written in ONE database
transaction; an always-on relay drains them to Kafka, at-least-once, the
CloudEvents `id` is stored with the outbox row). Consumers include
`product-discovery`:

`ProductContentPublished`, `ProductContentRetired`, `CategoryTreePublished`.

Nothing is published on `retailnetwork.product-content.analytics`: the model
names no analytics-only event for this context. No event carries a title, a
price, stock or PII.

**Ordering.** `ProductContentPublished` and `ProductContentRetired` of one
item share a Kafka key (the catalogItemId) and carry a monotonically
increasing `revision`: a consumer applies a revision only above the last one
it applied. `CategoryTreePublished` carries the tree `version` and the WHOLE
tree, so a consumer replaces its tree when the version is above the last one
it applied.

**Consumed** — `CatalogItemCreated` and `CatalogItemDiscontinued` from
`retailnetwork.offer-availability.events` feed the local `CatalogItemView`
(ADR 0003); a discontinued item also runs the `RetireOnDiscontinue` policy.
The consumer group name comes from the mandatory
`OFFER_AVAILABILITY_CONSUMER_GROUP`. The messages list only the fields this
service relies on; the producer's own AsyncAPI is authoritative, unknown
fields are ignored.

**Envelope** (mandatory): CloudEvents 1.0, structured content mode, Kafka
header `content-type: application/cloudevents+json; charset=UTF-8`; `subject`
and Kafka key = the aggregate id. Type convention, real examples:

```
com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished
com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished
```

## Running it

Real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` and
`OFFER_AVAILABILITY_CONSUMER_GROUP` are mandatory; a boot error names the
missing variable). Migrations run automatically at boot. There is no compose
file in this repo — point the variables at any Postgres/Kafka (the fleet's
`warehouse-infra` kind cluster or the integration tests' testcontainers both
work).

```sh
export DATABASE_URL="postgres://user:***@localhost:5432/product_content?sslmode=disable"
export KAFKA_BROKERS="localhost:9092"
export OFFER_AVAILABILITY_CONSUMER_GROUP=product-content-offer-availability
go run ./cmd/api      # REST on :8080; relay, consumer and policy in the same process
```

`GET /readyz` is 200 only while Postgres answers a ping AND a Kafka broker
answers a metadata request; `/healthz` is liveness only.

### Configuration

`cmd/api`:

| Env var | Default | Meaning |
|---|---|---|
| `HTTP_ADDR` | `:8080` | REST listen address |
| `DATABASE_URL` | **required** | Postgres DSN; unset ⇒ boot error naming the variable |
| `MIGRATIONS_DATABASE_URL` | `DATABASE_URL` | Direct (non-pooled) DSN for the boot migrations |
| `KAFKA_BROKERS` | **required** | Comma-separated brokers (relay, consumer, `/readyz`) |
| `OFFER_AVAILABILITY_CONSUMER_GROUP` | **required** | Stable group id of the consumer |
| `OUTBOX_RELAY_INTERVAL` | `1s` | Relay poll interval (Go duration or seconds) |
| `OUTBOX_MAX_ATTEMPTS` | `20` | Failures before the relay excludes a row (`0` = unlimited) |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | Wait after flipping `/readyz` before closing |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `OTEL_SERVICE_NAME` | `product-content` | `service.name` of spans and metrics |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | in-cluster Collector | OTLP/gRPC endpoint; export never blocks |
| `SERVICE_VERSION` | `dev` | `service.version` |
| `ENVIRONMENT` | `local` | `deployment.environment.name` |

`cmd/a2a` reads `DATABASE_URL` (**required**, same store as the API),
`A2A_AGENT_URL` (**required**: the absolute `http(s)` URL advertised on the
card; the chart defaults it to the in-cluster Service DNS),
`MIGRATIONS_DATABASE_URL` (default `DATABASE_URL`), `A2A_ADDR` (default
`:8093`), `LOG_LEVEL` and the shared telemetry variables (`OTEL_SERVICE_NAME`
defaults to `product-content-a2a`). It never writes, so it needs no Kafka (no
`KAFKA_BROKERS`, consumer group or relay).

## Observability

Logs, metrics and traces follow the fleet standard ([ADR
0005](docs/adr/0005-fleet-observability-standard.md)): JSON logs on stdout
carry `trace_id`/`span_id` and a `request_id`; `cmd/api` exports OTLP/gRPC to
the Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`,
`SERVICE_VERSION`, `ENVIRONMENT`; export never blocks, so no Collector costs
telemetry, nothing else); the trace continues across Kafka in the
`traceparent` header of each published event and is picked up by the consumer.

| Instrument | Kind | Source |
|---|---|---|
| `content.products.published` | counter, attributes `outcome`, `reason` | **this service's business metric** |
| HTTP request duration | histogram + one server span and request-log line per call | the HTTP adapter, sharing a trace id |
| `go.*` runtime metrics | various | OTel Go runtime instrumentation |

## API

Contract: [`apis/openapi.yaml`](apis/openapi.yaml). No authentication. Errors
are RFC 9457 problem details (`application/problem+json`). Every
state-changing request takes an `Idempotency-Key` header; a retry with the
same key replays the first response.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/product-contents` | Create a content draft |
| `GET` | `/product-contents` | List contents |
| `GET` | `/product-contents/{catalogItemId}` | Get one item's content |
| `POST` | `/product-contents/{catalogItemId}:revise` | Revise the draft (description, bullets, attributes, media) |
| `POST` | `/product-contents/{catalogItemId}:publish` | Publish the draft |
| `POST` | `/product-contents/{catalogItemId}:retire` | Retire the content |
| `POST` | `/product-contents/{catalogItemId}:discard` | Discard the draft |
| `POST` | `/categories` | Create a category |
| `GET` | `/categories` | Get the category tree |
| `POST` | `/categories:publish` | Publish the category tree (bumps the tree version) |
| `POST` | `/categories/{categoryId}:move` | Move a category |
| `POST` | `/categories/{categoryId}:rename` | Rename a category |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness (Postgres ping + Kafka metadata) |

## Helm chart

[`charts/product-content`](charts/product-content) — one always-on component:
the `api` Deployment (REST, relay, consumer and policy in one process); the
read-only A2A agent is an optional extra component (`a2a.enabled=true`,
default off). The chart refuses to render without `database.*`,
`kafka.brokers` and `config.offerAvailabilityConsumerGroup` (named in the
failure). `DATABASE_URL` always comes from a Secret.

```sh
helm install product-content ./charts/product-content \
  --set database.existingSecret=product-content-db \
  --set kafka.brokers=kafka:9092 \
  --set config.offerAvailabilityConsumerGroup=product-content-offer-availability
```

Telemetry is the `otel` values block (`enabled: true` by default, safe where
no Collector is deployed). Chart invariants are asserted by
`charts/product-content/tests/` (`render_test.sh`, `test_service_selectors.py`),
run in CI by the `helm-lint` (`ct lint`) and `chart-tests` jobs.

### Running the A2A agent in Kubernetes

The A2A agent ([ADR 0006](docs/adr/0006-a2a-inbound-adapter.md)) is a second
binary in the same image, `/app/a2a`, and the Helm chart deploys it as a
separate Deployment + ClusterIP Service (`<release>-a2a`, port 8093) when
`a2a.enabled=true` (default `false`; route at `/api/a2a/product-content` via
`a2a.route.*`). It is **read-only** and answers two goal-shaped skills over
the same read use cases as the REST GETs, as a message-only agent over
JSON-RPC (Agent Card at `/.well-known/agent-card.json`, endpoint at `/` and
`/a2a`, probes on `/healthz`):

- `get_product_content` `{"catalogItemId": "..."}`: the content of one
  catalogue item (`GET /product-contents/{catalogItemId}`).
- `list_categories` `{}`: the category tree (`GET /categories`).

Requests are `{"skill": "<id>", "arguments": {...}}` in a data part (or a text
part holding the same JSON); unknown arguments are refused. Rejections come
back **in the answer** as RFC 7807-shaped problems with the REST statuses
(`404 not-found`, `400 invalid-argument`, `500 internal-error`, ...), never as
JSON-RPC errors. A2A carries delegation only: facts still leave as CloudEvents
from the API's transactional outbox. The surface is unauthenticated, like REST.

- Layout: `cmd/a2a/` (composition root, read side only),
  `internal/adapters/inbound/a2a/` (skills over `GetProductContent` and
  `ListCategories`), `charts/product-content/templates/a2a-*.yaml`.

## Local development / quality gate

Every CI sensor is a `make` target; `make help` lists them all.

```sh
make check-fast  # fmt-check, vet, arch-test (+ tests of changed packages)
make check       # fmt-check, vet, build, lint, test
make check-all   # check + coverage (90% gate over domain+application) + arch-test + bdd
make integration # testcontainers Postgres/Kafka — needs only Docker
make mutation    # gremlins over ./internal/domain
make vuln        # govulncheck ./...
```

Git hooks are managed by [lefthook](https://github.com/evilmartians/lefthook)
(`lefthook.yml`): pre-commit runs `fmt-check` + `vet` + `lint`; pre-push runs
`make check`. Activate once per clone:

```sh
brew install lefthook   # or: go install github.com/evilmartians/lefthook@latest
lefthook install
```

## Tests

```sh
go test ./... -race                        # unit suites
make coverage                              # domain+application coverage, CI gate >= 90%
go test -tags=integration ./...            # testcontainers Postgres/Kafka (Docker only)
go test ./features/ -run TestFeatures -v   # godog/BDD over the real HTTP API
go test ./internal/architecture/... -v     # architecture fitness tests (arch-go)
gremlins unleash ./internal/domain         # mutation, thresholds in .gremlins.yaml
```

CI (`.github/workflows/ci.yml`) runs `lint`, `guide-lint`, `complexity`,
`test` (unit + the 90% coverage gate), `bdd`, `integration`, `mutation-fast`
(the blocking subset), `api-lint` (Spectral over `apis/openapi.yaml`), `vuln`
(govulncheck), `helm-lint` (`ct lint`), `chart-tests`, and `arch-test` on
every push (branches `main`, `develop`) and pull request. The exhaustive
`mutation` job runs only on the weekly schedule (Monday 06:00 UTC) and manual
dispatch, opening a `harness:red` issue on a scheduled failure.
`docker-publish` (GHCR, cosign-signed) and `release` (tag `vX.Y.Z` + chart
publish) run only on pushes to `main`, after every other job is green.

## Architecture Decision Records

[`docs/adr/`](docs/adr/) — [index](docs/adr/index.md); 6 ADRs (0001 bounded
context, 0002 assumptions, 0003 consumed contracts and local read model, 0004
CloudEvents envelope and type catalogue, 0005 fleet observability standard,
0006 A2A inbound adapter).

## License

[MIT](LICENSE).
