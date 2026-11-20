---
id: 0009
title: Every problem type has a replay class; a RETRYABLE refusal is not stored
status: Accepted (2026-11-20)
---

# ADR 0009: Every problem type has a replay class; a RETRYABLE refusal is not stored

Date: 2026-11-20 · Fleet decision (owner, 2026-11-18), standard:
`warehouse-harness-template` `.claude/rules/fleet/idempotency-and-outbox.md` and
`docs/idempotency-replay-classes.md`. Supersedes the "2xx and 4xx are stored"
sentence of ADR 0007 item 2 (pointer added there; its Decision text is unchanged).

## Context

ADR 0007 put every `POST` behind the `Idempotency-Key` middleware and stored the
first answer below 500 — a 4xx too — for 24 hours. Most refusals of this service
do not depend on the request alone: the catalogue item is a fact consumed from
`offer-availability` events (ADR 0003), the category tree is one live aggregate
every unkeyed PUT moves, and a lost optimistic-lock race is a coincidence of
timing. A caller that did the right thing and retried the same key got the stale
refusal replayed for 24 hours, exactly the case the key exists for.

## Decision

Each problem type the service can answer on an idempotent route declares exactly
one replay class on its catalogue entry (`problemCatalogue`,
`internal/adapters/inbound/http/errors.go`), next to the status and the slug:

- **FINAL** — the answer is a function of the request alone (or of state that
  only ever moves one way). Stored and replayed.
- **OUTCOME** — the command committed state and then refused; the writes commit
  with the stored answer. **No problem type of this service is one**: every
  keyed use case refuses without committing, because its unit of work joins the
  request's transaction as a nested savepoint that rolls back on the error
  (`postgres/unit_of_work.go` `nested`), and a mapped conflict that aborts the
  transaction is healed back to the handler savepoint, discarding the handler's
  writes (`postgres/idempotency.go` `healAbortedHandler`). In this store a 4xx
  whose handler wrote successfully would commit its writes with the key row, so
  OUTCOME could exist — no current refusal does it.
- **RETRYABLE** — nothing committed and the answer depends on state that can
  change. NOT stored: the whole request rolls back, key row included (the same
  rollback the 5xx branch always did, now triggered by `Record.Release`), the
  key is released and a retry executes again. A different body under a released
  key is a NEW request, not `idempotency-key-reused`.

A 2xx is stored; a 5xx or a panic never is, whatever the class. The middleware
knows no other status range and holds no slug list: `writeError` marks the
capturing writer with the class (`markReplay`) and `capture.record()` sets
`Release` for every answer that is not remembered. A runtime refusal with no
class (an unmapped error is the internal-error 500; a handler that wrote a bare
status) is released like RETRYABLE — the fail-safe direction — and
`TestEveryProblemTypeDeclaresAReplayClass` fails the build-time check on an
unclassified catalogue entry. A replay carries `Idempotency-Replayed: true`;
the 24-hour TTL is unchanged.

What a concurrent duplicate sees when the first request ends RETRYABLE: it
blocks on the key's unique index until the first transaction ends; the rollback
means the row never existed, so the blocked claim inserts it and runs the
handler itself (it never replays the released refusal and never runs alongside
the first); copies queued behind it replay that run's answer.

Outside the replay decision (never stored under a key, classified nowhere):
`idempotency-key-required` / `idempotency-key-invalid` (answered before the
store is entered), `idempotency-key-reused` (a property of the replay
comparison, not an answer the key stores) and `resource-not-found` /
`malformed-request` of the NUL guard (`rejectNUL` runs before the middleware, so
no key is ever recorded for them). `internal-error` is a 5xx and never stored.

## Class table

| Problem type | Class | Status | Reason |
| --- | --- | --- | --- |
| `malformed-request` | FINAL | 400 | the request body alone (`decodeJSON`, `response.go`) |
| `unknown-query-parameter` | FINAL | 400 | GET only (a collection query parameter); classified for completeness |
| `invalid-limit` | FINAL | 400 | GET only |
| `invalid-cursor` | FINAL | 400 | GET only |
| `not-found` | RETRYABLE | 404 | mixed slug: content of the path (another request creates it later) and a category (ids are minted by the caller, `category.go` 222); splitting is a follow-up |
| `content-retired` | FINAL | 409 | retirement is terminal and one-way: every command guards RETIRED (`commands.go` 144, 165, 187), only Retire sets it (215), nothing unretires or deletes, this service owns the state |
| `category-in-use` | RETRYABLE | 409 | never raised (guards a future removal, `category.go` 8); its cause, published content under the category, clears when that content retires |
| `unknown-catalog-item` | RETRYABLE | 422 | the item is a fact learned from `CatalogItemCreated` events (`commands.go` `checkItem`); the read model fills a moment later |
| `item-discontinued` | RETRYABLE | 422 | the same read model, fed by `CatalogItemDiscontinued`; the owning service's facts can change |
| `incomplete-content` | RETRYABLE | 422 | the publish gate is satisfied by other requests' revisions first (`commands.go` `complete`) |
| `unknown-category` | RETRYABLE | 422 | the primary category or the parent must be in the live tree (`commands.go` 28-30), which every category write moves |
| `too-many-bullets` | FINAL | 422 | the body of the request alone (`commands.go` 14-16) |
| `too-many-attributes` | FINAL | 422 | the body of the request alone (`commands.go` 17-19) |
| `too-many-media` | FINAL | 422 | the body of the request alone (`commands.go` 20-22) |
| `category-cycle` | RETRYABLE | 422 | the subtree changes with every move of another request |
| `max-depth` | RETRYABLE | 422 | the depth around the parent changes with every tree write |
| `duplicate-sibling-name` | RETRYABLE | 422 | a sibling rename (an unkeyed PUT) clears the clash |
| `concurrent-update` | RETRYABLE | 409 | the optimistic-lock guard after `transact`'s own attempts (`deps.go`) |
| `illegal-transition` | RETRYABLE | 409 | the aggregate state (DRAFT/PUBLISHED/RETIRED) is moved by other requests |
| `validation-failed` | RETRYABLE | 422 | mixed: request fields (`checkFields`) and the tree's size limit (`category.go` 54); splitting is a follow-up |

## Judgement calls

1. **`content-retired` is FINAL although it reads state**: retirement is
   provably one-way (guards above; no unretire, no delete, this service mints
   the ids), so a retry can never pass it and replaying it is safe — a
   re-execution would answer the same refusal anyway.
2. **`item-discontinued` is RETRYABLE although the read model treats
   discontinuation as terminal** (`MarkActive` never un-discontinues): the fact
   belongs to the item master; pinning our projection of it for 24 hours is the
   bigger risk. When unsure, RETRYABLE (fleet default).
3. **`validation-failed` is RETRYABLE**: one class per slug, and the slug also
   carries `ErrTooManyCategories` (tree size). Plain field validation re-runs on
   retry; splitting the slug is a recorded follow-up.
4. **`not-found` is RETRYABLE**: shared by content and category causes, both
   clearable by later requests. Splitting is a recorded follow-up.
5. **`category-in-use` is RETRYABLE although no code raises it**: the
   model-gap guard it backs (category removal) does not exist yet; unsure
   defaults to RETRYABLE.

## Consequences

- A caller that retries a RETRYABLE refusal under the same key gets a fresh
  execution, not the stale refusal; callers that treated a 409/422 as terminal
  under one key keep working (nothing is added client-side; automation retries,
  people-facing surfaces must surface the refusal — out of scope).
- The repository contract holds both store doubles (memory and Postgres) to the
  release and the waiting-copy semantics; the fleet reference test
  (`idempotency_replay_classes_test.go`, rendered from the harness template)
  proves every entry per class over the real middleware; the two RETRYABLE
  proofs over the real router and Postgres are
  `retryable_replay_integration_test.go` (concurrent-update via a trigger that
  drops the version-guarded UPDATE, and unknown-catalog-item via the item
  arriving after the refusal).
- Open point: expired `idempotency_keys` rows are only made inert (TTL checked
  at read and at claim takeover), nothing deletes them; the fleet retention
  sweep (order-sourcing #84 shape) is a follow-up.
