---
id: ubiquitous-language
title: Ubiquitous language
sidebar_label: Ubiquitous language
sidebar_position: 9
---

# Ubiquitous language

The terms this context speaks, each mapped to the identifier that implements it
on this branch. The first table is the context's own vocabulary, the second the
model elements of its aggregates, the third neighbour terms it uses but does not
own (their definitions belong to the owning context). The storefront-wide glossary
is
[Ubiquitous Language](https://iqvo.github.io/warehouse-docs/storefront/strategic-design/ubiquitous-language)
in the aggregator.

## Own terms

| Term | Definition | Code identifier | Not to be confused with |
| --- | --- | --- | --- |
| **Product Content** | Description, bullets, attributes, media and primary category of one catalogue item, with a lifecycle and a revision. | `content.ProductContent` (`internal/domain/content/content.go`) | **Catalog Item** (`offer-availability`): the network's own entry for the product, with title, dimensions and GTIN. Content never redefines the item; only `catalogItemId` crosses the boundary. |
| **Revision** | The number of the current shape of an item's content: 1 at creation, plus one per revise. Publishing publishes the current revision. | `content.ProductContent.Revision()` | The optimistic-concurrency `version`, which grows with every change and is a storage detail. `product-discovery` orders content by `revision`. |
| **Published revision** | The revision last published; 0 when never published. When it equals `revision` there is nothing new to publish. | `content.ProductContent.PublishedRevision()` | `revision`: a revision exists as soon as it is saved, a published revision only after `:publish`. |
| **Primary medium** | The one image a product page leads with; exactly one per published content. | `content.RolePrimary` | A `GALLERY` medium: any other picture. Publish fails without exactly one `PRIMARY`. |
| **Category** | One node of the browsable tree: `categoryId`, `parentId`, `name` and a derived `path`. | `category.Category` | The **primary category** a content names by `primaryCategoryId`: every category is a node, only one per content is primary. |
| **Category tree** | The single, live, versioned tree of categories, id `main`, at most four levels and 5000 categories. | `category.Tree` | The catalogue (`offer-availability`): the tree orders browsing, not the item master. |
| **Tree version** | The tree aggregate's version, which grows with every category command and every `POST /categories:publish`. | `category.Tree.Version()` | A content `revision`: they count independent things. |

## Model elements of this context

| Term | Definition | Code identifier | Not to be confused with |
| --- | --- | --- | --- |
| **Content state** | `DRAFT`, `PUBLISHED` or `RETIRED`. `RETIRED` is terminal. | `content.State`, `StateDraft`, `StatePublished`, `StateRetired` | The item's state in the item master (`offer-availability`) or in `catalog_item_view`. |
| **Retired reason** | Why content is `RETIRED`: `MANUAL` (retired by a content manager), `DISCARDED` (a draft discarded), `DISCONTINUED` (the item was discontinued). Visible on the REST read, not on the event. | `content.RetiredReason`, `ReasonManual`, `ReasonDiscarded`, `ReasonDiscontinued` | The event `ProductContentRetired`, which is raised for `MANUAL` and for `DISCONTINUED` retirement of PUBLISHED content only. |
| **Body** | The editable part of the content that create and revise replace as a whole. | `content.Body` | The whole aggregate: the body has no state or revision of its own. |
| **Attribute** | One named fact of the item: `name`, `value`, optional `unit`. At most 50. | `content.Attribute` | A filter facet of `product-discovery`, which derives its facets from attributes. |
| **Media** | One picture reference: `mediaId`, `url`, `altText`, `role`. At most 12. URLs to static assets only. | `content.Media`, `content.Role` | An uploaded asset: this context never stores bytes. |
| **Bullet** | One line of the bullet list. At most 10, 1 to 300 characters. | `content.Body.Bullets` | An attribute: a bullet is prose, an attribute a named value. |
| **Publish gate** | A description, exactly one `PRIMARY` medium and a category that is in the current tree. | `content.complete`, `ErrIncompleteContent`, `ErrUnknownCategory` | Validation of a DRAFT, which may be incomplete. |
| **Catalog item view** | The event-fed local copy of the items the network knows, `ACTIVE` or `DISCONTINUED`. It answers "is the item known and live" without a call. | `readmodel.CatalogItem`, table `catalog_item_view` | The item master itself (`offer-availability`): this is a projection, never the source of truth. |
| **Item status** | What the use case hands the domain about the item: unknown, active or discontinued. | `content.ItemStatus`, `ItemUnknown`, `ItemActive`, `ItemDiscontinued` | The `state` column of `catalog_item_view`. |
| **Retire on discontinue** | Policy: when the network discontinues an item, its PUBLISHED content retires with an event and its DRAFT retires silently. | `content.ProductContent.RetireOnDiscontinue`, `usecases.Consumer.ApplyCatalogItemDiscontinued` | `RetireContent` by a content manager, which only retires PUBLISHED content and sets `MANUAL`. |
| **Discard** | A content manager abandons a DRAFT: it becomes `RETIRED` with reason `DISCARDED` and no event. | `content.ProductContent.DiscardDraft`, `usecases.DiscardDraft` | Retire: discarding is for drafts, retiring for published content. |
| **Outbox** | The table written in the same transaction as the aggregate, drained to Kafka by the relay. | `outbox_events`, `outbox.Message`, relay in `internal/adapters/outbound/outbox` | The Kafka topic: a row is not an event on the wire until the relay has sent it. |
| **Inbox** | The table of consumed CloudEvents ids, so each consumed event applies once. | `processed_events`, `ports.Inbox` | The idempotency keys, which dedupe REST writes, not consumed events. |
| **Idempotency key** | The `Idempotency-Key` header of a write, remembered 24 hours per route. | `idempotency_keys`, `idempotency.Record` | The CloudEvents `id`. |

## Neighbour terms this context uses

| Term | Definition (owner's) | Code identifier (owner) | Not to be confused with |
| --- | --- | --- | --- |
| **Catalog Item** | The network's own entry for a product, independent of who ships it. | `catalog.Item` (`offer-availability`) | **Product Content**: this context's sellable description of that item. The title stays the item master's. |
| **Purchasable** | Item active in the item master, content PUBLISHED, price active, and at least one live listing with quantity above zero. | `productview.Purchasable` (`product-discovery`) | Published: PUBLISHED content is one condition, never the verdict. |
| **Product view** | The projection one product page or search hit renders from. | `productview.View` (`product-discovery`) | Product Content: the view folds this context's events together with listings and prices. |
| **Category view** | The browsable tree with purchasable counts per category. | `categoryview.View` (`product-discovery`) | Category tree: the view adds counts to the tree this context publishes. |

## Fleet translation

None. The storefront never touches the warehouse-systems fleet: this context
consumes only `offer-availability` events and serves only `product-discovery` and
operators.
