# ADR 0003: Consumed contracts and the local CatalogItemView

## Status

Accepted (2026-10-25).

## Context

Product content enriches a catalogue item that offer-availability owns. Creating and
publishing content must know whether the item exists and whether it is discontinued,
without calling offer-availability on the request path. The model lists no
`sync_calls` for this context: sibling data used in a decision is a local, event-fed
read model.

## Decision

### Consumed types

Both come from offer-availability on `retailnetwork.offer-availability.events`
(Conformist). The consumer group comes from the mandatory environment variable
`OFFER_AVAILABILITY_CONSUMER_GROUP` (suggested value
`product-content.offer-availability`).

```text
com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated
com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued
```

Only `data.catalogItemId` is used. The title, dimensions, GTIN and flags belong to
the item master and are dropped. Unknown fields and unknown types are ignored.

### CatalogItemView

One row per catalogue item: `catalog_item_id`, `state` (`ACTIVE` | `DISCONTINUED`),
`updated_at`. It backs `ErrUnknownCatalogItem` (no row) and `ErrItemDiscontinued`
(state `DISCONTINUED`) in CreateProductContent and PublishContent.

| Event | Effect on CatalogItemView | Effect on ProductContent |
|---|---|---|
| `CatalogItemCreated` | insert `ACTIVE` when the item is unknown; an existing row (ACTIVE or DISCONTINUED) is left as it is | none |
| `CatalogItemDiscontinued` | upsert `DISCONTINUED` (also for an item never seen: the event may overtake its Created) | RetireOnDiscontinue (below) |

`DISCONTINUED` is terminal: a late or replayed `CatalogItemCreated` never revives an
item.

### RetireOnDiscontinue

In the same transaction as the view update and the inbox row:

- PUBLISHED content becomes RETIRED (reason `DISCONTINUED`) and raises
  `ProductContentRetired`.
- A DRAFT becomes RETIRED (reason `DISCONTINUED`) with no event: nothing was ever
  published (model-gap decision 6).
- RETIRED content, or an item without content, is left alone.

### Idempotency and failures

- Every consumer claims the CloudEvents `id` in the `processed_events` inbox inside
  the unit of work that applies the effect; a duplicate is acknowledged without effect.
- Dispatch is on the full `type` string, never on a suffix.
- A message that fails CloudEvents validation is skipped with a WARN log and its
  offset committed. A message of a handled type whose `data` does not decode, or
  names an invalid `catalogItemId`, is dead-lettered at once to
  `retailnetwork.offer-availability.events.dlq` (`internal/adapters/inbound/kafka/deadletter.go`);
  so is a message whose handling failed five times in a row. The consumer then moves on.
- Transient fetch errors and a topic that does not exist yet are retried with
  backoff; the consumer never crashes the process for them.

## Consequences

- Content can only be created after the item's `CatalogItemCreated` was consumed. A
  422 `unknown-catalog-item` may therefore be a projection lag, not a missing item.
- A rebuild of CatalogItemView beyond topic retention would use offer-availability's
  `GET /catalog`; no route of this service needs it today.
