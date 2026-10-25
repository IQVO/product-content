# 1. Product Content is a bounded context of the storefront domain

Status: Accepted (bootstrap). Design source: warehouse-docs `storefront/model/model.yaml`, context `product-content`.

## Context

The storefront is the e-commerce domain of the retail-network organisation: six bounded contexts, each its own
service and database. Enriches the network's catalogue item with everything a product page needs and the item master does not have: description, bullets, attributes, media and the category tree. The title stays the item master's; media upload is out of scope (URLs to static assets only).

## Decision

- This repo owns the aggregates ProductContent, CategoryTree; the invariants and error names are those of the model.
- Business decisions of the context:
- Content publishes only when complete: a description, exactly one PRIMARY medium and a category.
- Only catalogItemId crosses the boundary; content never redefines the item itself [19].
- The category tree is one versioned aggregate, at most four levels deep.
- Events use the storefront CloudEvents namespace `com.retailnetwork.storefront.product-content.<entity>.<Event>` on `retailnetwork.product-content.events`
  (analytics-only events on `.analytics`). Published: ProductContentPublished, ProductContentRetired, CategoryTreePublished. Consumed: CatalogItemCreated, CatalogItemDiscontinued. No `warehouse.*` topic is consumed or produced.
- The storefront calls only the network's Open Host Service routes (model inbound rows for `shopper / storefront`),
  never an in-org or restricted route; sibling data used in a decision is a local event-fed read model, except the
  calls the model lists under `sync_calls`.
- No authentication on REST, consistent with the fleet and the network. This context stores no PII and no card data.

## Consequences

Relationships and the full consumed-event list are in the model and in the AsyncAPI contract added with the first feature PR.
