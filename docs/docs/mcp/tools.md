---
id: tools
title: MCP tools
sidebar_label: MCP tools
sidebar_position: 2
description: "The two read-only MCP tools of product-content, every one declaring authority L1, plus the content resource and the content review prompt, from the pinned tool registry."
---

# MCP tools

`cmd/mcp` serves a read-only [Model Context Protocol](https://modelcontextprotocol.io) server over **Streamable HTTP
only** ([ADR 0008](/docs/adr/0008-read-only-mcp-server)). It is unauthenticated like the rest of the fleet: the
in-cluster ClusterIP boundary is the access control.

| | |
| --- | --- |
| Listen address | `MCP_ADDR`, default `:8090` |
| Endpoint | `/mcp` (also mounted at `/`) |
| Probe | `GET /healthz` returns `200 {"status":"ok"}` |
| Needs | `DATABASE_URL` only; it dials no Kafka and registers no write seam |
| Gateway | `/api/mcp/product-content` (the chart's `mcp.route.path`, off by default), rewritten to `/mcp` |
| Deployed by | the chart behind `mcp.enabled` (default off; the switch-on is a `warehouse-infra` step) |

Source: `cmd/mcp/main.go`, `cmd/mcp/router.go`, `internal/adapters/inbound/mcp/{server,tools,tools_views,errors}.go`.

**How this page was produced.** The list below comes from the pinned tool registry
`internal/adapters/inbound/mcp/testdata/tool_registry.golden.json`, which `TestToolRegistryGolden` compares with the
server's `tools/list` on every test run; it was not captured from a running server. Run `tools/list` against a live
`cmd/mcp` to see the same two tools.

## Annotations and authority

Both tools carry `readOnlyHint: true`, `idempotentHint: true` and `openWorldHint: false`. There is no write tool:
the governance tests in `internal/adapters/inbound/mcp/governance_test.go` (`TestToolSurface` with its write-verb and
write-seam sensors) fail on a write-named tool or a write seam, and `TestMCPAdapterDependencyRule` limits the adapter
to the application and domain layers. A tool call adds no `outbox_events` row.

Every tool also declares its **agent authority level** `L1` (Agentic Standard v2, AS-1): a pure read of this
context's own state. The declaration travels in `tools/list` as `_meta` under the key `com.warehouse/authority`; the
same level is pinned per tool in the golden registry and by the static sensor
`internal/architecture/mcp_authority_fitness_test.go`. Both A2A skills carry the equivalent tag `authority:L1`.

| Tool | Required input | Optional input | Annotation | Authority | Backing use case | REST route | A2A skill |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `get_product_content` | `catalogItemId` | none | read-only | `L1` | `GetProductContent` | `GET /product-contents/{catalogItemId}` | `get_product_content` |
| `list_categories` | none | none | read-only | `L1` | `ListCategories` | `GET /categories` | `list_categories` |

Every input schema is `additionalProperties: false`. A failure is an `isError` tool result whose text is
`<slug>: <detail>` with the REST slug (`not-found`, `validation-failed`) or `invalid-argument` for a blank
`catalogItemId`; an untyped error is logged (`mcp tool failed with an unexpected error`) and answered as
`internal-error: an unexpected error occurred`.

## `get_product_content`

Reads the product content of one catalogue item, exactly as `GET /product-contents/{catalogItemId}` returns it.

- **Input:** `catalogItemId` (string, required, for example `item-123`).
- **Output:** `catalogItemId`, `state` (`DRAFT`, `PUBLISHED`, `RETIRED`), `retiredReason` (`MANUAL`, `DISCARDED`,
  `DISCONTINUED`; present only when retired), `description`, `bullets` (strings), `attributes` (each `name`, `value`
  and an optional `unit`), `media` (each `mediaId`, `url`, `altText`, `role` of `PRIMARY` or `GALLERY`),
  `primaryCategoryId` (omitted when unset), `revision` (the current shape), `publishedRevision` (the revision
  shoppers see, `0` when never published), `createdAt`, `updatedAt`, and `publishedAt` and `retiredAt` (omitted when
  unset). `bullets`, `attributes` and `media` are always arrays, never null.
- **Errors:** an unknown item is `not-found`. An id over 128 characters or carrying a NUL can never name a resource
  and is `not-found` without reaching the repository, as on REST; a blank id is `invalid-argument`.
- Like the REST `GET`, it also answers `DRAFT` and `RETIRED` content: MCP neither widens nor narrows what REST reads.

## `list_categories`

Reads the whole live category tree, exactly as `GET /categories` returns it.

- **Input:** none.
- **Output:** `version` (the tree version, `0` before the first category) and `categories`: each with `categoryId`,
  `parentId` (omitted for a root), `name` and `path` (the names from the root joined with `/`), parents before
  children. Before the first category the answer is version `0` with an empty array.
- **Errors:** none a read can raise beyond `internal-error`.

## Not tools

- **Content and category writes** (create, revise, publish, retire, discard content; create, move, rename and publish
  categories) are REST operations with the `Idempotency-Key` middleware and the transactional outbox. Nothing on this
  server reaches them.
- **`GET /product-contents`** (the paged list behind product-discovery's projection rebuild) is deliberately not a
  tool: it is a data-sync endpoint whose opaque cursor lives in the REST adapter, which the MCP adapter must not
  import ([ADR 0008](/docs/adr/0008-read-only-mcp-server)).

## Transport

The Streamable HTTP handler is **stateless** (Agentic Standard v2, AS-4): no `Mcp-Session-Id` is issued, the server
answers the 2026-07-28 revision's per-request handshake (`server/discover`, no `initialize` needed) and a legacy
`initialize` still answers `200`. Nothing in the server relies on cross-call session state; pinned by the raw-POST
tests in `internal/adapters/inbound/mcp/stateless_test.go`.

## Resource: product content of one catalogue item

`warehouse://product-content/contents/{catalogItemId}` (`application/json`) is the one resource of this server: the
content of ONE catalogue item, built from the same `GetProductContent` read use case as the `get_product_content`
tool and the REST `GET /product-contents/{catalogItemId}`. Bounded by construction: one item, the content's fixed
fields (at most 10 bullets, 50 attributes and 12 media), JSON text, never a dump of a table. A malformed URI, an id
that can never name a resource or an unknown item is an error.

## Prompt: `review_product_content`

The one prompt of this server is the operator procedure for this context: read the content, compare `revision` with
`publishedRevision`, check the publish gate facts (a description, exactly one `PRIMARY` medium and a category) and the
primary category against the tree, and escalate DRAFTs that miss the gate, revisions waiting for a publication and a
primary category missing from the tree to the human content owner. It takes one typed, required argument
(`catalogItemId`); it names tools and never calls them, and nothing it names can write. `prompts/get` returns the
text with the item id interpolated; a missing argument degrades to a `<catalogItemId>` placeholder, never to a hidden
call.
