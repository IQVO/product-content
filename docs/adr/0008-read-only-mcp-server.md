# ADR 0008: A read-only MCP server

## Status

Accepted (2026-11-14). Follows [ADR 0006](./0006-a2a-inbound-adapter.md) (the A2A agent,
which named "no MCP surface" as the reason it is read-only) and
[ADR 0005](./0005-fleet-observability-standard.md) (the HTTP and telemetry wiring
`cmd/mcp` reuses). Applies the fleet Agentic Standard v2 (warehouse-docs
`docs/architecture/agentic-standard.md`: AS-1 authority levels, AS-3 complete surface,
AS-4 stateless handler, AS-9 "the storefront contexts that have an A2A agent but no MCP
server gain one") and the fleet decision of one MCP server per bounded context, as an
additive inbound adapter, on the official MCP Go SDK, over Streamable HTTP only, as a
separate `cmd/mcp` binary.

## Context

product-content has a REST API and a read-only A2A agent but no MCP server, so an
agent that wants a catalogue item's content has to go through A2A delegation or a
bespoke REST client. The fleet standard decides that every storefront context with an
A2A agent also exposes its reads as MCP tools; this is not a question for this
context. The two reads that already exist as use cases behind a REST `GET` and an A2A
skill are `GetProductContent` and `ListCategories`. The paged `GET /product-contents`
is the projection bootstrap of product-discovery (`ListProductContents`), a data-sync
endpoint that is not part of a user request path.

## Decision

- **A second driving adapter, a separate binary.** `internal/adapters/inbound/mcp` is
  built on the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`) and
  served over Streamable HTTP only (no stdio, no SSE) by `cmd/mcp`, mounted at `/mcp`
  and `/`, with an open `GET /healthz`. The handler is **stateless** (AS-4): no
  `Mcp-Session-Id` is issued and nothing relies on cross-call session state. The
  binary ships in the same image as `/app/mcp` (port **8090**); the Helm chart gets an
  `mcp` Deployment and Service (default **off**) and an optional gateway route at
  `/api/mcp/product-content`.
- **Two read tools, each a thin wrapper over a use case that already backs a REST GET
  and an A2A skill.** Arguments and results are lowerCamelCase like the REST contract.
  Every tool is annotated read-only, idempotent and closed-world, and declares
  authority **L1** (a pure read of this context's own state) under the tool metadata
  key `com.warehouse/authority`.

  | Tool | Input | Authority | Backing use case | REST counterpart | A2A counterpart |
  |---|---|---|---|---|---|
  | `get_product_content` | `catalogItemId` (required) | L1 | `usecases.GetProductContent` | `GET /product-contents/{catalogItemId}` | skill `get_product_content` |
  | `list_categories` | none | L1 | `usecases.ListCategories` | `GET /categories` | skill `list_categories` |

  Like REST, `get_product_content` also answers DRAFT and RETIRED content: MCP does not
  widen or narrow what the REST read exposes. An id that can never name a resource
  (over 128 characters, or carrying a NUL) is `not-found` without reaching the
  repository, as the REST route and the A2A skill do; a blank id is `invalid-argument`.
- **A complete surface (AS-3): tools plus a resource plus a prompt.**
  - Resource template `warehouse://product-content/contents/{catalogItemId}`: the
    content of ONE catalogue item as JSON, from the same `GetProductContent` use case
    (a scoped read model, never a dump of the table).
  - Prompt `review_product_content(catalogItemId)`: this context's operator procedure
    (read the content, check the publish gate facts, interpret `revision` versus
    `publishedRevision`, know what to escalate). It names tools but never calls them
    and cannot write.
- **Read-only is structural.** The adapter holds `*usecases.GetProductContent` and
  `*usecases.ListCategories` and nothing that can write; `cmd/mcp` wires only the
  content and category-tree repositories into them (no unit of work, no outbox, no
  inbox, no clock, no id generator) and never dials Kafka (no `KAFKA_BROKERS`): the
  relay and the offer-availability consumer run in `cmd/api` only. Tests pin it:
  `TestToolSurface` (exact tool set, budget, read-only annotations, no write verb in a
  tool name), `TestWriteSeamSensorFailsOnWriteSeams` (the adapter never names a write
  use case or a transactional seam), `TestToolRegistryGolden` (the whole advertised
  registry byte for byte), `TestStateless_*` (server/discover, authority in `_meta`,
  legacy initialize), and the architecture tests `TestEveryMCPToolDeclaresAuthority`,
  `TestMCPWriteToolsAreAtLeastL3`, `TestMCPServerOffersResourceAndPrompt`,
  `TestMCPOptionalToolInputsCarryOmitempty` and `TestMCPAdapterDependencyRule` (nothing
  but `cmd/mcp` imports the adapter; the adapter imports no `inbound/http`). The
  integration test boots the real wiring over Postgres, lists the tools, reads what the
  write use cases persisted and proves the reads add no outbox row.
- **No sibling calls, no auth.** The server reads this context's own tables only,
  calls no other context, and has no auth layer of any kind (the fleet-wide revert of
  2026-09-11; `TestNoAuthMiddlewareReintroduced` stays green). Access control is the
  in-cluster ClusterIP boundary.
- **Observability.** `cmd/mcp` reuses the shape of `cmd/api`: `telemetry.go` with
  `service.name` `product-content-mcp` (`OTEL_SERVICE_NAME`), and the router middleware
  order of the fleet standard (RequestID, otelchi, request duration, RequestLogger,
  Recoverer). No new metric is introduced.
- **Boot.** `DATABASE_URL` is mandatory (no in-memory mode); the binary runs the
  idempotent embedded migrations on start (`MIGRATIONS_DATABASE_URL` for a direct DSN)
  inside the same `bootretry` budget as `cmd/api` and `cmd/a2a`. `MCP_ADDR` (default
  `:8090`), `LOG_LEVEL`, `SERVICE_VERSION` and `ENVIRONMENT` complete the environment.
- **A2A authority tags.** The two A2A skills gain the tag `authority:L1` (metadata
  only; the card's skills, ids and behaviour are unchanged) because the fleet's
  authority sensor (`TestA2ASkillsDeclareAuthority`) requires every skill to declare
  its level.

## Consequences

- An agent can read a catalogue item's content and the category tree through MCP
  without REST glue or an A2A delegation.
- Adding a tool is a reviewed act: the tool budget, the curated list, the golden file
  and `docs/docs/mcp/tools.md` change together, as an amendment of this ADR.
- Not in this decision: any write tool (create, revise, publish, retire, discard
  content; create, move, rename, publish categories stay REST operations with the
  `Idempotency-Key` middleware and the transactional outbox), and a paged
  `list_product_contents` tool. The latter is a projection-bootstrap endpoint whose
  opaque cursor lives in the REST adapter (`inbound/http/page`), which the MCP adapter
  must not import; exposing it would mean a second cursor format.
- The infra switch-on (`mcp.enabled: true` in warehouse-infra) and the regeneration of
  the fleet agent catalogue are separate steps outside this repository.
