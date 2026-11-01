---
id: observability
title: Observability
sidebar_label: Observability
sidebar_position: 3
---

# Observability

Logs, metrics and traces follow the fleet standard
([ADR 0005](/docs/adr/0005-fleet-observability-standard)). Everything below is
read from the code on this branch and from `warehouse-infra` `develop`.

## Exporter configuration

`internal/adapters/outbound/telemetry/telemetry.go` (`Setup`) builds a
`TracerProvider` and a `MeterProvider` that export over **OTLP/gRPC** to a
Collector, installs the W3C `traceparent` + baggage propagator and starts the Go
runtime metrics. Metrics are pushed every **15 s**. Export is non-blocking:
the exporters connect lazily, so a missing Collector costs telemetry, never
availability (exporter errors are logged at DEBUG as `opentelemetry error`).
A flush of at most 5 s runs last on shutdown.

| Variable | Default | Both binaries? |
| --- | --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` (bare `host:port` or URL; a bare value gets `http://`) | yes |
| `OTEL_SERVICE_NAME` | `product-content` (`cmd/api`), `product-content-a2a` (`cmd/a2a`) | yes, different defaults |
| `SERVICE_VERSION` | `dev` | yes |
| `ENVIRONMENT` | `local` (`deployment.environment.name`) | yes |
| `LOG_LEVEL` | `info` | yes |

The chart renders `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_SERVICE_NAME` from
`otel.endpoint` (`otel-collector.observability.svc.cluster.local:4317`) and
`otel.serviceName` when `otel.enabled` is true (the default); the A2A pod gets
`<serviceName>-a2a`. See [Configuration](/docs/operations/configuration).

## Metrics

| Instrument | Type | Unit | Attributes | Emitted by | Source |
| --- | --- | --- | --- | --- | --- |
| `content.products.published` (Prometheus `content_products_published_total`) | counter (Int64) | `{publication}` | `outcome` = `published` \| `rejected`; `reason` (on `rejected` only) = a problem slug: `not-found`, `content-retired`, `unknown-catalog-item`, `item-discontinued`, `incomplete-content`, `unknown-category`, `concurrent-update`, `illegal-transition`, `validation-failed`, `internal-error` | `cmd/api` | `internal/adapters/outbound/telemetry/content_metrics.go`, `internal/application/usecases/content_metrics.go` |
| `http.server.request.duration` | histogram | `s` | route (chi pattern), method (otelchi metric defaults) | both binaries | `otelchimetric.NewServerRequestDuration` in `internal/adapters/inbound/http/server.go`, `cmd/a2a/router.go` |
| Go runtime metrics (`go.goroutine.count`, `go.memory.used`, GC and others) | various | various | various | both binaries | `go.opentelemetry.io/contrib/instrumentation/runtime`, started in `telemetry.Setup` |

The business counter is incremented once per `PublishContent` attempt: a
revision that was saved counts `outcome=published`; a refusal counts
`outcome=rejected` with the `reason`. A publish with nothing new to publish
(idempotent no-op) counts neither. The counter binds to the global
`MeterProvider` after `telemetry.Setup` (`NewContentMetrics` is called after it
in `cmd/api/main.go`). The status code is **not** an attribute of the HTTP
histogram in this fleet; use Kong's `kong_http_requests_total` for 5xx.

## Traces

| Span | Kind | Created by | Attributes |
| --- | --- | --- | --- |
| `<METHOD> <chi route pattern>` (one name per route, not per item id) | server | `otelchi.Middleware` in the REST router and in `cmd/a2a` | HTTP semantic attributes |
| `<topic> process` (for example `retailnetwork.offer-availability.events process`) | consumer | `internal/adapters/inbound/kafka/tracing.go` | `messaging.system=kafka`, `messaging.destination.name`, `messaging.operation.name=process`; errors are recorded on the span |
| `a2a.skill <skill id>` | internal | `internal/adapters/inbound/a2a/skills.go` | `a2a.skill`; status Error with the problem detail when the skill answers a problem |

Trace context crosses Kafka: when a use case enqueues an event, the active
span context is injected as `traceparent`/`tracestate` into the outbox row's
headers **in the same transaction** (`internal/adapters/outbound/postgres/outbox_trace.go`),
the relay publishes them as Kafka headers, and the consumer (this service or
`product-discovery`) extracts them as the parent of its `<topic> process`
span. A message without the headers starts a new trace. The outbox relay and
the Postgres calls create no spans of their own.

## Logs

JSON on stdout (`log/slog`), level from `LOG_LEVEL`. The handler is wrapped by
`telemetry.WithTraceContext` (`internal/adapters/outbound/telemetry/slogotel.go`),
so a record written with a span-carrying context gets `trace_id` and `span_id`.

| Log line (`msg`) | Level | Fields | Source |
| --- | --- | --- | --- |
| `http request` (one per call; `/healthz` and `/readyz` are skipped) | INFO (2xx/3xx), WARN (4xx), ERROR (5xx) | `method`, `route` (pattern, never the raw path), `status`, `duration_ms`, `bytes`, `request_id`, `trace_id`, `span_id` | `internal/adapters/inbound/requestlog/requestlog.go` |
| `telemetry configured` | INFO | `service_name`, `otlp_endpoint` | `cmd/api/telemetry.go`, `cmd/a2a/main.go` |
| `readiness: dependency is not ready` | WARN | `dependency` (`postgres` or `kafka`), `error` | `internal/adapters/inbound/http/readiness.go` |
| `postgres adapters configured` / `postgres read side configured` | INFO | none | `cmd/api/main.go`, `cmd/a2a/main.go` |
| `retrying`, `succeeded after retry` (boot migrations / first ping) | WARN / INFO | `op`, `attempt`, `in`, `err` | `internal/bootretry/bootretry.go` |
| `consumer running` | INFO | `consumer`, `topic`, `group_id`, `brokers` | `cmd/api/main.go` |
| `outbox relay running` | INFO | `interval`, `max_attempts`, `topic` | `cmd/api/main.go` |
| `outbox relay pass failed` | ERROR | `error` | `internal/adapters/outbound/outbox/relay.go` |
| `outbox relay: row reached the attempt cap and is excluded from future claims; re-arm it with attempts = 0 after fixing the cause` | ERROR, once per row | `outbox_id`, `topic`, `event_type`, `key`, `attempts`, `max_attempts`, `error` | `relay.go` (`AttemptCapReachedMsg`) |
| `<consumer> handling failed; retrying the same message` | ERROR | `error`, `partition`, `offset`, `attempt`, `max_attempts`, `retry_in` | `internal/adapters/inbound/kafka/kafka.go` |
| `<consumer> message can never be applied; dead-lettering it without retry` / `... handling failed after the maximum attempts; dead-lettering the message` | ERROR | `error`, `partition`, `offset`, `attempts` | `kafka.go` |
| `<consumer> broker connection stalled or lost; fetching again after the backoff` | WARN | `error`, `attempt`, `backoff` | `kafka.go` |
| `skipping a message that is not a valid CloudEvent` | WARN | `consumer`, `topic`, `error` | `internal/adapters/inbound/kafka/consumer.go` |
| `dead-lettering an event that can never be applied` / `skipping an event that can never be applied` | WARN | `consumer`, `type`, `event_id`, `error` | `consumer.go` |
| `shutdown: readiness flipped to not-ready; waiting for traffic to drain` | INFO | `drain_delay` | `cmd/api/main.go` |
| `<worker> stopped unexpectedly: ...` / `consumer stopped` / `outbox relay stopped` | ERROR | `consumer`, `error` | `cmd/api/main.go` |
| `a2a skill failed` | ERROR | `error` | `internal/adapters/inbound/a2a/skills.go` |
| `invalid OUTBOX_RELAY_INTERVAL; using the default`, `invalid OUTBOX_MAX_ATTEMPTS; using the default`, `ignoring invalid SHUTDOWN_DRAIN_DELAY` | WARN | `value`, `default` | `cmd/api/config.go` |

## Dashboards and alerts (warehouse-infra `develop`)

| Artifact | Where | What it shows |
| --- | --- | --- |
| Grafana dashboard `warehouse-product-content` ("Warehouse - Product Content") | `terraform/dashboards/contexts/product-content.json` | Kong request rate, 5xx ratio and p95 latency (request vs upstream) for the context's routes; service HTTP RED by route (`http_server_request_duration_seconds_count`, p95 by process, 5xx by Kong route); the business panel `sum by (outcome) (rate(content_products_published_total[5m]))`; Go goroutines and memory; Loki live log stream, log volume by `level` and an errors-and-warnings stream for `{app="product-content"}` |
| Alert group `warehouse-product-content` | `terraform/alerting/warehouse-alert-rules.json` | `WarehouseKong5xxRatioHigh` (5xx ratio above 5%), `WarehousePodRestarting` (two or more container restarts of `product-content-.+` pods in 15 min) |
| Alert group `warehouse-product-content-slo` | same file | multi-window burn-rate alerts `WarehouseApiAvailabilityBurnFast`, `WarehouseApiLatencyBurnFast`, `WarehouseApiAvailabilityBurnSlow`, `WarehouseApiLatencyBurnSlow` (availability: 1 % 5xx budget; latency: at most 5 % of requests slower than 400 ms upstream; fast windows 1 h and 5 m at burn rate 14.4, slow windows 6 h and 30 m at burn rate 3) |

All alert annotations point their `runbook_url` at this repo's runbook
(`docs/operations/runbook.md` on `develop`, now served at
[Runbook](/docs/operations/runbook)) and their `dashboard_url` at the
dashboard above. There is **no** alert on outbox lag for this context (the
`WarehouseOutboxLagHigh` rule exists only for other contexts), none on
consumer lag, and no panel for the A2A pod beyond the shared `service_name=~"product-content.*"`
selectors. The dashboard and the Kong selectors also mention `-mcp` and
`-reports` routes that this repo does not have; they match nothing here.

Debugging pointers: [Troubleshooting](/docs/operations/troubleshooting).
