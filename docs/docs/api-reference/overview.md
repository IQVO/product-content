---
id: overview
title: API overview
sidebar_label: Overview
slug: /api-reference
sidebar_position: 1
---

# API overview

:::info Implemented by cmd/api and cmd/a2a
`cmd/api` serves every REST route below (`internal/adapters/inbound/http`) on
`HTTP_ADDR` (default `:8080`); `cmd/a2a` serves the read-only A2A agent on
`A2A_ADDR` (default `:8093`). The contracts are `apis/openapi.yaml` and
`apis/asyncapi.yaml`, pinned by [ADR 0001](/docs/adr/0001-product-content-bounded-context),
[ADR 0002](/docs/adr/0002-product-content-assumptions) and
[ADR 0004](/docs/adr/0004-cloudevents-envelope-and-type-catalogue). The router
registers the eleven API routes of the spec plus `/healthz` and `/readyz`, which
the spec does not list. See the [Quickstart](/docs/overview/quickstart) for real
responses.
:::

| Interface | Source of truth | Notes |
| --- | --- | --- |
| REST | `apis/openapi.yaml` (OpenAPI 3.0.3, version 0.1.0) | The pages under *REST* are generated from the spec with `npm run gen-api-docs`; do not edit them by hand. The spec names one server, `http://localhost:8080`; through Kong in the kind cluster the base path is `http://localhost:8000/api/product-content` (the prefix is stripped). |
| Events | `apis/asyncapi.yaml` (AsyncAPI 3.0.0, version 1.0.0) | Summarised on the [event catalogue](/docs/api-reference/events); every field on [Domain events](/docs/ddd/domain-events). |
| MCP | none | There is no `cmd/mcp`: this context has no MCP server and no MCP tool. |
| A2A | [ADR 0006](/docs/adr/0006-a2a-inbound-adapter), `internal/adapters/inbound/a2a` | `cmd/a2a` serves JSON-RPC 2.0 on `/` and `/a2a` (port 8093; through Kong `/api/a2a/product-content`), the Agent Card at `/.well-known/agent-card.json`, `GET /healthz` for probes. Two read-only skills, no write skill: `get_product_content` and `list_categories`. No auth, by fleet decision. |

## Endpoints

| Tag | Endpoint | Operation id | Success |
| --- | --- | --- | --- |
| ProductContents | `POST /product-contents` | `createProductContent` | `201`, the DRAFT content at revision 1 |
| ProductContents | `GET /product-contents` | `listProductContents` | `200 {items, nextCursor?}`, every state, ordered by `catalogItemId`; `cursor`, `limit` 1 to 500 (default 100) |
| ProductContents | `GET /product-contents/{catalogItemId}` | `getProductContent` | `200`, the content; `404 not-found` |
| ProductContents | `POST /product-contents/{catalogItemId}/revisions` | `reviseProductContent` | `200`, the revised content at its new revision |
| ProductContents | `POST /product-contents/{catalogItemId}/publications` | `publishProductContent` | `200`, the content as PUBLISHED; unchanged and silent when the current revision is already published |
| ProductContents | `POST /product-contents/{catalogItemId}/retirements` | `retireProductContent` | `200`, the RETIRED content |
| ProductContents | `DELETE /product-contents/{catalogItemId}/draft` | `discardProductContentDraft` | `200`, the DRAFT retired with reason `DISCARDED` |
| Categories | `POST /categories` | `createCategory` | `201`, the category (id minted by the service) |
| Categories | `GET /categories` | `listCategories` | `200`, the tree version and every category; version 0 and empty before the first one |
| Categories | `POST /category-tree-publications` | `publishCategoryTree` | `200`, the live tree republished under a new version |
| Categories | `PUT /categories/{categoryId}/parent` | `moveCategory` | `200`, the moved category |
| Categories | `PUT /categories/{categoryId}/name` | `renameCategory` | `200`, the renamed category |
| Health | `GET /healthz`, `GET /readyz` | none (not in the spec) | `200 {"status":"ok"}` / `200 {"status":"ready"}`, or `503 {"status":"not_ready"}` once shutdown has started or Postgres or Kafka does not answer; see the [Runbook](/docs/operations/runbook) |

Source: `internal/adapters/inbound/http/server.go` (`NewRouter`),
`handlers.go`, `handlers_category.go`, `readiness.go`.

## Conventions in the contract

- **Noun routes.** Lifecycle actions are `POST` transition resources named by a plural noun
  after the id (`{catalogItemId}/publications`); a draft discard is `DELETE .../draft`; a
  category move or rename is `PUT .../parent` or `PUT .../name`. The body of `/revisions` is the whole
  editable content; `/publications`, `/retirements` and `DELETE .../draft` take no body. One
  `/revisions` route serves both states: the state picks `ReviseDraft` or
  `ReviseContent` ([Use cases](/docs/ddd/use-cases)).
- **`Idempotency-Key`.** Required on every `POST` (1 to 128 printable ASCII characters; `400
  idempotency-key-required` when absent, `400 idempotency-key-invalid` when malformed). Global: remembered 24 hours by key alone, compared by method + path + body hash, in
  `idempotency_keys`: the same key with the same request replays the stored
  response, marked with the header `Idempotency-Replayed: true`; the same key with a
  different request is `409 idempotency-key-reused`. Every response below 500 is
  stored, 4xx included, so retry a failed request with a fresh key. Implemented by
  `internal/adapters/inbound/http/idempotency.go`.
- **Paging.** `GET /product-contents` takes `limit` (1 to 500, default 100) and an opaque `cursor`; the response is `{items, nextCursor}` and `nextCursor` is absent on the last page. Any other query parameter is `400 unknown-query-parameter`. Ordered by `catalogItemId`; every
  state is listed.
- **Revisions and versions.** `revision` is the content shape number (1 at
  creation, plus one per revise); `publishedRevision` is the last published one (0
  = never). The tree carries one `version`, which grows with every category write
  and every `POST /category-tree-publications`.
- **Errors** are RFC 9457 `application/problem+json` with
  `type = /problems/<slug>` (a relative reference). Slugs: `malformed-request`,
  `not-found`, `content-retired`, `category-in-use` (declared, never raised),
  `unknown-catalog-item`, `item-discontinued`, `incomplete-content`,
  `unknown-category`, `too-many-bullets`, `too-many-attributes`, `too-many-media`,
  `category-cycle`, `max-depth`, `duplicate-sibling-name`, `concurrent-update`,
  `illegal-transition`, `validation-failed`, `idempotency-key-required`,
  `idempotency-key-reused` and `internal-error`. Which error maps to which slug and
  status is on [Use cases](/docs/ddd/use-cases#error-to-problem-mapping), with
  remedies on [Troubleshooting](/docs/operations/troubleshooting).
- **Limits.** Description at most 4000 characters; at most 10 bullets of 1 to 300;
  at most 50 attributes (name 1 to 100, value 1 to 300, unit at most 20); at most 12
  media (absolute http(s) URL up to 2048 characters, alt text 1 to 200, role
  `PRIMARY` or `GALLERY`, unique `mediaId`); a category name 1 to 100 characters
  without `/`; at most 4 levels and 5000 categories. A `catalogItemId` is 1 to 64
  characters of `A-Za-z0-9._:-` starting with a letter or digit.
- **A NUL character** (U+0000) in a path, query or body is a client error, never a
  database failure (`internal/adapters/inbound/http/nul.go`).
- **Auth**: none. The fleet reverted REST and A2A auth on 2026-09-11, and
  `TestNoAuthMiddlewareReintroduced` fails CI if auth middleware returns under
  `internal/adapters/inbound`.
- **No PII.** No route or event carries a title, a price, stock or personal data.

## A2A

`cmd/a2a` is a **message-only** agent built on the official A2A Go SDK. Its Agent
Card (`GET /.well-known/agent-card.json`) advertises one JSON-RPC interface at
`A2A_AGENT_URL`, a required variable that must be the absolute URL clients dial (the
binary refuses to boot without it), no streaming, input and output modes
`application/json` and `text/plain`, and no security scheme.

A skill call is a JSON-RPC message whose first part is a data part (or a text part
holding the same JSON) with the skill id and its arguments:

| Skill | Arguments | Answers |
| --- | --- | --- |
| `get_product_content` | `catalogItemId` (required, at most 128 characters) | The content exactly as `GET /product-contents/{catalogItemId}` returns it: state, description, bullets, attributes, media, primary category, revision and published revision; a `not-found` problem when the item has no content. |
| `list_categories` | none | The tree version and every category with id, parent id, name and path, as `GET /categories`. |

The reply is one agent message with a data part (the envelope with the skill id and
the data, or a problem) and a text part (a one-line summary). Rejections are
**answered, not raised**: a domain or request problem comes back in the message as
a problem with the REST status and one of the slugs `not-found` (404),
`validation-failed` (422), `internal-error` (500), `malformed-skill-request`,
`unknown-skill` or `invalid-argument` (400), never as a JSON-RPC error. Unknown
fields in a skill request are refused. Each skill run opens an `a2a.skill <id>`
span. The agent reads Postgres only: it has no Kafka, no relay, no consumer and no
write path (`TestA2AAgentIsReadOnlyWithNoOutboundClient`), and no outbound A2A
client exists. Facts between contexts still travel as CloudEvents on Kafka.

Source: `internal/adapters/inbound/a2a/server.go`, `skills.go`, `mapping.go`,
`cmd/a2a/router.go`.
