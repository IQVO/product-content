---
id: troubleshooting
title: Troubleshooting
sidebar_label: Troubleshooting
sidebar_position: 4
---

# Troubleshooting

Symptom, cause, check, fix, from the real failure modes in the code. Variables
are in [Configuration](/docs/operations/configuration); procedures in the
[Runbook](/docs/operations/runbook); log fields in
[Observability](/docs/operations/observability). Error `type` URIs are
`/problems/<slug>`.

## Boot

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| `cmd/api` exits 1 at once: `DATABASE_URL is required: this context runs on real Postgres, there is no in-memory mode` | `DATABASE_URL` unset | the message names the variable; nothing was opened or bound | set it (chart: `database.url` or `database.existingSecret`). The chart refuses to render without it |
| `KAFKA_BROKERS is required: this context runs on real Kafka (outbox relay, consumers, /readyz)` | unset or only blanks/commas | same | set a comma-separated list (chart: `kafka.brokers`) |
| `OFFER_AVAILABILITY_CONSUMER_GROUP is required: the consumer always runs on real Kafka` | group id unset | same | set a stable id such as `product-content-offer-availability` (chart: `config.offerAvailabilityConsumerGroup`) |
| `cmd/a2a` exits 1: `A2A_AGENT_URL is required ...` or `A2A_AGENT_URL="..." is not an absolute http(s) URL` | missing, relative or schemeless URL | `cmd/a2a/main.go` (`checkAgentURL`); both mandatory problems are reported together | set an absolute URL such as `http://<release>-a2a.<ns>.svc.cluster.local:8093/` |
| Pod stuck before ready, logs `retrying ... op=run migrations` / `ping postgres` then `service exited with error` | Postgres unreachable, wrong DSN, or Istio sidecar not ready; after 5 attempts (1+2+4+8 s) the last error is returned | the `err` field of the `retrying` WARN lines | fix the DSN/network. The startup probe allows about 60 s, so a slow sidecar is tolerated, a dead database is not |
| Boot hangs on `run migrations` | `MIGRATIONS_DATABASE_URL` (or `DATABASE_URL` fallback) goes through a transaction-mode pooler; golang-migrate's session advisory lock cannot be taken | compare the migrations DSN with the pooled one | point `MIGRATIONS_DATABASE_URL` at a direct, session-mode connection (chart: `database.migrationsExistingSecretKey`) |
| `helm template` / install fails naming a value | the chart refuses to render without `database.*`, `kafka.brokers`, `config.offerAvailabilityConsumerGroup`, with a grace period shorter than drain + 30 s, or with a `*_MODE` / `*_CONSUMER_GROUP` in `extraEnv` | the failure message names the value | set the value; see [Runbook](/docs/operations/runbook#deploy) |
| Log line `invalid OUTBOX_RELAY_INTERVAL; using the default` (or `OUTBOX_MAX_ATTEMPTS`, `ignoring invalid SHUTDOWN_DRAIN_DELAY`) | malformed value | the `value` and `default` fields | correct the value. A typo never disables the attempt cap |

## Readiness and liveness

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| `GET /readyz` returns `503` `application/problem+json`, `type` `/problems/dependency-unavailable`, detail `not ready: kafka` (or `postgres`) | the broker did not answer a metadata request, or the Postgres ping failed, within 2 s | WARN `readiness: dependency is not ready` with `dependency` and `error`; verdicts are cached 3 s | restore the dependency. Topics are not checked, only reachability |
| `GET /readyz` returns `503` `{"status":"not_ready"}` | the process received SIGTERM and is draining | pod is terminating | none, expected for `SHUTDOWN_DRAIN_DELAY` |
| Pod restarts; last log `outbox-relay stopped unexpectedly` or `offer-availability stopped unexpectedly: exiting so the orchestrator restarts the service` | the relay or the consumer returned on its own; the consumer exits after 10 consecutive transient fetch failures or on a non-transient reader error | `kubectl logs --previous` for the preceding `consumer stopped` / `outbox relay stopped` ERROR | fix the broker; the restart is by design. Alert `WarehousePodRestarting` fires at 2 restarts in 15 min |
| A2A pod `Ready` but answers fail | `cmd/a2a` probes only `/healthz` (process up), not Postgres | call the agent; look for `a2a skill failed` | restore Postgres |

## REST responses

All errors are RFC 9457 `application/problem+json` (`internal/adapters/inbound/http/errors.go`).

| Status and `type` | Cause | Fix |
| --- | --- | --- |
| `400 idempotency-key-required` | a POST without `Idempotency-Key`, or one longer than 128 characters | send a key of 1 to 128 characters, unique per logical request |
| `409 idempotency-key-reused` | the same key and route with a different method, path, query or body | use a new key for a different request; the same key replays the stored first response (header `Idempotency-Replayed: true`) for 24 h. A 5xx is never stored |
| `400 malformed-request` | invalid JSON, an unknown field, a wrong type, a body over 64 KiB, or a NUL character in path, query or body | fix the request |
| `422 unknown-catalog-item` | no `CatalogItemCreated` for this id has been consumed yet (the local `catalog_item_view` does not know it) | check the item exists in the network, that the consumer is running (`consumer running` log, group lag) and that the event reached `retailnetwork.offer-availability.events`; retry once it is applied |
| `422 item-discontinued` | `CatalogItemDiscontinued` was consumed for the item | none: content is neither created nor published for a discontinued item |
| `422 incomplete-content` | publish needs a description, exactly one `PRIMARY` medium and a category; revising PUBLISHED content must also stay complete | revise the draft to satisfy the gate, then publish |
| `422 unknown-category` | `primaryCategoryId` (or a parent) is not in the live tree | `GET /categories`; create the category first |
| `422 too-many-bullets` / `too-many-attributes` / `too-many-media` | more than 10 bullets, 50 attributes or 12 media | trim the body |
| `422 validation-failed` | a field rule: text must be trimmed and non-blank (description up to 4000, bullet up to 300, attribute name 100 / value 300 / unit 20, alt text up to 200 characters), media URL absolute `http(s)` up to 2048, unique valid `mediaId`, `role` `PRIMARY` or `GALLERY`, ids of 1 to 64 characters from `A-Z a-z 0-9 . _ : -` | the `detail` names the field |
| `409 concurrent-update` | optimistic version mismatch (two writers), or a second create for the same item | re-read and retry |
| `409 illegal-transition` | for example `/retirements` on a DRAFT (use `DELETE .../draft`), `DELETE .../draft` on PUBLISHED (use `/retirements`) | check `state` with `GET /product-contents/{id}` |
| `409 content-retired` | RETIRED is terminal; no revise or publish | create nothing new for that item; there is no un-retire |
| `404 not-found` | no content or category with that id | check the id |
| `422 category-cycle`, `max-depth` (tree at most 4 levels), `duplicate-sibling-name` (case-insensitive), `unknown-category` (unknown parent) | category rules | adjust the tree command |
| `500 internal-error` | any unmapped error; the detail never leaks the cause | search the log by `request_id`/`trace_id` for the ERROR line |

## Events not flowing

| Symptom | Cause | Check | Fix |
| --- | --- | --- | --- |
| `publish` returns 200 but nothing appears on `retailnetwork.product-content.events` | the relay cannot reach the broker, or the row hit the attempt cap | `SELECT id, event_type, attempts, last_error FROM outbox_events WHERE published_at IS NULL ORDER BY id;` and ERROR `outbox relay pass failed` | restore the broker; rows retry every `OUTBOX_RELAY_INTERVAL`. Capped rows: fix the cause, then `UPDATE outbox_events SET attempts = 0 WHERE id = <id>;` (ERROR `outbox relay: row reached the attempt cap ...` names `outbox_id`) |
| A `/publications` of unchanged PUBLISHED content raises no event | by design: the current revision is already published | `revision` equals `publishedRevision` | `/revisions` first, or `POST /category-tree-publications` to re-send the tree |
| Downstream sees the same event twice | at-least-once delivery after a relay retry | same CloudEvents `id` | de-duplicate on `id`; the producer republishes the stored id |
| `unknown-catalog-item` persists although the item exists | the consumer is not running under the expected group, or the group has no committed offsets and is replaying | `consumer running` log: `group_id`, `topic`; `kafka-consumer-groups --describe` | a fresh group starts from the first offset and replays the topic; give it time. Never derive the group from the pod name |
| Messages in `retailnetwork.offer-availability.events.dlq` | `type` known but `catalogItemId` invalid (poison, dead-lettered at once), or a transient failure persisted for 5 attempts | headers `x-dlq-error`, `x-dlq-source-*`; WARN `dead-lettering an event that can never be applied` | fix the producer, re-drive per [Runbook](/docs/operations/runbook#re-driving-the-dlq) |
| Consumer partition seems frozen, ERROR `offer-availability dead-letter publish failed; retrying the same message` repeats | the DLQ publish itself fails (broker down or topic not ready); it is retried forever on purpose | log `error` field | restore the broker; nothing is dropped |
| WARN `skipping a message that is not a valid CloudEvent` | a producer sent the retired flat envelope or non-JSON | `error` field | producer must send CloudEvents 1.0 structured mode with `content-type: application/cloudevents+json` |
| A discontinued item still shows PUBLISHED content | `CatalogItemDiscontinued` not yet consumed | consumer lag | the `RetireOnDiscontinue` policy retires PUBLISHED content (reason `DISCONTINUED`, raises `ProductContentRetired`) and DRAFT content silently when the event is applied |

## A2A

| Symptom | Cause | Fix |
| --- | --- | --- |
| Clients dial the wrong host | the Agent Card advertises `A2A_AGENT_URL` | check `/.well-known/agent-card.json`; set `a2a.agentUrl` to the URL clients can reach |
| JSON-RPC reply is a normal message containing a problem (`unknown-skill`, `malformed-skill-request`, `invalid-argument`, `not-found`) | skill errors are answered **in the message**, never as JSON-RPC errors | read the data part: `skill` must be `get_product_content` or `list_categories`; unknown argument names are refused; `catalogItemId` is required |
| `404 not-found` for an item that exists | the agent reads the same database; the id is over 128 characters or the content was never created | check `GET /product-contents/{id}` on the API |

## Data and capacity

| Symptom | Cause | Fix |
| --- | --- | --- |
| `idempotency_keys`, `processed_events`, published `outbox_events` grow without bound | no sweeper exists (expired idempotency keys are ignored, not deleted) | prune by hand, for example `DELETE FROM idempotency_keys WHERE created_at < now() - interval '7 days';` and `DELETE FROM outbox_events WHERE published_at < now() - interval '30 days';` |
| Telemetry missing | Collector unreachable; export is non-blocking | run with `LOG_LEVEL=debug` for `opentelemetry error`; check `otel.endpoint` |
| Statements cancelled after 5 s | `statement_timeout = 5s` on every pooled connection; pool of 10 | look at Postgres load; the limits are compiled in |
