# ADR 0002: Aggregates, invariants and assumptions

## Status

Accepted (2026-10-25).

## Context

The design source is warehouse-docs `storefront/model/model.yaml`, context
`product-content`, with the FINAL model-gap decisions of the storefront campaign
applied on top (they override the model where they differ). This ADR records the
aggregates as built and every assumption (A1, A2, ...) taken where the model and the
decisions are silent. Each assumption is also reported to the orchestrator as a
`model_gap`.

## Decision

### Aggregates

- **ProductContent** (`internal/domain/content`, root `catalogItemId`): description,
  bullets (at most 10), attributes `{name, value, unit?}` (at most 50), media
  `{mediaId, url, altText, role PRIMARY|GALLERY}` (at most 12), `primaryCategoryId`,
  `revision`, `publishedRevision`.
  - States `DRAFT -> PUBLISHED -> RETIRED`, plus `DRAFT -> DRAFT` (ReviseDraft),
    `PUBLISHED -> PUBLISHED` (ReviseContent, then PublishContent with the new
    revision) and `DRAFT -> RETIRED` (DiscardDraft, reason `DISCARDED`, no event, and
    RetireOnDiscontinue, reason `DISCONTINUED`).
  - Errors: `ErrUnknownCatalogItem`, `ErrItemDiscontinued`, `ErrIncompleteContent`,
    `ErrUnknownCategory`, `ErrContentRetired`, `ErrTooManyBullets`,
    `ErrTooManyAttributes`, `ErrTooManyMedia`.
- **CategoryTree** (`internal/domain/category`, root `categoryTreeId` = `main`): ONE
  live aggregate with no draft state. Every command (CreateCategory, MoveCategory,
  RenameCategory, PublishCategoryTree) applies to the live tree and publishes a new
  version. Errors: `ErrCategoryCycle`, `ErrMaxDepth` (4), `ErrDuplicateSiblingName`,
  `ErrCategoryInUse`. Categories are never deleted, only created, moved and renamed:
  there is no remove-category command by design, and `ErrCategoryInUse` is declared
  to guard a future removal (model-gap decision 4); nothing raises it today.
- The two aggregates share no state. The only cross-aggregate fact is whether a
  category exists in the current tree, which the use case passes to ProductContent as
  the `content.Categories` interface.

### Assumptions

- **A1 Revisions.** `revision` is the number of the current shape of the content: 1
  at creation, plus one for every ReviseDraft and ReviseContent. `publishedRevision`
  is the revision last published (0 = never). PublishContent publishes the CURRENT
  revision. Publishing PUBLISHED content whose `revision == publishedRevision` has
  nothing new to publish: the call succeeds, changes nothing and raises no event, so
  every `ProductContentPublished` carries a revision above the previous one and a
  repeated publish is harmless. `ProductContentRetired.revision` is the revision at
  retirement.
- **A2 Integers.** Every bounded integer fits a Postgres `integer`.
- **A3 Time.** Instants are UTC, truncated to the microsecond Postgres stores. The
  CloudEvents `time` of an event is the aggregate's `updatedAt` after the change.
- **A4 One revise route.** `POST /product-contents/{catalogItemId}:revise` is
  ReviseDraft for a DRAFT and ReviseContent for PUBLISHED content; the state picks
  the command. The body replaces the whole editable content. PUBLISHED content must
  stay complete (`incomplete-content` otherwise), so it is always publishable. A
  revise of RETIRED content is 409 `content-retired`.
- **A5 Retirement reasons.** `retiredReason` is `MANUAL` (RetireContent on PUBLISHED
  content), `DISCARDED` (DiscardDraft) or `DISCONTINUED` (RetireOnDiscontinue). It is
  on the REST read, not on the event (the event keeps the model's data list).
  RetireContent of a DRAFT or of RETIRED content is 409 `illegal-transition`; a DRAFT
  is discarded instead.
- **A6 New routes.** `POST /product-contents/{catalogItemId}:discard` (DiscardDraft)
  and `POST /categories:publish` (PublishCategoryTree, decision 1). PublishCategoryTree
  changes no category: it publishes the live tree again under a new version, the way
  to re-send it to a consumer.
- **A7 The tree.** It is created by the first CreateCategory. Before that,
  `GET /categories` answers version 0 and an empty list. The tree version is the
  aggregate version. Category ids are minted by the service (`cat-` and 12 hex
  characters). Names are trimmed, 1 to 100 characters, without `/`; siblings are
  compared case-insensitively; a category path is the names from the root joined with
  `/`; the tree holds at most 5000 categories. Moving or renaming to the current
  parent or name still publishes a new version.
- **A8 Category errors.** An unknown `categoryId` in the path (move, rename) is 404
  `not-found`. An unknown parent in a body (create, move) is 422 `unknown-category`,
  like an unknown `primaryCategoryId`. A move never raises `category-in-use`.
- **A9 Duplicate create.** A second CreateProductContent for an item is 409
  `concurrent-update` (the primary key on `catalogItemId` is the guard).
- **A10 Item errors.** `unknown-catalog-item` and `item-discontinued` are 422.
  ErrUnknownCatalogItem applies to CreateProductContent and PublishContent;
  ErrItemDiscontinued too (decision 7). Revise, retire and discard do not look at the
  item.
- **A11 Collection and field limits.** Beyond the three bounds (10/50/12, decision 5):
  description at most 4000 characters, a bullet 1 to 300, an attribute name 1 to 100,
  value 1 to 300, unit at most 20, a medium URL an absolute http(s) URL of at most
  2048 characters, alt text 1 to 200, media ids valid and unique within the content.
  The collection bounds are checked before the fields, the fields before the category.
  A DRAFT may be incomplete (no description, no media, no category); completeness is
  the publish gate.
- **A12 Listing.** `GET /product-contents` lists every state ordered by
  `catalogItemId`; `after` is the last `catalogItemId` of the previous page and `next`
  is that cursor, absent on the last page; the default page is 100 and the limit is
  1 to 500.
- **A13 Idempotency.** Every write takes an `Idempotency-Key` (1 to 128 characters,
  remembered 24 hours per route and key). A repeated key with the same request body
  replays the stored response; with a different body it is 422 `validation-failed`.
  Only 2xx responses are stored.
- **A14 Ids.** A `catalogItemId` is an opaque id of 1 to 64 characters from
  `A-Za-z0-9._:-` starting with a letter or digit (`shared.ValidID`). Any other id is
  422 `validation-failed` on REST and a dead-lettered message on the consumer.
- **A15 Analytics.** The model declares no analytics-only event; the
  `retailnetwork.product-content.analytics` topic receives nothing.

## Consequences

- A DRAFT can be edited and discarded; PUBLISHED content can be edited and published
  again; shoppers (through product-discovery) only ever see published revisions.
- The tree is small (~10^3 categories), so it is stored as one row and every event
  carries the whole tree.
