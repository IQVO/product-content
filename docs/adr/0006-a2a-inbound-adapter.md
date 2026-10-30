# ADR 0006: A2A inbound adapter

## Status

Accepted (2026-10-30). Follows [ADR 0004](./0004-cloudevents-envelope-and-type-catalogue.md)
(CloudEvents on every message) and [ADR 0005](./0005-fleet-observability-standard.md)
(the HTTP and telemetry wiring `cmd/a2a` reuses). Mirrors the fleet's A2A rollout
(workforce-management ADR 0039, wes-work-planning ADR 0046, process-path-management
ADR 0039).

## Context

The fleet is adopting the Agent2Agent (A2A) protocol so that a bounded context can
**delegate** a goal to another context's agent. Three transports now carry three
different things and must not overlap:

| Transport | Carries |
|---|---|
| Kafka + CloudEvents | **facts** between contexts ([ADR 0004](./0004-cloudevents-envelope-and-type-catalogue.md)) |
| MCP | an agent's **tools** |
| A2A | **delegation** of a goal to another context's agent |

product-content has **no MCP surface** (`cmd/` holds only `cmd/api`), so there is no
MCP charter to derive skills from and no MCP write for a skill to mirror. Its REST
API is therefore the only existing surface, and the rule "a skill may change state
only if the context's MCP surface already offers that write" leaves exactly one safe
answer: the agent is **read-only**. Delegation must never widen what the context
exposes, and the context's writes (create, revise, publish, retire, discard content;
create, move, rename, publish categories) stay REST-only, with their facts leaving as
CloudEvents through the transactional outbox.

## Decision

Add `internal/adapters/inbound/a2a`, a driving adapter over the **same** read use
cases the REST adapter uses, built on the official `a2aproject/a2a-go/v2` SDK (pinned
`v2.7.0`) and served by a new `cmd/a2a` binary (JSON-RPC 2.0 over HTTP, port **8093**,
the Agent Card at `/.well-known/agent-card.json`, the endpoint mounted at `/` and
`/a2a`, `GET /healthz` for the probes).

### Read-only, two curated skills

| Skill | Kind | What it does | Backed by |
|---|---|---|---|
| `get_product_content` | read | the product content of one catalogue item, exactly as `GET /product-contents/{catalogItemId}` returns it: state, description, bullets, attributes, media, primary category, revisions, timestamps | `usecases.GetProductContent` |
| `list_categories` | read | the whole live category tree, exactly as `GET /categories` returns it: tree version and every category with parent and path; no arguments | `usecases.ListCategories` |

The skills are goal-shaped (what does a product page need for this item; what does
the tree look like), not one per endpoint, and they are the two reads a delegating
agent most needs. `GET /product-contents` (the paged read behind product-discovery's
projection rebuild) is deliberately **not** a skill: it is a data-sync endpoint, and
A2A carries delegation, never data sync. No new behaviour and no new query: the
answers carry the REST response fields and nothing the REST adapter withholds (it
withholds nothing; note that, like REST, `get_product_content` also answers DRAFT and
RETIRED content).

A fitness test (`internal/architecture/a2a_fitness_test.go`) enforces the boundary:
an allowlist over the Go syntax tree of the adapter and `cmd/a2a` accepts only
`usecases.GetProductContent`, `usecases.ListCategories` and the `usecases.Deps`
bundle, and fails on any write use case, unit of work, outbox, inbox, Kafka package
or variable, in-memory double, or outbound A2A client; a second test proves the
detector bites from code fixtures; arch-go rules keep the adapter dependent on
application and domain only and keep everything else off it.

### Message-only, answered in-band

Both skills are transactional request/response reads, so the agent is message-only
(no Task lifecycle). Domain rejections and malformed requests are **answered** as
RFC 7807-shaped `problem`s inside the message envelope, never raised as JSON-RPC
transport errors, in the REST adapter's `/problems/` namespace and with its status
classification for the outcomes a read can have:

| Status | Problem type | When |
|---|---|---|
| `404` | `not-found` | no content for that `catalogItemId` (same slug as REST), or an id that can never name a resource (over 128 characters, or carrying a NUL, which Postgres text cannot hold): answered without reaching the repository, as the REST route does |
| `422` | `validation-failed` | a use case raises `shared.ErrValidation` (same slug as REST; no current read can) |
| `400` | `invalid-argument` | missing or blank `catalogItemId`, a mistyped, unknown or unexpected argument |
| `400` | `malformed-skill-request`, `unknown-skill` | the message is not a skill request, or names a skill the card does not list (including every REST write) |
| `500` | `internal-error` | anything unrecognised; the answer carries no underlying error text (it is logged) |

REST's `409` classes (concurrent update, illegal transition, retired content) belong
to commands and cannot be raised by these reads, so they are not mapped. This
context has no deprecated alias or key for an A2A skill to leak.

### Strict input

Unknown argument names are **rejected**, not ignored (`json.Decoder` with
`DisallowUnknownFields`), and data after the JSON document is rejected. Go's
`encoding/json` matches names case-insensitively, so `catalogitemid` still reaches
`catalogItemId`. A request is a Data part, or a Text part holding the same JSON:
`{"skill":"<id>","arguments":{...}}`. Every skill call runs in an OTel span
`a2a.skill <id>` with the `a2a.skill` attribute.

### Composition root

`cmd/a2a` keeps only what the two reads need: the Postgres pool and the content and
category-tree repositories. It has no outbox, inbox, unit of work, Kafka producer or
consumer, relay or scheduler, so it needs no `KAFKA_BROKERS` and no consumer group;
only `cmd/api` drains `outbox_events` and consumes offer-availability events.
`DATABASE_URL` and `A2A_AGENT_URL` (an absolute `http(s)` URL advertised on the card)
are mandatory and refused at boot with an error naming the variable, before
telemetry, any connection and the listener. Like `cmd/api` it applies the embedded
migrations at boot (idempotent; `MIGRATIONS_DATABASE_URL` is the direct DSN, default
`DATABASE_URL`), so the agent also starts against a database the API has not migrated
yet. There is no in-memory runtime path.

### Unauthenticated, no outbound client

The card declares no security schemes: this surface is unauthenticated by the same
fleet-wide decision as REST (see `.claude/rules/fleet/no-auth-and-mcp.md`). Schemes
are added to the card, not a redesign, if that decision is ever reversed. The adapter
contains no outbound A2A client and calls no other context.

## Consequences

- A second binary (`/app/a2a`) in the same image and an `a2a.*` chart block
  (`<release>-a2a`, default **off**), exposed through the Gateway API at
  `/api/a2a/product-content` rewritten to `/`. The chart tests assert that the
  rendered Deployment carries `A2A_AGENT_URL` (defaulting to the in-cluster Service
  DNS, overridable with `a2a.agentUrl`), `DATABASE_URL` from the Secret, and neither
  `KAFKA_BROKERS`, the consumer group nor the api ConfigMap.
- The agent depends on the api's data: enable it in the same release.
- Port 8093 is the fleet's A2A port (8080 is the api in this chart).
- Delegating *to* this agent does not change who decides: this context defines what a
  product page says and how categories nest, and never decides pricing, availability
  or discovery.
- Not in this decision: a paged `list_product_contents` skill, any write skill (all
  stay REST-only until an MCP charter exists to derive them from), and an outbound
  A2A client.
