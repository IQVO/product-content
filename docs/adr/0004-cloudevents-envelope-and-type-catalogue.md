# ADR 0004: CloudEvents envelope and type catalogue

## Status

Accepted (2026-10-25).

## Context

Every Kafka message in the retail network is a CloudEvents 1.0 event in structured
mode (the repo rule `.claude/rules/fleet/cloudevents.md`). This ADR is the catalogue
of every type product-content publishes. `TestEventCatalogueMatchesContract` checks
it against `apis/asyncapi.yaml`. The types product-content only consumes are listed
in ADR 0003, not here.

## Decision

### Envelope

- `specversion` `1.0`; `id` a UUID persisted with the outbox row (stable across
  relay retries; consumers dedupe on it); `source`
  `/retailnetwork/product-content`; `subject` the aggregate root id (the
  `catalogItemId` for content events, the `categoryTreeId` `main` for tree events);
  `time` the instant the change was committed, in UTC; `datacontenttype`
  `application/json`; `dataschema`
  `urn:retailnetwork:product-content:events:<EventName>:v1`.
- Kafka value = the JSON event format; header
  `content-type: application/cloudevents+json; charset=UTF-8`; Kafka key = the
  subject, so all events of one item stay ordered on one partition.
- `data` is camelCase; optional fields are omitted when unset; timestamps are
  RFC 3339 UTC; arrays are always present.
- Built and decoded only through `internal/adapters/kafka/cloudevents`.
- `<entity>` is the raising aggregate, lowercase, no separators: `productcontent`,
  `categorytree`. Tier is `storefront`.

### Published types

All three types go to the topic `retailnetwork.product-content.events`. The model
declares no analytics-only event for this context, so nothing is written to
`retailnetwork.product-content.analytics`.

```text
type: com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished stream: events
type: com.retailnetwork.storefront.product-content.productcontent.ProductContentRetired stream: events
type: com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished stream: events
```

| Event | Subject / key | Raised when | `data` |
|---|---|---|---|
| `ProductContentPublished` | `catalogItemId` | PublishContent is saved (DRAFT to PUBLISHED, or PUBLISHED to PUBLISHED with a waiting revision) | `{catalogItemId, revision, description, bullets[], attributes[{name, value, unit?}], media[{mediaId, url, altText, role}], primaryCategoryId}` |
| `ProductContentRetired` | `catalogItemId` | RetireContent, or RetireOnDiscontinue for PUBLISHED content, is saved | `{catalogItemId, revision}` |
| `CategoryTreePublished` | `categoryTreeId` (`main`) | CreateCategory, MoveCategory, RenameCategory or PublishCategoryTree is saved | `{version, categories[{categoryId, parentId?, name, path}]}` |

Payload rules:

- The `data` field lists are the model's `publishes[].data` lists. Wire types are
  recorded in `apis/asyncapi.yaml`.
- `revision` increases for every revision of one item: a consumer applies a revision
  only above the last one it applied. `version` does the same for the tree.
- `CategoryTreePublished` carries the WHOLE tree, ordered by path (parents before
  children), so a consumer replaces its tree instead of replaying edits.
- No event carries a title, a price, stock or PII: only content, category data and
  references (`catalogItemId`, `primaryCategoryId`).
- Discarding a DRAFT, retiring a DRAFT on discontinuation, and publishing content
  whose current revision is already published raise nothing.
- A breaking payload change is a new `.v2` type and dataschema, never a mutation of v1.

## Consequences

- Consumers need nothing but this catalogue and `apis/asyncapi.yaml`; there is no
  shared Go code.
- Adding a published type requires a line here in the same commit as its asyncapi
  message, or the architecture fitness test fails.
