---
id: events
title: Event catalogue
sidebar_label: Event catalogue
sidebar_position: 2
---

# Event catalogue

Every Kafka message this service produces or consumes is a CloudEvents 1.0
event in **structured mode**: the Kafka value is the whole JSON envelope and the
header `content-type` is `application/cloudevents+json; charset=UTF-8`. Envelopes
are built and decoded only in `internal/adapters/kafka/cloudevents`
([ADR 0004](/docs/adr/0004-cloudevents-envelope-and-type-catalogue)). The
contract is `apis/asyncapi.yaml` (AsyncAPI 3.0.0); field-level detail is on
[Domain events](/docs/ddd/domain-events).

:::info Built
The outbox publisher and relay (`internal/adapters/outbound/{kafka,outbox}`) and the
offer-availability consumer (`internal/adapters/inbound/kafka`) are the runtime of
`cmd/api` and are always on, so the service needs `DATABASE_URL`, `KAFKA_BROKERS`
and `OFFER_AVAILABILITY_CONSUMER_GROUP` to boot. The tables below are the pinned
contract, not observed traffic. Whether the other side of each edge exists is on
[Integration](/docs/ecosystem/integration).
:::

## Envelope

| Attribute | Value |
| --- | --- |
| `specversion` | `1.0` |
| `id` | a UUID minted when the event is encoded and stored with the outbox row, so a relay retry republishes the same id; consumers dedupe on it |
| `source` | `/retailnetwork/product-content` |
| `type` | `com.retailnetwork.storefront.product-content.<entity>.<EventName>`, entity `productcontent` or `categorytree` |
| `subject` | the aggregate root id: the `catalogItemId` for content events, `main` for the tree |
| `time` | the instant the change was committed, UTC (the aggregate's `updatedAt`) |
| `datacontenttype` | `application/json` |
| `dataschema` | `urn:retailnetwork:product-content:events:<EventName>:v1` |
| Kafka key | the subject, so all events of one item stay ordered on one partition |

`data` is camelCase, instants are RFC 3339 UTC, optional fields are omitted when
unset (never `null`) and arrays are always present. A breaking payload change is a
new `.v2` type and dataschema, never a mutation of v1.

## Published

All three types go to the topic `retailnetwork.product-content.events`. Nothing is
written to `retailnetwork.product-content.analytics`: the model declares no
analytics-only event.

| Type | Key / subject | Raised when | `data` |
| --- | --- | --- | --- |
| `com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished` | `catalogItemId` | `PublishContent` is saved: DRAFT to PUBLISHED, or PUBLISHED to PUBLISHED with a waiting revision | `catalogItemId`, `revision`, `description`, `bullets[]`, `attributes[{name, value, unit?}]`, `media[{mediaId, url, altText, role}]`, `primaryCategoryId` |
| `com.retailnetwork.storefront.product-content.productcontent.ProductContentRetired` | `catalogItemId` | `RetireContent`, or RetireOnDiscontinue for PUBLISHED content, is saved | `catalogItemId`, `revision` (the revision at retirement) |
| `com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished` | `main` | `CreateCategory`, `MoveCategory`, `RenameCategory` or `PublishCategoryTree` is saved | `version`, `categories[{categoryId, parentId?, name, path}]`, the whole tree ordered by path |

Ordering and replay: a consumer applies a content revision only above the last one
it applied, and the tree only when `version` is above the last applied one, so a
re-delivery or a re-publish is harmless. Discarding a DRAFT, retiring a DRAFT on
discontinuation and publishing content whose current revision is already published
raise nothing. `TestEventCatalogueMatchesContract` fails when a type in
`apis/asyncapi.yaml` is missing from ADR 0004.

## Consumed

| Type | Topic | Used |
| --- | --- | --- |
| `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated` | `retailnetwork.offer-availability.events` | `data.catalogItemId`: the item enters `catalog_item_view` as `ACTIVE` unless already known |
| `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued` | `retailnetwork.offer-availability.events` | `data.catalogItemId`: the item becomes `DISCONTINUED` (terminal) and its content retires |

Verified against the producer's contract on `origin/develop` (see
[Integration](/docs/ecosystem/integration)). Every other type on the topic is
ignored; a message that is not a valid CloudEvent is logged and skipped. The
consumer group comes from `OFFER_AVAILABILITY_CONSUMER_GROUP` (mandatory, no
default). A message that fails five times, or whose payload can never be applied,
is published to `retailnetwork.offer-availability.events.dlq`; see the
[Runbook](/docs/operations/runbook).
