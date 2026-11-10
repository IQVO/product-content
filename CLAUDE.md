# CLAUDE.md — product-content

`product-content` is the **Product Content** bounded context (Generic subdomain,
`storefront` tier) of the retail-network storefront domain (GitHub org `IQVO`,
Go module `github.com/IQVO/product-content`). It enriches a network catalogue
item with everything a product page needs and the item master does not have:
description, bullets, attributes, media (URLs only, no upload) and the ONE live
category tree. The title stays the item master's. It owns the `ProductContent`
and `CategoryTree` aggregates and ships two binaries: `cmd/api` (REST, the
outbox relay, the offer-availability consumer and the RetireOnDiscontinue
policy in one process) and `cmd/a2a` (read-only Agent2Agent agent, ADR 0006).

## Hard rules (each one is fitness-tested or decided in an ADR)

1. **Hexagonal dependency rule.** `internal/domain` imports nothing outside the
   domain; `internal/application` only domain and application; inbound and
   outbound adapters never import each other; only `cmd/` wires layers
   (`TestHexagonalArchitecture`, `make arch-test`). No JSON tags, SQL or HTTP
   types in the domain.
2. **CloudEvents 1.0 is MANDATORY** on every Kafka message produced or consumed
   (structured mode, no flat envelope, no dual-write, no envelope toggle:
   `TestCloudEventsOnly`, `TestNoEventEnvelopeToggleOrFlatEnvelope`). Build and
   decode only through `internal/adapters/kafka/cloudevents/`. Types are
   `com.retailnetwork.storefront.product-content.<aggregate>.<Event>`; a
   breaking payload change is a new `.v2` type. Catalogue: ADR 0004, payloads:
   `apis/asyncapi.yaml` (`TestEventCatalogueMatchesContract`).
3. **Events leave through the transactional outbox only**: the aggregate row and
   its encoded events commit in ONE unit of work; the relay in `cmd/api`
   publishes to `retailnetwork.product-content.events`. Never write to Kafka
   from a handler or a use case. Every relay passes an attempt cap
   (`OUTBOX_MAX_ATTEMPTS`, `TestOutboxRelaysPassAttemptCap`).
4. **Ordering is a published contract.** Content events of one item share the
   Kafka key (catalogItemId) and carry an increasing `revision`;
   `CategoryTreePublished` carries the tree `version` and the WHOLE tree.
   Consumers apply only a revision/version above the last one they applied.
5. **Consumed facts become a local read model** (ADR 0003): `CatalogItemCreated`
   and `CatalogItemDiscontinued` from `retailnetwork.offer-availability.events`
   feed `CatalogItemView` through the inbox (dedupe on CloudEvents `id`); a
   poison message goes to `<topic>.dlq`, a cancelled context never does
   (`TestDeadLetterCallsGuardCancellation`). The consumer group comes from the
   mandatory `OFFER_AVAILABILITY_CONSUMER_GROUP`, never a literal
   (`TestKafkaConsumerGroupNeverHardcodedInline`).
6. **Storefront boundary**: this service has no outbound HTTP client. It
   consumes only the retail network's published `retailnetwork.*` events and
   never calls an in-org or restricted network route (ADR 0001).
7. **Every POST is behind the `Idempotency-Key` middleware** (missing = 400
   `idempotency-key-required`, same key with another body = 409
   `idempotency-key-reused`, replay marked `Idempotency-Replayed: true`); PUT and
   DELETE are idempotent by method. A concurrent write answers 409
   `concurrent-update`. Errors are RFC 9457 `application/problem+json`
   (`internal/adapters/inbound/http/errors.go`). REST guideline: ADR 0007.
8. **No auth layer on REST or A2A** (fleet-wide revert 2026-09-11): never add
   bearer/JWT/API-key middleware (`TestNoAuthMiddlewareReintroduced`).
9. **The A2A agent is read-only** with no outbound client: only the read use
   cases may be referenced from `internal/adapters/inbound/a2a` and `cmd/a2a`
   (`TestA2AAgentIsReadOnlyWithNoOutboundClient`). Facts stay CloudEvents.
10. **Real infrastructure only**: `cmd/*` never imports the in-memory adapters
    (`TestRuntimeBinaryHasNoInMemoryAdapters`); Kafka and Postgres integration
    tests use testcontainers. No Go file at the repo root
    (`TestNoGoFilesAtRepoRoot`).
11. **An ADR comes first** for a new consumed or published contract, a new
    attribute family or any auth. ADRs live in `docs/adr/`.

## Commands

```bash
make check-fast   # fmt-check + vet + arch-test + tests of changed packages: run before saying "done"
make check        # fmt-check vet build lint test
make check-all    # check + coverage (90% gate on domain+application) + arch-test + bdd
make bdd          # godog scenarios in features/ (runner: internal/bdd)
make integration  # real Postgres/Kafka via testcontainers (needs Docker)
make mutation     # gremlins on each internal/domain package (CI job mutation-fast, blocking)
make guide-lint   # these guides: skills load, references resolve, context budget
```

Run locally: `DATABASE_URL=... KAFKA_BROKERS=... OFFER_AVAILABILITY_CONSUMER_GROUP=... go run ./cmd/api`
(REST `:8080`, migrations at boot, a missing variable is a boot error);
`DATABASE_URL=... A2A_AGENT_URL=... go run ./cmd/a2a` (`:8093`, no Kafka).

## Where things are written down (read before editing the matching area)

- `.claude/rules/domain-model.md` (always loaded): ubiquitous language,
  aggregates, events, use cases. `.claude/rules/rest-api.md`: every route and
  the error slugs. `.claude/rules/integration-events.md`: published and
  consumed events, the outbox, the type table.
- `.claude/rules/fleet/*.md`: fleet-wide rules copied from the harness
  template (CloudEvents, A2A, idempotency and outbox, Kafka testing, no auth);
  never edit them here.
- `docs/adr/`: decisions (index: `docs/adr/index.md`). `apis/openapi.yaml`,
  `apis/asyncapi.yaml`: contracts. Operating docs: `docs/docs/operations/`
  (configuration, runbook, observability, troubleshooting),
  `docs/docs/development/testing.md`, `docs/docs/overview/quickstart.md`.
  `HARNESS.md`: what each sensor runs and why. `INCIDENTS.md`: the fleet
  incident log behind the fitness tests.

harness-template: v4
(hooks, guide-lint).

<!-- harness:scoped-rules:start (generated by tools/migrate_v3.py in warehouse-harness-template; do not hand-edit) -->
## Scoped rules and harness

Claude Code loads each rule below automatically when you touch the matching paths. OpenCode and Codex do NOT: read the rule BEFORE editing matching files.

| When touching | Read |
|---|---|
| `internal/adapters/**/kafka/**`, `internal/adapters/outbound/outbox/**`, `apis/asyncapi*` | `.claude/rules/integration-events.md` |
| `internal/adapters/inbound/http/**`, `apis/openapi*.yaml`, `apis/openapi/**` | `.claude/rules/rest-api.md` |

Hooks (`scripts/harness/hook.py`, wired for Claude Code, Codex and OpenCode) block pushes to develop/main, `--no-verify`, bare `rm -rf`, and edits to generated files, and feed gofmt/vet findings back after each edit. Before saying "done" run `make check-fast`; the full gate is `make check-all`. `HARNESS_OFF=1` disables the hooks when debugging the harness itself.
<!-- harness:scoped-rules:end -->
