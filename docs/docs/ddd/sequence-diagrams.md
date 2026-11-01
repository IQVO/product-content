---
id: sequence-diagrams
title: Sequence diagrams
sidebar_label: Sequence diagrams
sidebar_position: 9
---

# Sequence diagrams

Six UML sequence diagrams inside the service: how a request or a consumed event
moves through the layers, with the transaction boundary and the error branches.
Participants are code units. Cross-context scenarios are on
[Domain message flow](/docs/ddd/domain-message-flow).

## 1. Create content, revise it and publish it

```mermaid
sequenceDiagram
    autonumber
    actor CM as content manager
    participant H as inbound/http
    participant ID as Idempotency-Key middleware
    participant UC as usecases
    participant D as domain content
    participant PG as Postgres
    participant R as outbox relay
    participant K as Kafka
    CM->>H: POST /product-contents with Idempotency-Key
    H->>ID: look up route and key
    ID->>PG: select idempotency_keys
    PG-->>ID: absent
    ID->>UC: CreateProductContent.Execute
    UC->>PG: begin unit of work
    UC->>PG: select catalog_item_view, select category_trees
    UC->>D: Create(id, body, item status, tree)
    D-->>UC: DRAFT at revision 1
    UC->>PG: insert product_contents
    UC->>PG: commit
    UC-->>ID: 201 content
    ID->>PG: insert idempotency_keys
    ID-->>CM: 201 content
    CM->>H: POST product-contents/id:publish
    H->>UC: PublishContent.Execute
    UC->>PG: begin, select product_contents and catalog_item_view
    UC->>D: Publish(item status, tree)
    D-->>UC: PUBLISHED and ProductContentPublished
    UC->>PG: update product_contents where version = loaded
    UC->>PG: insert outbox_events with the encoded CloudEvent
    UC->>PG: commit
    UC-->>CM: 200 content
    loop every OUTBOX_RELAY_INTERVAL, 1 s by default
        R->>PG: claim unpublished rows in id order
        R->>K: write to retailnetwork.product-content.events
        K-->>R: acknowledged
        R->>PG: set published_at
    end
```

Source: `internal/adapters/inbound/http/server.go`, `idempotency.go`,
`internal/application/usecases/content.go`, `deps.go` (`transact`),
`internal/domain/content/commands.go`, `internal/adapters/outbound/outbox/relay.go`.

The aggregate row and the outbox row commit in one transaction, so a crash between
save and publish never loses the event. The idempotency record is written by the
middleware afterwards, in its own statement: a crash in between leaves the command
done and the key unrecorded, and a retry with the same key runs the command again
(a repeated create then answers `409 concurrent-update`). The relay repeats the
stored CloudEvents `id` on a retry, so a consumer sees at-least-once delivery of one
stable id.

## 2. Publish rejected

```mermaid
sequenceDiagram
    autonumber
    actor CM as content manager
    participant H as inbound/http
    participant UC as usecases PublishContent
    participant D as domain content
    participant PG as Postgres
    CM->>H: POST product-contents/id:publish
    H->>UC: Execute(id)
    UC->>PG: begin, select product_contents
    alt no row
        PG-->>UC: not found
        UC-->>H: ports.ErrNotFound
        H-->>CM: 404 not-found
    else row found
        UC->>D: Publish(item status, tree)
        alt RETIRED
            D-->>H: ErrContentRetired, 409 content-retired
        else item unknown
            D-->>H: ErrUnknownCatalogItem, 422 unknown-catalog-item
        else item discontinued
            D-->>H: ErrItemDiscontinued, 422 item-discontinued
        else not complete
            D-->>H: ErrIncompleteContent, 422 incomplete-content
        else category not in the tree
            D-->>H: ErrUnknownCategory, 422 unknown-category
        else current revision already published
            D-->>UC: no event, nothing to save
            UC-->>CM: 200 content unchanged
        end
    end
    Note over UC,PG: rollback on every error,<br/>the rejection is counted on content.products.published
```

Source: `internal/application/usecases/content.go` (`PublishContent`),
`internal/application/usecases/content_metrics.go`,
`internal/domain/content/commands.go` (`Publish`),
`internal/adapters/inbound/http/errors.go`.

## 3. Edit the category tree

```mermaid
sequenceDiagram
    autonumber
    actor CM as content manager
    participant H as inbound/http
    participant UC as usecases MoveCategory
    participant T as domain category Tree
    participant PG as Postgres
    CM->>H: POST categories/id:move with parentId
    H->>UC: Execute(categoryId, parentId)
    UC->>PG: begin, select category_trees
    PG-->>UC: tree at version v
    UC->>T: MoveCategory(id, parentId)
    alt unknown category
        T-->>H: ErrCategoryNotFound, 404 not-found
    else unknown parent
        T-->>H: ErrUnknownParent, 422 unknown-category
    else parent inside the moved subtree
        T-->>H: ErrCategoryCycle, 422 category-cycle
    else subtree would pass four levels
        T-->>H: ErrMaxDepth, 422 max-depth
    else a sibling has the same name
        T-->>H: ErrDuplicateSiblingName, 422 duplicate-sibling-name
    else accepted
        T-->>UC: category and CategoryTreePublished at version v+1
        UC->>PG: update category_trees where version = v
        UC->>PG: insert outbox_events
        UC->>PG: commit
        UC-->>CM: 200 category
    end
```

Source: `internal/application/usecases/category.go` (`MoveCategory`, `saveTree`),
`internal/domain/category/category.go` (`MoveCategory`).

A concurrent tree edit fails the conditional update with `ErrConcurrentUpdate`; the
use case runs once more on fresh state and then answers `409 concurrent-update`.
Create, rename and publish follow the same shape.

## 4. Retire on discontinue

```mermaid
sequenceDiagram
    autonumber
    participant K as Kafka
    participant C as inbound/kafka consumer
    participant UC as usecases Consumer
    participant D as domain content
    participant PG as Postgres
    K->>C: CatalogItemDiscontinued
    C->>C: decode CloudEvents, dispatch on the full type
    C->>UC: ApplyCatalogItemDiscontinued(meta, catalogItemId)
    UC->>PG: begin unit of work
    UC->>PG: insert processed_events for the event id
    alt id already claimed
        PG-->>UC: not first
        UC->>PG: commit, nothing applied
    else first time
        UC->>PG: upsert catalog_item_view as DISCONTINUED
        UC->>PG: select product_contents
        alt no content
            UC->>PG: commit
        else content found
            UC->>D: RetireOnDiscontinue
            D-->>UC: ProductContentRetired for PUBLISHED, none for DRAFT or RETIRED
            UC->>PG: update product_contents and insert outbox_events when changed
            UC->>PG: commit
        end
    end
    UC-->>C: nil
    C->>K: commit the offset
```

Source: `internal/application/usecases/consume.go`,
`internal/adapters/inbound/kafka/offer_availability.go`, `consumer.go`.

The inbox claim, the view update, the content change and the outbox row are one
transaction; the offset is committed after it, so a crash in between replays the
message and the claim makes the replay a no-op.

## 5. Consumer retry and dead letter

```mermaid
sequenceDiagram
    autonumber
    participant K as Kafka
    participant L as consume loop
    participant H as handler
    participant DLQ as offer-availability events dlq
    K->>L: message
    L->>H: HandleMessage(value)
    alt not a valid CloudEvents 1.0 document or unknown type
        H-->>L: nil, logged at WARN
        L->>K: commit the offset
    else data cannot be decoded or catalogItemId invalid
        H-->>L: poison
        L->>DLQ: publish at once with x-dlq headers
        L->>K: commit the offset
    else transient failure
        loop up to 5 attempts, backoff 200 ms doubling to 5 s
            L->>H: HandleMessage again
        end
        L->>DLQ: publish after the fifth failure
        L->>K: commit the offset
    else applied
        H-->>L: nil
        L->>K: commit the offset
    end
```

Source: `internal/adapters/inbound/kafka/consumer.go`, `kafka.go`, `deadletter.go`.

The loop never commits a message it failed to handle or dead-letter. Shutdown stops
the retries without dead-lettering the message in flight
(`TestDeadLetterCallsGuardCancellation`).

## 6. Idempotent replay

```mermaid
sequenceDiagram
    autonumber
    actor CM as client
    participant ID as Idempotency-Key middleware
    participant PG as Postgres
    participant UC as use case
    CM->>ID: POST with Idempotency-Key k
    ID->>PG: select idempotency_keys for route and k
    alt no record
        ID->>UC: run the command
        UC-->>ID: response
        opt status below 500
            ID->>PG: insert idempotency_keys
        end
        ID-->>CM: response
    else record with the same request hash
        ID-->>CM: stored response with Idempotent-Replayed true
    else record with another request hash
        ID-->>CM: 422 idempotency-key-reused
    end
```

Source: `internal/adapters/inbound/http/idempotency.go`.

A missing or over-long key is `400 idempotency-key-required` before anything runs. A
record older than 24 hours counts as absent.
