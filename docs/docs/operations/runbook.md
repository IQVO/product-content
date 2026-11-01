---
id: runbook
title: Runbook
sidebar_label: Runbook
sidebar_position: 2
---

# Operations Runbook

Product Content owns descriptions, bullets, attributes, media URLs and the
category tree that enrich a network catalogue item (no price, no stock, no
PII). It ships **two binaries in one image**:

| Binary | Role | Port | Kubernetes workload |
| --- | --- | --- | --- |
| `cmd/api` (image `ENTRYPOINT ./api`) | REST API, the transactional outbox relay and the offer-availability consumer in a single process | `:8080` | Deployment `<release>` (always on) |
| `cmd/a2a` (`command: ["/app/a2a"]`) | Read-only A2A agent over the same Postgres: no Kafka, no relay | `:8093` | Deployment `<release>-a2a` (optional, `a2a.enabled`, default off) |

Runtime: real Postgres and, for `cmd/api`, real Kafka only. There is no
in-memory or permissive mode, so a pod that cannot reach either refuses to
boot and never pretends to be healthy. Variables are in
[Configuration](/docs/operations/configuration).

## Deploy

The chart is `charts/product-content` (packaged by the `release` job as
`oci://ghcr.io/iqvo/product-content:<version>`; the image is
`ghcr.io/iqvo/product-content:v<version>`, also tagged `latest` and the short
SHA). The kind cluster deploys it through the warehouse-infra ArgoCD app
(`helm-values/product-content.yaml`) with the local image
`warehouse/product-content`; Kong serves the API under `/api/product-content`
and, when enabled, the A2A agent under `/api/a2a/product-content`. The pods run
as uid 1000 with a read-only root filesystem.

Mandatory values — the chart refuses to render without them:

| Value | Env var | Why |
| --- | --- | --- |
| `database.url` or `database.existingSecret` | `DATABASE_URL` | Postgres DSN (from a Secret, never the ConfigMap) |
| `database.migrationsExistingSecretKey` / `database.migrationsUrl` | `MIGRATIONS_DATABASE_URL` | Direct (non-pooled) DSN for boot migrations; the Secret key is `optional`, the binary falls back to `DATABASE_URL` |
| `kafka.brokers` | `KAFKA_BROKERS` | Comma-separated brokers (relay, consumer, `/readyz`) |
| `config.offerAvailabilityConsumerGroup` | `OFFER_AVAILABILITY_CONSUMER_GROUP` | Stable, service-prefixed group id (`product-content-offer-availability`), never the pod name; `extraEnv` may not set `*_CONSUMER_GROUP` or any `*_MODE` |

Useful optionals: `config.outboxRelayInterval` (default 1s),
`config.outboxMaxAttempts` (default 20, 0 = unlimited),
`config.shutdownDrainDelay` (default 5s), `autoscaling.api.*` (HPA, off).
`terminationGracePeriodSeconds` (40) must exceed drain + 30s (HTTP 10s +
consumer 10s + relay 10s); the chart refuses anything shorter.

Deploy / upgrade:

```sh
helm upgrade --install product-content charts/product-content \
  --namespace product-content --create-namespace \
  --set database.existingSecret=product-content-db \
  --set kafka.brokers=kafka.kafka.svc.cluster.local:9092 \
  --set config.offerAvailabilityConsumerGroup=product-content-offer-availability
kubectl -n product-content rollout status deployment/product-content
```

### A2A agent

`a2a.enabled=true` adds a second Deployment `<release>-a2a`, a ClusterIP
Service (port 8093) and an optional Gateway route (`a2a.route.enabled`, path
`/api/a2a/product-content`). It needs only `DATABASE_URL`
(+ `MIGRATIONS_DATABASE_URL`) and `A2A_AGENT_URL`, which defaults to the
release's in-cluster Service DNS `http://<release>-a2a.<ns>.svc.cluster.local:8093/`;
override `a2a.agentUrl` with the gateway URL when the agent must be reachable
off-cluster. Kafka variables are never rendered into it. All three probes use
`GET /healthz`. It applies the embedded migrations on start like the API and
reads the data the API writes, so deploy the API in the same release.

```sh
curl -s http://<a2a-host>:8093/.well-known/agent-card.json
```

## Rollback

```sh
helm history product-content -n product-content
helm rollback product-content <revision> -n product-content
kubectl -n product-content rollout status deployment/product-content
```

The boot migrations are forward-only (`0001_product_content_schema` is the only
one; its `.down.sql` exists for the up/down/up integration test, never for a
deploy). Before rolling back across a release that added a migration, confirm
the older binary tolerates the newer schema (columns are additive fleet-wide).
The consumer group is stable, so rolled-back pods resume from the committed
offsets, with no replay storm.

## Health and readiness

- `GET /healthz`: liveness only (startup + liveness probes of the API; all probes of the A2A agent).
- `GET /readyz` (API only): 200 only while Postgres answers a ping AND a Kafka
  broker answers a topic-less metadata request (verdict cached 3s, check
  bounded to 2s). A failed dependency is `503` with a problem document naming
  `postgres` or `kafka`. It flips to 503 first during shutdown, before the
  listener drains. Topics are not part of readiness.

```sh
kubectl -n product-content port-forward svc/product-content 8080:80
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

The startup probe allows about 60 s (`periodSeconds: 2` x `failureThreshold:
30`): migrations and the first Postgres ping retry 5 times with 1, 2, 4 and 8 s
of backoff (`internal/bootretry`), so a sidecar still warming up does not kill
the pod. A pod that exits non-zero on its own means the relay or the consumer
died. The process exits deliberately so the orchestrator restarts it: look at
`kubectl -n product-content logs deploy/product-content --previous` for the
worker name in the final line (`<worker> stopped unexpectedly: exiting so the
orchestrator restarts the service`).

### Shutdown order

SIGTERM: (1) `/readyz` flips to 503, (2) wait `SHUTDOWN_DRAIN_DELAY`, (3) drain
HTTP (10 s), (4) stop and await the consumer (10 s), (5) stop and await the
relay last (10 s) so nothing committed is stranded, (6) flush telemetry (5 s),
close the pool. The A2A agent only drains HTTP (10 s).

## Migrations

Embedded in the binary (`internal/adapters/outbound/postgres/migrations/`),
applied by golang-migrate at boot of **both** binaries against
`MIGRATIONS_DATABASE_URL` (a direct, session-mode connection because the
advisory lock does not survive a transaction-mode pooler; if the pooled DSN
goes through one, keep the direct key populated or migrations hang). Concurrent
boots are safe (the lock serialises them). Tables: `product_contents`,
`category_trees`, `catalog_item_view`, `processed_events` (inbox),
`idempotency_keys`, `outbox_events`. The service connects with its own
least-privilege role over these tables only; pool of 10 connections and
`statement_timeout = 5s`.

## Kafka

One broker platform-wide (`localhost:9092` from the kind cluster host). The
service never creates topics through an admin API: its writers set
`AllowAutoTopicCreation`.

| Topic | Direction | Notes |
| --- | --- | --- |
| `retailnetwork.product-content.events` | produced (outbox relay) | `ProductContentPublished`, `ProductContentRetired`, `CategoryTreePublished` (CloudEvents 1.0, structured mode, key = subject; [ADR 0004](/docs/adr/0004-cloudevents-envelope-and-type-catalogue)) |
| `retailnetwork.offer-availability.events` | consumed | `CatalogItemCreated`, `CatalogItemDiscontinued`; consumer group = `OFFER_AVAILABILITY_CONSUMER_GROUP` (chart suggestion `product-content-offer-availability`) |
| `retailnetwork.offer-availability.events.dlq` | dead letters | written by this consumer; headers `x-dlq-source-topic`, `x-dlq-source-partition`, `x-dlq-source-offset`, `x-dlq-error`, `x-dlq-failed-at` |

There is no `retailnetwork.product-content.analytics` topic and no analytics
event.

### Consumer behaviour

- At-least-once with an atomic effect: the inbox claim on the CloudEvents `id`
  (`processed_events`) and the effect commit in one transaction; the offset is
  committed only after handling returned nil.
- A message that is not a CloudEvents 1.0 event, or has an unknown `type`, is
  logged at WARN and committed past.
- A payload that can never be applied (invalid `catalogItemId`) is dead-lettered
  at once. A transient failure is retried with 200 ms doubling to a 5 s cap and
  dead-lettered after 5 attempts. A failed dead-letter publish is retried
  forever (the partition waits rather than dropping data).
- A stalled or reset broker connection is retried up to 10 times in a row, then
  the consumer fails and the process exits for a restart.

### Re-driving the DLQ

Fix the cause (usually a bad payload from the producer), then republish the
dead-lettered value to `retailnetwork.offer-availability.events` with a tool
such as `kcat`, using the original key and headers minus `x-dlq-*`. A
redelivered event with the same CloudEvents `id` is skipped by the inbox, so
give a repaired event a new `id`.

```sh
kcat -C -b localhost:9092 -t retailnetwork.offer-availability.events.dlq -o beginning -e -f '%k | %h | %s\n'
```

## Outbox relay

`cmd/api` runs the relay always: it drains `outbox_events` in `id` order, 100
rows per pass, sleeping `OUTBOX_RELAY_INTERVAL` (1 s) after a pass that did not
fill a batch. Rows are claimed with `FOR UPDATE SKIP LOCKED`, so two replicas
never send the same row; a row is marked published only after the broker
acknowledged it (acks=all). A failure increments `attempts` and stores
`last_error`. After `OUTBOX_MAX_ATTEMPTS` failures (20) the row is excluded from
claims, one ERROR line with its `outbox_id`, `topic`, `event_type` and `key` is
logged, and later rows overtake it. Delivery is at-least-once; the CloudEvents
`id` is minted once and stored with the row, so consumers de-duplicate.

```sql
-- stuck / excluded rows
SELECT id, event_type, subject, attempts, last_error, created_at
  FROM outbox_events WHERE published_at IS NULL ORDER BY id LIMIT 20;
-- re-arm after fixing the cause
UPDATE outbox_events SET attempts = 0 WHERE id = <id>;
```

## Sweepers

None. The service starts no sweeper or scheduler: the only background loops are
the outbox relay and the offer-availability consumer. In particular nothing
deletes expired `idempotency_keys` rows (an expired record is ignored on read
and replaced on the next write of the same key after 24 h) or old
`processed_events` and published `outbox_events` rows, so those tables only
grow; prune them by hand when needed (see [Troubleshooting](/docs/operations/troubleshooting)).

## Routine procedures

| Task | How |
| --- | --- |
| Check backlog of unpublished events | `SELECT count(*) FROM outbox_events WHERE published_at IS NULL;` |
| Check consumer lag | `kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group product-content-offer-availability` |
| Raise log detail | set `config.logLevel=debug` and roll the Deployment |
| Look at a failing request | take the `request_id` or `trace_id` from the client/`http request` log line and search Loki `{app="product-content"}` |
| Make the A2A agent reachable off-cluster | `a2a.enabled=true`, `a2a.route.enabled=true`, set `a2a.agentUrl` to the public URL |
| Scale the API | `replicaCount` or `autoscaling.api.enabled`; N>1 is safe for the relay (row claims) and the consumer (stable group, inbox) but has not been load-tested |

## Dashboards and telemetry

Logs are stdout JSON (level from `LOG_LEVEL`) collected by the cluster log
pipeline. The Grafana dashboard `warehouse-product-content` and the alert
rules live in `warehouse-infra`; details and the full instrument list are in
[Observability](/docs/operations/observability). The alert annotations in
`warehouse-infra` still link to this runbook at its old path
`docs/operations/runbook.md` on `develop`.

Before the first release to `main`, see
[Before the first release](/docs/operations/before-first-release).
