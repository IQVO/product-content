---
paths:
  - "internal/adapters/inbound/http/**"
  - "apis/openapi*.yaml"
  - "apis/openapi/**"
---

# REST API (inbound adapter)

Every route -> use case mapping, kept in sync with `apis/openapi.yaml` (routes live
in `internal/adapters/inbound/http/server.go`). This file is documentation for an
agent, not generated: keep it manually accurate.

- `POST /product-contents` -> `CreateProductContent`
- `GET /product-contents` -> `ListProductContents` (paged, `after` / `limit`)
- `GET /product-contents/{catalogItemId}` -> `GetProductContent`
- `POST /product-contents/{catalogItemId}:revise` -> `ReviseContent` (ReviseDraft for a DRAFT, ReviseContent for PUBLISHED)
- `POST /product-contents/{catalogItemId}:publish` -> `PublishContent`
- `POST /product-contents/{catalogItemId}:retire` -> `RetireContent`
- `POST /product-contents/{catalogItemId}:discard` -> `DiscardDraft`
- `POST /categories` -> `CreateCategory`
- `GET /categories` -> `ListCategories`
- `POST /categories:publish` -> `PublishCategoryTree`
- `POST /categories/{categoryId}:move` -> `MoveCategory`
- `POST /categories/{categoryId}:rename` -> `RenameCategory`
- `GET /healthz` (liveness) and `GET /readyz` (drain gate + Postgres ping + Kafka metadata)

## Conventions

- Error shape: RFC 9457 `application/problem+json`, `type` = `/problems/<slug>`
  (catalogue in `errors.go`); every POST needs an `Idempotency-Key` (missing = 400
  `idempotency-key-required`, same key with another body = 422
  `idempotency-key-reused`, replay marked `Idempotent-Replayed: true`); a NUL in the
  path, query or body is a 400 before any key is recorded.
- Auth: none. Fleet-wide auth was deliberately reverted 2026-09-11; do not add
  bearer/JWT/API-key middleware (`TestNoAuthMiddlewareReintroduced`).
