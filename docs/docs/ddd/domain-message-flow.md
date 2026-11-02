---
id: domain-message-flow
title: Domain message flow
sidebar_label: Domain message flow
sidebar_position: 5
---

# Domain message flow

Following ddd-crew
[Domain Message Flow Modelling](https://github.com/ddd-crew/domain-message-flow-modelling).
Five scenarios. Participants are contexts and actors. Every arrow is a message
prefixed `cmd:` (command), `qry:` (query) or `evt:` (event). A `Note` names work a
context does on itself. Kafka is drawn as one participant. Event names are written
`entity.EventName`; the full type is
`com.retailnetwork.<tier>.<producer>.<entity>.<EventName>` (see
[Domain events](/docs/ddd/domain-events)). REST path parameters are written without
braces.

## 1. Author and publish content

```mermaid
sequenceDiagram
    autonumber
    actor CM as Content manager
    participant PC as product-content
    participant K as Kafka
    participant PD as product-discovery
    CM->>PC: cmd: CreateProductContent<br/>POST /product-contents
    Note over PC: item known and live in catalog_item_view,<br/>state DRAFT, revision 1
    PC-->>CM: reply 201 DRAFT
    CM->>PC: cmd: ReviseContent<br/>POST catalogItemId:revise
    PC-->>CM: reply 200 DRAFT, revision 2
    CM->>PC: cmd: PublishContent<br/>POST catalogItemId:publish
    Note over PC: publish gate, state PUBLISHED,<br/>publishedRevision 2
    PC->>K: evt: productcontent.ProductContentPublished
    PC-->>CM: reply 200 PUBLISHED
    K->>PD: evt: productcontent.ProductContentPublished
    Note over PD: fold into the product view,<br/>revision guard
```

Source: `internal/application/usecases/content.go` (`CreateProductContent`,
`ReviseContent`, `PublishContent`), `internal/domain/content/commands.go`,
`product-discovery`'s `internal/adapters/inbound/kafka/product_content.go`.

Omitted: the outbox write and relay (the event leaves after the commit, see
[Sequence diagrams](/docs/ddd/sequence-diagrams)), the `Idempotency-Key` handling, and
how the item reached `catalog_item_view` (scenario 4's topic).

## 2. Publish rejected (failure path)

```mermaid
sequenceDiagram
    autonumber
    actor CM as Content manager
    participant PC as product-content
    CM->>PC: cmd: PublishContent<br/>POST catalogItemId:publish
    alt content RETIRED
        PC-->>CM: reply 409 content-retired
    else item unknown to catalog_item_view
        PC-->>CM: reply 422 unknown-catalog-item
    else item discontinued
        PC-->>CM: reply 422 item-discontinued
    else no description, not exactly one PRIMARY medium, or no category
        PC-->>CM: reply 422 incomplete-content
    else primary category not in the current tree
        PC-->>CM: reply 422 unknown-category
    end
    Note over PC: nothing saved,<br/>nothing published
```

Source: `internal/domain/content/commands.go` (`Publish`, order of the checks),
`internal/adapters/inbound/http/errors.go`.

The manager corrects the DRAFT with `:revise` and publishes again. A publish of the
revision that is already live is not a rejection: it answers 200 and changes nothing.

## 3. Edit the tree

```mermaid
sequenceDiagram
    autonumber
    actor CM as Content manager
    participant PC as product-content
    participant K as Kafka
    participant PD as product-discovery
    CM->>PC: cmd: CreateCategory<br/>POST /categories
    PC->>K: evt: categorytree.CategoryTreePublished, version 1
    PC-->>CM: reply 201 category
    CM->>PC: cmd: MoveCategory<br/>POST categories/categoryId:move
    PC->>K: evt: categorytree.CategoryTreePublished, version 2
    PC-->>CM: reply 200 category
    CM->>PC: cmd: PublishCategoryTree<br/>POST /categories:publish
    Note over PC: tree unchanged,<br/>version bumped
    PC->>K: evt: categorytree.CategoryTreePublished, version 3
    K->>PD: evt: categorytree.CategoryTreePublished
    Note over PD: replace the category view<br/>when version is above the last
```

Source: `internal/application/usecases/category.go` (`saveTree` appends the event for
every command), `internal/domain/category/category.go` (`publish`).

Every category command publishes the whole tree under a new version. The structural
errors (`category-cycle`, `max-depth`, `duplicate-sibling-name`, `unknown-category`,
`not-found`) are answered before anything is saved or published.

## 4. Item discontinued: retire the content

```mermaid
sequenceDiagram
    autonumber
    participant OA as offer-availability
    participant K as Kafka
    participant PC as product-content
    participant PD as product-discovery
    OA->>K: evt: catalogitem.CatalogItemCreated
    K->>PC: evt: catalogitem.CatalogItemCreated
    Note over PC: claim the event id,<br/>item ACTIVE in catalog_item_view
    OA->>K: evt: catalogitem.CatalogItemDiscontinued
    K->>PC: evt: catalogitem.CatalogItemDiscontinued
    Note over PC: claim the event id,<br/>item DISCONTINUED,<br/>policy RetireOnDiscontinue
    alt content is PUBLISHED
        Note over PC: state RETIRED, reason DISCONTINUED
        PC->>K: evt: productcontent.ProductContentRetired
        K->>PD: evt: productcontent.ProductContentRetired
        Note over PD: content not PUBLISHED,<br/>item not purchasable
    else content is a DRAFT
        Note over PC: state RETIRED, reason DISCONTINUED,<br/>no event
    else no content or already RETIRED
        Note over PC: nothing more happens
    end
```

Source: `internal/adapters/inbound/kafka/offer_availability.go`,
`internal/application/usecases/consume.go` (`ApplyCatalogItemCreated`,
`ApplyCatalogItemDiscontinued`), `internal/domain/content/commands.go`
(`RetireOnDiscontinue`).

Omitted: the retry and dead-letter path (see
[Integration](/docs/ecosystem/integration#how-every-consumed-message-is-handled)). The
item's own lifecycle is `offer-availability`'s.

## 5. Projection bootstrap: product-discovery reads the snapshot

```mermaid
sequenceDiagram
    autonumber
    participant PD as product-discovery
    participant PC as product-content
    participant K as Kafka
    Note over PD: cmd/rebuild: snapshot, then catch-up
    loop paged
        PD->>PC: qry: ListProductContents<br/>GET /product-contents after, limit
        PC-->>PD: reply page and next
    end
    PD->>PC: qry: ListCategories<br/>GET /categories
    PC-->>PD: reply version and categories
    Note over PD: snapshot loaded
    K->>PD: evt: productcontent.ProductContentPublished
    Note over PD: catch-up on events,<br/>revision guard
```

Source: `internal/application/usecases/queries.go` (`ListProductContents`,
`ListCategories`), and `product-discovery`'s `cmd/rebuild/main.go` on its
`origin/develop`.

Omitted: the other bootstrap reads `product-discovery` does against
`offer-availability` and `pricing`. `GetProductContent` (one item) has the same shape
as one page of the list and is not drawn separately.
