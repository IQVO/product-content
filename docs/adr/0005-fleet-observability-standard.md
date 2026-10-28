# ADR 0005: Adopt the fleet observability standard (logs, metrics, traces)

## Status

Accepted (2026-10-26).

## Context

product-content logged JSON with `log/slog` and nothing else: no trace or span id
on a log line, no request id, no request log, no metrics, no traces and no
`traceparent` on the events it publishes. The warehouse-systems services
(inventory-storage, wes-work-planning) already share one pipeline and one
convention, recorded in their ADRs "standard metrics convention" (inventory-storage
ADR 0016, wes-work-planning ADR 0013), and customer-ordering adopted it in its
ADR 0007:

- pods export OTLP/gRPC to the in-cluster Collector
  (`otel-collector.observability.svc.cluster.local:4317`), which feeds Jaeger
  (traces) and a Prometheus exporter (metrics);
- logs stay on stdout as JSON for Alloy and Loki.

Without the same wiring a content publication could not be followed from the REST
call through the Kafka event to its consumers in other services, a log line could
not be joined to its trace, and no dashboard could show publish rate, errors or
latency. A service-specific setup was rejected (every repo should answer the same
query the same way), as were the OTel auto-instrumentation agents (they cannot
follow a trace across `segmentio/kafka-go` nor name spans by the chi route
pattern).

## Decision

Tier 1 (identical in every repo) and Tier 2 (this context's business metric):

1. **Telemetry adapter.** `internal/adapters/outbound/telemetry` holds `Setup`
   (OTLP/gRPC traces and metrics, W3C TraceContext + Baggage propagator, Go
   runtime metrics; exporters dial lazily, so no Collector means dropped
   telemetry, never a blocked boot or request; returns a shutdown func) and
   `WithTraceContext` (stamps `trace_id`/`span_id` on a log record written with a
   span-carrying context). Only the composition root (`cmd/api`) and the other
   adapters import it; `internal/domain` and `internal/application` never import
   OpenTelemetry.
2. **Logger.** `slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, ...)))`,
   `LOG_LEVEL`, uppercase levels. Code on a request or message path uses the
   `*Context` slog variants; the bare variants have no span.
3. **HTTP middleware order** on the chi router: `RequestID` ->
   `otelchi.Middleware(serviceName, WithChiRoutes(r))` ->
   `otelchimetric.NewServerRequestDuration` -> `RequestLogger` -> `Recoverer`; the
   existing `rejectNUL` and Idempotency-Key middleware stay after them.
   `telemetry.Setup` runs before the router is built (otelchimetric binds the
   global MeterProvider at construction) and is flushed last on shutdown. The
   request line carries `method`, `route` (the chi pattern, never the raw path),
   `status`, `duration_ms`, `bytes` and `request_id`; 5xx is ERROR, 4xx WARN, the
   rest INFO; `/healthz` and `/readyz` are not logged. `RequestLogger` lives in
   `internal/adapters/inbound/requestlog`, so a future `cmd/mcp` can share it
   without importing the REST adapter.
4. **Traces over Kafka.** `OutboxRepo.Append` stamps `traceparent`/`tracestate`
   from the active span into the stored outbox headers, inside the transaction
   that writes the aggregate (`internal/adapters/outbound/postgres/outbox_trace.go`).
   The relay publishes the headers unchanged as Kafka headers; the inbound
   consumer extracts them and runs every handling attempt in a consumer span named
   `<topic> process`. Only extra headers are added: the CloudEvents attributes and
   body are unchanged (ADR 0004).
5. **Environment contract.** `cmd/api` reads `OTEL_SERVICE_NAME` (default
   `product-content`), `OTEL_EXPORTER_OTLP_ENDPOINT` (default
   `telemetry.DefaultEndpoint`), `SERVICE_VERSION`, `ENVIRONMENT` (read by `Setup`)
   and `LOG_LEVEL`.
6. **Tier 2 business metric.** One counter, `content.products.published`, unit
   `{publication}`, attribute `outcome` (`published` | `rejected`) and, on a
   rejection, `reason` (the problem slug the REST adapter answers with:
   `incomplete-content`, `item-discontinued`, `content-retired`, `unknown-category`,
   `unknown-catalog-item`, `not-found`, `concurrent-update`, `illegal-transition`,
   `validation-failed`, `internal-error`). `ports.ContentMetrics` is the
   application port; `PublishContent` calls it once per publish attempt; the OTel
   implementation is in the telemetry adapter and is injected from `cmd/api`; a nil
   `Deps.Metrics` is a no-op. A publish with nothing new to publish (ADR 0002, A1)
   is not an attempt and is not counted.

### Why this counter

`apis/openapi.yaml` and ADR 0001 make publication the one act that matters to the
context: shoppers only ever see `publishedRevision`, and `ProductContentPublished`
is the event other contexts consume. The other commands (create, revise, retire,
discard, the category commands) are either authoring steps or rarely used. The
interesting operational question is how often a publish is refused and why, which
is exactly an `outcome` + `reason` counter.

### Assumptions

- The Tier 2 counter is `content.products.published` (namespace prefix is the
  context name, per the fleet brief), counted in `PublishContent` only.
- Idempotent no-op publishes are excluded so retried requests do not inflate it.
- `PublishCategoryTree` also raises a publish event but is a tree command, not
  content publication; it is not counted.

### Deviations from customer-ordering (ADR 0007)

- No `cmd/mcp` in this repo: nothing to wire, one `OTEL_SERVICE_NAME` default.
- No Helm chart yet (ecommerce repos are not deployed): the `otel.*` values and the
  chart render tests are skipped and must be added with the chart.
- One inbound consumer (offer availability) instead of several; same extraction.

Not part of this ADR: dashboards and alert rules, which live in warehouse-infra
and are generated per deployed context from the instrument names above.

## Consequences

- A request, its log lines, the events it enqueued and the consumers that handle
  them share one trace id; `trace_id` in Loki jumps to Jaeger.
- Request rate, errors and duration come from `http.server.request.duration`
  (otelchimetric); publication health from `content.products.published`.
- Every new log call on a request or message path must use a `*Context` variant,
  or its line carries no trace id.
- A consumer span is created per handling attempt, so a retried message shows as
  several spans under the same parent.
- `OutboxRepo.Insert` (the raw path used by tests) stamps nothing; only `Append`,
  the path every use case takes, does.
