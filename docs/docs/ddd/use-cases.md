---
id: use-cases
title: Use cases
sidebar_label: Use cases
sidebar_position: 1
---

# Use cases

The application layer is `internal/application/usecases`. Every use case runs
in **one** `ports.UnitOfWork` through `transact`: load the aggregate, call the
domain, save it conditionally on the `version` it loaded, and append the raised
events to the outbox (`ports.EventOutbox`) in the same Postgres transaction.
When another writer got in between the load and the save
(`ports.ErrConcurrentUpdate`) the whole attempt runs again on fresh state: 2
attempts for a command (`commandAttempts`), 5 for a consumed event
(`consumerAttempts`), then the conflict is returned (`deps.go`). The use cases
call no sibling context: what they need to know about a catalogue item comes
from the event-fed `catalog_item_view`.

There are **14 use cases**: 9 commands behind REST, 3 REST queries and 2
consumer effects behind the offer-availability topic. Errors are domain values
that pass through unchanged; the REST adapter maps them to RFC 9457 problems
(see [Error to problem mapping](#error-to-problem-mapping)). The step-by-step
flows are on [Sequence diagrams](/docs/ddd/sequence-diagrams), the event
payloads on [Domain events](/docs/ddd/domain-events), the routes on the
[API overview](/docs/api-reference).

## Commands and queries

Routes are those of `apis/openapi.yaml`; every `POST` needs an
`Idempotency-Key` header (see below). Kafka types are the full CloudEvents
`type` of the producer.

| # | Use case (file) | Trigger | Inputs | Invariants checked | Events raised |
| --- | --- | --- | --- | --- | --- |
| 1 | `CreateProductContent` (`content.go`) | REST `POST /product-contents` | `catalogItemId`, optional `description`, `bullets`, `attributes`, `media`, `primaryCategoryId` | The id is a valid id (1 to 64 characters of `A-Za-z0-9._:-`). The item is in `catalog_item_view` (`ErrUnknownCatalogItem`) and not discontinued (`ErrItemDiscontinued`). At most 10 bullets, 50 attributes, 12 media; field limits (description 4000, bullet 300, attribute name 100 / value 300 / unit 20, alt text 200, URL absolute http(s) up to 2048, media ids unique); `primaryCategoryId`, when given, is in the current tree (`ErrUnknownCategory`). A DRAFT may be incomplete. A second create for the item is `ErrConcurrentUpdate` (primary key on `catalog_item_id`). Result: DRAFT, revision 1. | none |
| 2 | `ReviseContent` (`content.go`) | REST `POST /product-contents/{catalogItemId}:revise` | the whole editable body (it replaces the previous one); a body `catalogItemId` must match the path | One route, two domain commands picked by state (ADR 0002, A4): `ReviseDraft` for a DRAFT, `ReviseContent` for PUBLISHED. RETIRED is `ErrContentRetired`. Same bounds and category check as create. PUBLISHED content must stay complete, else `ErrIncompleteContent`. Takes the next `revision`. | none: shoppers see the new revision only after a publish |
| 3 | `PublishContent` (`content.go`) | REST `POST /product-contents/{catalogItemId}:publish` | the id | Not RETIRED (`ErrContentRetired`); the item is known (`ErrUnknownCatalogItem`) and not discontinued (`ErrItemDiscontinued`); complete: a description, exactly one `PRIMARY` medium, a category (`ErrIncompleteContent`); the category is in the current tree (`ErrUnknownCategory`). PUBLISHED content whose `publishedRevision` equals `revision` has nothing new to publish: the call succeeds, saves nothing and raises nothing (ADR 0002, A1). | `ProductContentPublished` (full content and `revision`), unless there was nothing new |
| 4 | `RetireContent` (`content.go`) | REST `POST /product-contents/{catalogItemId}:retire` | the id | Only PUBLISHED content retires, reason `MANUAL`; a DRAFT or RETIRED content is `ErrIllegalTransition`. `RETIRED` is terminal. | `ProductContentRetired` |
| 5 | `DiscardDraft` (`content.go`) | REST `POST /product-contents/{catalogItemId}:discard` | the id | Only a DRAFT discards, reason `DISCARDED`; anything else is `ErrIllegalTransition`. | none: nothing was published |
| 6 | `CreateCategory` (`category.go`) | REST `POST /categories` | `parentId` (optional, empty is a root), `name` | The service mints the id (`cat-` and 12 hex characters). Name trimmed, 1 to 100 characters, no `/`. Unknown parent is `ErrUnknownParent`. Tree below 5000 categories. Depth at most 4 (`ErrMaxDepth`). Siblings never share a name, compared case-insensitively (`ErrDuplicateSiblingName`). The first create makes the tree (version 1). | `CategoryTreePublished` (whole tree, new version) |
| 7 | `MoveCategory` (`category.go`) | REST `POST /categories/{categoryId}:move` | path id, `parentId` (empty moves to the root) | Category exists (`ErrCategoryNotFound`), parent exists (`ErrUnknownParent`), the parent is not inside the moved subtree (`ErrCategoryCycle`), parent depth plus subtree height at most 4 (`ErrMaxDepth`), no sibling of the same name at the new parent (`ErrDuplicateSiblingName`). Paths of the subtree are recomputed. Moving to the current parent still publishes a new version. | `CategoryTreePublished` |
| 8 | `RenameCategory` (`category.go`) | REST `POST /categories/{categoryId}:rename` | path id, `name` | Category exists, name valid as above, no sibling with the new name. Paths of the subtree follow. | `CategoryTreePublished` |
| 9 | `PublishCategoryTree` (`category.go`) | REST `POST /categories:publish` | none | Changes no category: publishes the live tree again under the next version, the way to re-send it to a consumer. On the empty tree it publishes version 1 with no categories. | `CategoryTreePublished` |
| 10 | `GetProductContent` (`queries.go`) | REST `GET /product-contents/{catalogItemId}`; A2A skill `get_product_content` | the id | Unknown id is `ports.ErrNotFound`. Returns every state, retired content included. | none |
| 11 | `ListProductContents` (`queries.go`) | REST `GET /product-contents` | optional `after` (last id of the previous page), `limit` | `limit` 1 to 500, default 100 (`MaxPageSize`, `DefaultPageSize`); ordered by `catalogItemId`; `next` is the cursor and absent on the last page. All states are listed: this is the page `product-discovery` walks to rebuild its projection. | none |
| 12 | `ListCategories` (`queries.go`) | REST `GET /categories`; A2A skill `list_categories` | none | Before the first category the answer is version 0 and an empty list. | none |
| 13 | `Consumer.ApplyCatalogItemCreated` (`consume.go`) | Kafka `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated` | `catalogItemId` from `data` | Valid id, else `ErrValidation` (a poison message, dead-lettered at once). The inbox claims the CloudEvents `id` first (a repeat is acknowledged and applied zero times). The item is recorded `ACTIVE` unless the view already knows it: a discontinued item is never revived. | none |
| 14 | `Consumer.ApplyCatalogItemDiscontinued` (`consume.go`) | Kafka `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued` | `catalogItemId` from `data` | Same id and inbox rules. Marks the item `DISCONTINUED`, then runs the policy **RetireOnDiscontinue** on its content: no content, nothing; PUBLISHED retires (reason `DISCONTINUED`); a DRAFT retires silently with the same reason; RETIRED is left alone. Save and outbox row in the same unit of work. | `ProductContentRetired` for PUBLISHED content only |

The consumer ignores other event types on the topic and skips a message that is
not a CloudEvents 1.0 document with a warning (`inbound/kafka/consumer.go`).
The retry and dead-letter behaviour is on
[Integration](/docs/ecosystem/integration#failure-behaviour).

## Domain operations behind them

The use cases call these aggregate operations; the invariants live in the
domain packages, not in the adapters.

| Operation | State change | Rule |
| --- | --- | --- |
| `content.Create(id, body, item, cats, now)` | none to `DRAFT`, revision 1, version 1 | Valid id, item `ItemActive`, body valid. |
| `ProductContent.ReviseDraft` | `DRAFT` to `DRAFT` | New revision. A PUBLISHED content here is `ErrIllegalTransition`. |
| `ProductContent.ReviseContent` | `PUBLISHED` to `PUBLISHED` | New revision, body must stay complete. A DRAFT here is `ErrIllegalTransition`. |
| `ProductContent.Publish` | `DRAFT` or `PUBLISHED` to `PUBLISHED` | `publishedRevision` takes `revision`, `publishedAt` is set, version grows, event raised. |
| `ProductContent.Retire` | `PUBLISHED` to `RETIRED` (`MANUAL`) | Raises `ProductContentRetired` with the revision at retirement. |
| `ProductContent.DiscardDraft` | `DRAFT` to `RETIRED` (`DISCARDED`) | No event. |
| `ProductContent.RetireOnDiscontinue` | `PUBLISHED` or `DRAFT` to `RETIRED` (`DISCONTINUED`) | Event only from PUBLISHED. |
| `Tree.CreateCategory`, `MoveCategory`, `RenameCategory`, `PublishCategoryTree` | version + 1 | Every command applies to the one live tree and publishes it: there is no draft tree. |

Source: `internal/domain/content/content.go`, `commands.go`,
`internal/domain/category/category.go`.

## Idempotency

All nine `POST` routes sit behind `RequireIdempotencyKey`
(`internal/adapters/inbound/http/idempotency.go`, route names in `server.go`).
The key is 1 to 128 characters and is remembered 24 hours per route and key in
`idempotency_keys`. A repeated key with the same body replays the stored
response (marked `Idempotent-Replayed: true`); with a different body it is `422
idempotency-key-reused`. A missing key is `400 idempotency-key-required`. Every
response below 500 is stored, client errors included, so a client that retries
after a 4xx needs a fresh key; a 5xx is not stored (ADR 0002, A13). Consumed
events are made idempotent by the inbox (`processed_events`), not by this key.

## Error to problem mapping

`internal/adapters/inbound/http/errors.go` maps each error, in match order, to
`application/problem+json` with `type` `/problems/<slug>`:

| Error | Slug (status) |
| --- | --- |
| malformed JSON, unknown field, bad type | `malformed-request` (400) |
| `ports.ErrNotFound`, `category.ErrCategoryNotFound` | `not-found` (404) |
| `content.ErrContentRetired` | `content-retired` (409) |
| `category.ErrCategoryInUse` | `category-in-use` (409): declared for a future category removal, no route raises it |
| `content.ErrUnknownCatalogItem` | `unknown-catalog-item` (422) |
| `content.ErrItemDiscontinued` | `item-discontinued` (422) |
| `content.ErrIncompleteContent` | `incomplete-content` (422) |
| `content.ErrUnknownCategory`, `category.ErrUnknownParent` | `unknown-category` (422) |
| `ErrTooManyBullets`, `ErrTooManyAttributes`, `ErrTooManyMedia` | `too-many-bullets`, `too-many-attributes`, `too-many-media` (422) |
| `category.ErrCategoryCycle` | `category-cycle` (422) |
| `category.ErrMaxDepth` | `max-depth` (422) |
| `category.ErrDuplicateSiblingName` | `duplicate-sibling-name` (422) |
| `ports.ErrConcurrentUpdate` | `concurrent-update` (409) |
| `shared.ErrIllegalTransition` | `illegal-transition` (409) |
| `shared.ErrValidation` | `validation-failed` (422) |
| missing `Idempotency-Key` | `idempotency-key-required` (400) |
| reused `Idempotency-Key` | `idempotency-key-reused` (422) |
| anything else | `internal-error` (500), the real error only in the log |

The same slugs are the `reason` attribute of the `content.products.published`
counter when a publish is rejected; see
[Observability](/docs/operations/observability).

## Where the code differs from the design source

The aggregator's model pre-dates ADR 0002; the code follows the ADR.

- The model has `ReviseContent` only on PUBLISHED content and no way to edit a
  draft or publish a revision again. The code adds `ReviseDraft`, a second
  `Publish` for a waiting revision (A1) and the `:discard` route (A5, A6).
- The model's `PublishCategoryTree` had no route; the code serves
  `POST /categories:publish` (A6) and every category command also publishes the
  tree (model-gap decision 8), so create, move and rename raise
  `CategoryTreePublished` rather than nothing.
- `ErrItemDiscontinued` is checked on create and publish, not only
  `ErrUnknownCatalogItem` (A10).
- `ErrCategoryInUse` exists but nothing raises it: categories are never
  deleted (ADR 0002).
