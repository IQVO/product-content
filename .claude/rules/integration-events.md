---
paths:
  - "internal/adapters/**/kafka/**"
  - "internal/adapters/outbound/outbox/**"
  - "apis/asyncapi*"
---

# Cross-service integration events (Kafka)

product-content PUBLISHES three events to `retailnetwork.product-content.events` (there
is no analytics-only event, so `.analytics` is unused) and CONSUMES two events from
`retailnetwork.offer-availability.events`. Full contract:
`docs/adr/0003-consumed-contracts-and-local-read-model.md`,
`docs/adr/0004-cloudevents-envelope-and-type-catalogue.md` and `apis/asyncapi.yaml`.

## Events: CloudEvents 1.0 is MANDATORY

Every message produced or consumed is a CloudEvents 1.0 event in structured content
mode; read `.claude/rules/fleet/cloudevents.md`. There is no flat envelope, no
dual-write and no envelope toggle (`TestNoEventEnvelopeToggleOrFlatEnvelope`).

- Build and decode only through `internal/adapters/kafka/cloudevents` (`New`,
  `Decode`, `ContentTypeHeader`); transport is `segmentio/kafka-go`.
- Required attributes: `specversion=1.0`; `id` (UUID minted once per domain event and
  stored with the outbox row); `source=/retailnetwork/product-content`; `type`;
  `subject` (aggregate id, never empty); `time` (UTC); `datacontenttype=application/json`;
  `dataschema=urn:retailnetwork:product-content:events:<Event>:v1`. Message key =
  subject, `kafkago.Hash{}` balancer.
- Consumers `Decode` (validates), dispatch on the FULL `type` string, ignore unknown
  types, dedupe on `id` through the inbox, dead-letter a payload that can never be
  applied to `<topic>.dlq`, and never crash or block the partition.
- Tests: a golden exact-JSON test per published type
  (`internal/adapters/outbound/kafka/testdata`), a flat-message-rejected test per
  consumer, Kafka integration tests via testcontainers only.

### Published types

| `type` | topic | `subject` | `dataschema` |
| --- | --- | --- | --- |
| `com.retailnetwork.storefront.product-content.productcontent.ProductContentPublished` | `retailnetwork.product-content.events` | catalogItemId | `urn:retailnetwork:product-content:events:ProductContentPublished:v1` |
| `com.retailnetwork.storefront.product-content.productcontent.ProductContentRetired` | `retailnetwork.product-content.events` | catalogItemId | `urn:retailnetwork:product-content:events:ProductContentRetired:v1` |
| `com.retailnetwork.storefront.product-content.categorytree.CategoryTreePublished` | `retailnetwork.product-content.events` | `main` | `urn:retailnetwork:product-content:events:CategoryTreePublished:v1` |

### Consumed types

| `type` | topic | producer |
| --- | --- | --- |
| `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemCreated` | `retailnetwork.offer-availability.events` | offer-availability |
| `com.retailnetwork.commerce.offer-availability.catalogitem.CatalogItemDiscontinued` | `retailnetwork.offer-availability.events` | offer-availability |

## Consumer group id

The group comes from the mandatory environment variable
`OFFER_AVAILABILITY_CONSUMER_GROUP` (read in `cmd/api/config.go`, never a literal;
`TestKafkaConsumerGroupNeverHardcodedInline`). The consumer resumes from its committed
offset (it is not a replay-on-start cache), so a stable shared group is correct.
