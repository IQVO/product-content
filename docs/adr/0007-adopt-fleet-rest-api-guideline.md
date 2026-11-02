# ADR 0007: Adopt the fleet REST API guideline

## Status

Accepted (2026-11-01). Amends [ADR 0002](./0002-product-content-assumptions.md) A6, A12
and A13 (routes, listing, idempotency). Mirrors the fleet "Warehouse REST API
Guideline" (warehouse-docs).

## Context

The fleet measured divergent REST conventions: three command-URL styles (`:verb`
custom methods, verb sub-paths, sub-resources), two spellings of the replay header,
key reuse answered `422` in most services and `409` in one, and unpaginated lists.
This service had `:revise`, `:publish`, `:retire`, `:discard`, `:move`, `:rename`
custom methods, `Idempotent-Replayed`, `422 idempotency-key-reused`, a per-route key
and an `after`/`next` page.

## Decision

Hard cutover, no aliases, no dual routes, no toggles; enforced by `.spectral.yaml`
(`rest-guideline`, rules R1 to R8) in the `api-lint` job.

1. **Noun routes.**

   | Old | New |
   |---|---|
   | `POST /product-contents/{id}:revise` | `POST /product-contents/{id}/revisions` |
   | `POST /product-contents/{id}:publish` | `POST /product-contents/{id}/publications` |
   | `POST /product-contents/{id}:retire` | `POST /product-contents/{id}/retirements` |
   | `POST /product-contents/{id}:discard` | `DELETE /product-contents/{id}/draft` |
   | `POST /categories:publish` | `POST /category-tree-publications` |
   | `POST /categories/{id}:move` | `PUT /categories/{id}/parent` |
   | `POST /categories/{id}:rename` | `PUT /categories/{id}/name` |

   `operationId`s, status codes and bodies are unchanged. The `PUT` and `DELETE`
   routes are idempotent by method and take no middleware.
2. **Idempotency.** Every `POST` sits behind the key middleware (no `PATCH` exists).
   `Idempotency-Key` is 1 to 128 printable ASCII characters (`400
   idempotency-key-required` when absent, `400 idempotency-key-invalid` when
   malformed). The key is GLOBAL: records are looked up by key alone and compared by
   fingerprint (method + path + SHA-256 of the body); a different fingerprint is `409
   idempotency-key-reused`, a replay carries `Idempotency-Replayed: true` and
   `Idempotent-Replayed` is retired. The key row is written in the SAME transaction
   as the aggregate change and the outbox rows (the Postgres store joins the unit of
   work); 2xx and 4xx are stored for 24 hours, a 5xx or panic rolls back and is not
   stored. The existing `idempotency_keys` table is reused with the `route` column
   held constant (`''`), so no migration is needed.
3. **Pagination.** `GET /product-contents` takes `limit` (1 to 500, default 100) and
   an opaque `cursor` (base64url JSON of the sort key and id) and answers
   `{items, nextCursor}`; keyset order `catalogItemId`, in Postgres and in memory.
   Problems: `400 invalid-limit`, `400 invalid-cursor`, `400 unknown-query-parameter`
   (the retired `after`, `offset`, `nextToken` fail loudly).
4. **Exemption.** `GET /categories` is `x-pagination-exempt: view`: the CategoryTree
   is ONE versioned aggregate (at most four levels), so one response is one consistent
   snapshot of one tree version; paging would let a page straddle two versions.

## Consequences

- Breaking for every client: it deploys together with the REST cutover set of the fleet.
- No in-repo caller needed changing (no web remote, no outbound HTTP client); the
  A2A/MCP adapter exposes `GetProductContent` only.
- The `Idempotent-Replayed` header and `422` reuse answer no longer exist anywhere.
