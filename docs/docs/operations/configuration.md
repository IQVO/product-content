---
id: configuration
title: Configuration
sidebar_label: Configuration
sidebar_position: 1
---

# Configuration

`product-content` ships **two binaries**, `cmd/api` and `cmd/a2a`, both
configured only through environment variables. There is no config file, no
flag and no secret-manager integration in the repository. REST and A2A are
unauthenticated fleet-wide, so there is no token or key to configure.

The service runs on real Postgres (both binaries) and real Kafka (`cmd/api`
only) with no in-memory mode. Mandatory variables are validated before
anything is opened or bound; a missing one is a boot error that names it
(see [Troubleshooting](/docs/operations/troubleshooting)).

The variables are read in `cmd/api/config.go`, `cmd/api/main.go`,
`cmd/api/telemetry.go`, `cmd/a2a/main.go` and, for the telemetry resource,
`internal/adapters/outbound/telemetry/telemetry.go`. No other package reads
the environment. There are no test-only variables (the integration tests start
their own containers).

## `cmd/api`

REST, the transactional outbox relay and the offer-availability consumer in
one process.

| Variable | Default | Required? | Meaning |
| --- | --- | --- | --- |
| `HTTP_ADDR` | `:8080` | no | Listen address of the REST server (also serves `/healthz` and `/readyz`). The chart keeps `service.targetPort` equal to its port. |
| `DATABASE_URL` | none | **yes** | Postgres DSN. Missing = boot error `DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode`. Pool of at most 10 connections, `statement_timeout = 5s`. |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct (non-pooled, session-mode) DSN used only for the embedded boot migrations, whose advisory lock does not survive a transaction-mode pooler. Empty falls back to `DATABASE_URL`. |
| `KAFKA_BROKERS` | none | **yes** | Comma-separated broker list (entries trimmed, blanks dropped). Missing = boot error `KAFKA_BROKERS is required: this context runs on real Kafka (outbox relay, consumers, /readyz)`. Used by the relay, the consumer and the `/readyz` broker check. Locally the single broker is `localhost:9092`. |
| `OFFER_AVAILABILITY_CONSUMER_GROUP` | none | **yes** | Stable Kafka consumer group id of the offer-availability consumer (topic `retailnetwork.offer-availability.events`). No default and no literal in the code. Missing = boot error `OFFER_AVAILABILITY_CONSUMER_GROUP is required: the consumer always runs on real Kafka`. Suggested value: `product-content-offer-availability`. |
| `OUTBOX_RELAY_INTERVAL` | `1s` | no | Pause between relay passes that did not fill a batch. A Go duration (`500ms`, `2s`) or plain seconds (`2`, `0.5`). Unparsable or non-positive values log a WARN and use the default. |
| `OUTBOX_MAX_ATTEMPTS` | `20` | no | Failures after which the relay stops claiming an outbox row. `0` = unlimited. Negative or unparsable values log a WARN and use `20`, so a typo never removes the cap. |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | no | After SIGINT/SIGTERM `/readyz` flips to 503 and the process waits this long before closing the listener. `0` disables the wait. Negative or unparsable values log a WARN and use `5s`. Read when the signal arrives, not at boot. |
| `LOG_LEVEL` | `info` | no | `debug`, `info`, `warn` (or `warning`), `error`, case-insensitive. Anything else silently falls back to `info`. Logs are JSON on stdout and carry `trace_id`/`span_id` when written inside a span. |
| `OTEL_SERVICE_NAME` | `product-content` | no | `service.name` of spans and metrics, also the HTTP server span/metric name. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint, `host:port` or URL. Export never blocks: an unreachable Collector costs telemetry, never availability. |
| `SERVICE_VERSION` | `dev` | no | `service.version` resource attribute. The chart sets it to the image tag. |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name` resource attribute, read by `telemetry.Setup`. |

The first mandatory failure is returned alone (the checks run in the order
`DATABASE_URL`, `KAFKA_BROKERS`, `OFFER_AVAILABILITY_CONSUMER_GROUP`).

## `cmd/a2a`

Read-only Agent2Agent agent over the same Postgres. It has no Kafka producer,
consumer or relay, so it reads **no** `KAFKA_BROKERS`, `OFFER_AVAILABILITY_CONSUMER_GROUP`
or `OUTBOX_*` variable; the chart never renders them into this workload.

| Variable | Default | Required? | Meaning |
| --- | --- | --- | --- |
| `A2A_ADDR` | `:8093` | no | Listen address (JSON-RPC on `/` and `/a2a`, Agent Card, `/healthz`). |
| `A2A_AGENT_URL` | none | **yes** | Absolute `http(s)` URL of this agent's JSON-RPC endpoint, advertised on the Agent Card. A missing, relative or schemeless value is a boot error naming the variable. The chart defaults it to `http://<release>-a2a.<ns>.svc.cluster.local:8093/`. |
| `DATABASE_URL` | none | **yes** | Postgres DSN (the same database as `cmd/api`). Missing = boot error `DATABASE_URL is required: ...`. Both problems are reported together. |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct DSN for the embedded boot migrations; `cmd/a2a` applies them on start like `cmd/api` (idempotent). |
| `LOG_LEVEL` | `info` | no | As `cmd/api`. |
| `OTEL_SERVICE_NAME` | `product-content-a2a` | no | `service.name` of spans and metrics. Note the `-a2a` default, different from `cmd/api`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint. |
| `SERVICE_VERSION` | `dev` | no | `service.version`, also advertised as the Agent Card version. |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name`, read by `telemetry.Setup`. |

Real output shape of a misconfigured start is documented in
[Troubleshooting](/docs/operations/troubleshooting).

## Readiness (`cmd/api`)

`GET /readyz` is 200 only when the shutdown drain gate is open **and**
Postgres answers a ping **and** a Kafka broker answers a topic-less metadata
request. The verdict is cached for 3 s and a round of checks is bounded by 2 s.
`/healthz` is liveness only. `cmd/a2a` serves `GET /healthz` only and uses it
for all three probes; it has no `/readyz`.

## Compiled-in settings (not configurable)

| Setting | Value | Source |
| --- | --- | --- |
| HTTP `ReadHeaderTimeout` | 5 s (both binaries) | `cmd/api/main.go`, `cmd/a2a/main.go` |
| HTTP graceful shutdown timeout / per-worker drain timeout | 10 s / 10 s (`cmd/api`); 10 s (`cmd/a2a`) | `cmd/api/main.go` (`shutdownTimeout`, `workerDrainTimeout`), `cmd/a2a/main.go` |
| Telemetry flush on shutdown | 5 s | `cmd/api/telemetry.go`, `cmd/a2a/main.go` |
| Postgres pool size and statement timeout | 10 connections, `statement_timeout = 5s` | `internal/adapters/outbound/postgres/pool.go` |
| Boot retry (migrations, first ping) | 5 attempts, backoff 1 s, 2 s, 4 s, 8 s | `internal/bootretry/bootretry.go` |
| Outbox relay batch size | 100 rows per pass | `internal/adapters/outbound/outbox/relay.go` (`DefaultBatchSize`) |
| Kafka producer | acks=all, hash balancer on the message key, 10 ms batch timeout, `AllowAutoTopicCreation` | `internal/adapters/outbound/kafka/writer.go` |
| Consumer retry backoff | 200 ms doubling to a 5 s cap; at most 5 handling attempts, then dead letter on `<topic>.dlq` | `internal/adapters/inbound/kafka/kafka.go`, `deadletter.go` |
| Transient broker-connection failures on fetch | up to 10 consecutive, then the worker fails and the process exits | `internal/adapters/inbound/kafka/kafka.go` (`maxTransientFetchFailures`) |
| `/readyz` verdict cache / check timeout | 3 s / 2 s | `internal/adapters/inbound/http/readiness.go` |
| Request body limit | 64 KiB | `internal/adapters/inbound/http/response.go` |
| `Idempotency-Key` length / record TTL | 1 to 128 characters / 24 h | `internal/adapters/inbound/http/idempotency.go`, `internal/application/idempotency/idempotency.go` |
| List page size default | 100 | `internal/application/usecases/queries.go` (`DefaultPageSize`) |
| Topic produced | `retailnetwork.product-content.events` | `internal/adapters/outbound/kafka/encoder.go` (`Topic`) |
| Topic consumed | `retailnetwork.offer-availability.events` | `internal/adapters/inbound/kafka/offer_availability.go` |

## What is deliberately absent

- **No auth variables.** REST and A2A are unauthenticated fleet-wide;
  `TestNoAuthMiddlewareReintroduced` guards REST.
- **No `*_MODE` variables.** Postgres and Kafka are the only runtime; the
  chart refuses any `*_MODE` entry in `extraEnv`.
- **No MCP.** The repo has no `cmd/mcp`.
- **No CORS, rate-limit or feature-flag variables.**

## Chart values

`charts/product-content` maps these variables onto the pods: `database.*`,
`kafka.brokers`, `config.*`, `otel.*` and `a2a.*` (see
[Runbook](/docs/operations/runbook)).

| Chart value | Variable |
| --- | --- |
| `config.httpAddr` | `HTTP_ADDR` |
| `config.logLevel` | `LOG_LEVEL` (api and a2a) |
| `config.offerAvailabilityConsumerGroup` | `OFFER_AVAILABILITY_CONSUMER_GROUP` |
| `config.outboxRelayInterval` | `OUTBOX_RELAY_INTERVAL` |
| `config.outboxMaxAttempts` | `OUTBOX_MAX_ATTEMPTS` |
| `config.shutdownDrainDelay` | `SHUTDOWN_DRAIN_DELAY` |
| `database.url` / `database.existingSecret` | `DATABASE_URL` (always from a Secret) |
| `database.migrationsExistingSecretKey` / `database.migrationsUrl` | `MIGRATIONS_DATABASE_URL` |
| `kafka.brokers` | `KAFKA_BROKERS` |
| `otel.endpoint`, `otel.serviceName` | `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME` (a2a gets `<serviceName>-a2a`) |
| `a2a.httpAddr` | `A2A_ADDR` |
| `a2a.agentUrl` | `A2A_AGENT_URL` |
