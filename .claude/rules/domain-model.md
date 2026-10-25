# Domain model: ubiquitous language, aggregates, events, use cases

## Ubiquitous Language (use these exact names)

- **ProductContent** — the shopper-facing enrichment of one catalogue item (description,
  bullets, attributes, media, primary category). Keyed by `catalogItemId`.
- **Revision / publishedRevision** — `revision` numbers the current shape; `publishedRevision` is
  the last one shoppers see (0 = never published).
- **DRAFT / PUBLISHED / RETIRED** — the closed content states. RETIRED carries a reason:
  `MANUAL`, `DISCARDED` or `DISCONTINUED`.
- **CategoryTree** — the ONE live tree of categories (id `main`, at most four levels,
  sibling names unique, no cycles); no draft state. Categories are renamed and moved, never deleted.
- **CatalogItemView** — local read model of the catalogue item's state (`ACTIVE` |
  `DISCONTINUED`), fed by offer-availability events; backs `ErrUnknownCatalogItem` and
  `ErrItemDiscontinued`.
- **RetireOnDiscontinue** — policy: a discontinued item's PUBLISHED content retires (event) and its DRAFT retires (no event).

## Aggregates

- **ProductContent** (`internal/domain/content`): bounds 10 bullets / 50 attributes / 12 media
  (`ErrTooMany...`); publishing needs a description, exactly one PRIMARY medium and a known
  category (`ErrIncompleteContent`, `ErrUnknownCategory`); RETIRED never changes
  (`ErrContentRetired`); a discontinued item can neither create nor publish (`ErrItemDiscontinued`).
  Transitions: ReviseDraft DRAFT->DRAFT, DiscardDraft DRAFT->RETIRED (no event), Publish
  DRAFT->PUBLISHED or PUBLISHED->PUBLISHED with a new revision.
- **CategoryTree** (`internal/domain/category`): `ErrCategoryCycle`, `ErrMaxDepth`,
  `ErrDuplicateSiblingName`, `ErrUnknownParent`; every command bumps the version and
  publishes the tree. `ErrCategoryInUse` guards a future removal and never fires today.
- `internal/domain/shared`: ids, `ErrValidation`, `ErrIllegalTransition`, the event interface.

## Domain events

- `ProductContentPublished` — PublishContent saved a revision (carries the whole body).
- `ProductContentRetired` — RetireContent, or RetireOnDiscontinue on PUBLISHED content.
- `CategoryTreePublished` — any category command or PublishCategoryTree saved a new tree version.

## Key use cases (`internal/application/usecases`)

- `CreateProductContent`, `ReviseContent`, `PublishContent`, `RetireContent`, `DiscardDraft` — load,
  call the aggregate, save and append to the outbox in one UnitOfWork; read CatalogItemView.
- `CreateCategory`, `MoveCategory`, `RenameCategory`, `PublishCategoryTree` — same shape over the tree.
- `GetProductContent`, `ListProductContents`, `ListCategories` — read-only queries.
- `Consumer.ApplyCatalogItemCreated` / `ApplyCatalogItemDiscontinued` — claim the inbox row, update
  CatalogItemView and run RetireOnDiscontinue in one UnitOfWork.
