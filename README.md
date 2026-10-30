# product-content

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Product Content** (Generic, storefront tier).

Enriches the network's catalogue item with everything a product page needs and the item master does not have: description, bullets, attributes, media and the category tree. The title stays the item master's; media upload is out of scope (URLs to static assets only).

Status: under construction. The design lives in [warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/) `storefront/model/model.yaml` (single source of truth).
This repository is part of the storefront domain: it consumes the retail network's Open Host Service routes and its
published `retailnetwork.*` events; it never calls an in-org or restricted network route.

Runtime: Go, hexagonal, real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` are mandatory), CloudEvents 1.0 on every message
(`com.retailnetwork.storefront.product-content.<entity>.<Event>`), transactional outbox.

## Observability

Logs, metrics and traces follow the fleet standard ([ADR 0005](docs/adr/0005-fleet-observability-standard.md)): JSON logs on stdout carry `trace_id`/`span_id`
and a `request_id`; `cmd/api` exports OTLP/gRPC to the Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `SERVICE_VERSION`, `ENVIRONMENT`;
export never blocks, so no Collector costs telemetry, nothing else) with HTTP request duration, Go runtime metrics and the business counter
`content.products.published` (`outcome`, `reason`); the trace continues across Kafka in the `traceparent` header of each published event and is picked up by the
consumer.

## A2A agent (`cmd/a2a`)

The A2A agent ([ADR 0006](docs/adr/0006-a2a-inbound-adapter.md)) is a second binary in the same image, `/app/a2a`, and the Helm chart deploys it as a separate
Deployment + ClusterIP Service (`<release>-a2a`, port 8093) when `a2a.enabled=true` (default `false`; route at `/api/a2a/product-content` via `a2a.route.*`).
It is **read-only** and answers two goal-shaped skills over the same read use cases as the REST GETs, as a message-only agent over JSON-RPC
(Agent Card at `/.well-known/agent-card.json`, endpoint at `/` and `/a2a`, probes on `/healthz`):

- `get_product_content` `{"catalogItemId": "..."}`: the content of one catalogue item (`GET /product-contents/{catalogItemId}`).
- `list_categories` `{}`: the category tree (`GET /categories`).

Requests are `{"skill": "<id>", "arguments": {...}}` in a data part (or a text part holding the same JSON); unknown arguments are refused. Rejections come back
**in the answer** as RFC 7807-shaped problems with the REST statuses (`404 not-found`, `400 invalid-argument`, `500 internal-error`, ...), never as JSON-RPC errors.
It never writes, so it needs no Kafka (no `KAFKA_BROKERS`, consumer group or relay): environment `DATABASE_URL` (required, same store as the API),
`MIGRATIONS_DATABASE_URL` (default `DATABASE_URL`), `A2A_AGENT_URL` (required: the absolute `http(s)` URL advertised on the card; the chart defaults it to the
in-cluster Service DNS), `A2A_ADDR` (default `:8093`), `LOG_LEVEL`, `OTEL_SERVICE_NAME` (default `product-content-a2a`). A2A carries delegation only: facts still
leave as CloudEvents from the API's transactional outbox. The surface is unauthenticated, like REST.

Layout: `cmd/a2a/` (composition root, read side only), `internal/adapters/inbound/a2a/` (skills over `GetProductContent` and `ListCategories`),
`charts/product-content/templates/a2a-*.yaml`.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
