---
id: introduction
title: Introduction
sidebar_label: Introduction
sidebar_position: 1
---

# Product Content

`product-content` is a **Storefront-area Generic** bounded context of the
retail-network organisation. It enriches a catalogue item the network already
knows with everything a product page needs and the item master does not have:
a description, bullet points, an attribute table, media and a place in the
category tree. Content is written as a DRAFT, published as a numbered
revision when it is complete, and retired when it is no longer sold.

The title stays the item master's (`offer-availability`); only
`catalogItemId` crosses the boundary. Media upload is out of scope: media are
URLs to static assets. Shoppers never call this service. `product-discovery`
reads its REST pages to rebuild a projection and folds its events into the
product page, so the browser reaches the storefront edge and never this
context ([Integration](/docs/ecosystem/integration)).

:::info What runs on develop
Two binaries: `cmd/api` (REST, the transactional outbox relay and the
offer-availability consumer in one process) and `cmd/a2a` (a read-only
Agent2Agent agent over the same Postgres,
[ADR 0006](/docs/adr/0006-a2a-inbound-adapter)). Both run on real Postgres;
`cmd/api` also needs real Kafka. There is no in-memory mode, and
`DATABASE_URL`, `KAFKA_BROKERS` and `OFFER_AVAILABILITY_CONSUMER_GROUP` are
mandatory for `cmd/api` ([Configuration](/docs/operations/configuration)).
Packaged as a `Dockerfile` that builds both binaries and the Helm chart
`charts/product-content` (the `api` Deployment always, the A2A Deployment when
`a2a.enabled=true`). There is no `cmd/mcp`: this context has no MCP server.
:::

## What the context owns

| Concept | Where it lives | Rule in one line |
| --- | --- | --- |
| **ProductContent** (one per catalogue item) | `internal/domain/content` | The editable body (description, up to 10 bullets, 50 attributes, 12 media, primary category) with a lifecycle `DRAFT`, `PUBLISHED`, `RETIRED` and a `revision`. It publishes only when complete: a description, exactly one `PRIMARY` medium and a category in the current tree. `RETIRED` is terminal. |
| **CategoryTree** (one, id `main`) | `internal/domain/category` | The single live, versioned tree of categories, at most four levels deep and 5000 categories, with unique sibling names. There is no draft: every command applies to the live tree and publishes a new version. |
| **CatalogItemView** | `internal/application/readmodel`, table `catalog_item_view` | Event-fed local copy of the items offer-availability reports (`ACTIVE` or `DISCONTINUED`), so completeness checks never call the network ([ADR 0003](/docs/adr/0003-consumed-contracts-and-local-read-model)). Not aggregate state. |
| **RetireOnDiscontinue** | `internal/application/usecases/consume.go` | When the network discontinues an item, its PUBLISHED content retires with `ProductContentRetired` and a DRAFT retires silently. |

## What it does not own

| Out of scope | Where it belongs |
| --- | --- |
| The item itself: title, dimensions, weight, GTIN, discontinuation | `offer-availability` (network item master) |
| Stock and listings | `offer-availability` |
| Prices and sale windows | `pricing` |
| Search, browse and the product-page read side | `product-discovery`, fed by this context's events |
| Media upload and binary storage | nobody here: media are URLs only |
| Promotions, reviews, recommendations | later phases |
| PII | none: the storefront stores none and no event carries any |
| Anything in the warehouse-systems fleet | the storefront never touches the fleet |

## Classification and area

- **Generic subdomain.** Describing a product is a commodity problem a
  production shop solves by buying a product information management (PIM)
  system; the study builds it anyway so the storefront's event contracts are
  explicit ([Subdomain classification](/docs/ddd/subdomain-classification)).
- **Storefront area**, tier segment `storefront`: CloudEvents types look like
  `com.retailnetwork.storefront.product-content.<entity>.<EventName>`.

## Main capabilities

1. Create a content draft for a known, not discontinued catalogue item and
   revise it (`POST /product-contents`, `/revisions`), up to the collection
   bounds.
2. Publish the current revision (`/publications`): a full-content
   `ProductContentPublished` event reaches `product-discovery` without a call
   back. Publishing again after a revision of live content publishes the new
   revision; publishing a revision that is already live changes nothing.
3. Retire published content (`/retirements`) or discard a draft (`DELETE .../draft`); the
   network's `CatalogItemDiscontinued` does the same on its own.
4. Create, move and rename categories (`/categories`, `PUT .../parent`, `PUT .../name`); every
   one publishes the whole tree under a new version, and
   `POST /category-tree-publications` re-sends the tree on demand.
5. Read content (paged, for a projection rebuild) and the category tree over
   REST, or the same two reads as A2A skills (`get_product_content`,
   `list_categories`).
6. Publish `ProductContentPublished`, `ProductContentRetired` and
   `CategoryTreePublished` on `retailnetwork.product-content.events` through
   the transactional outbox.

## Where to go next

| You want to | Read |
| --- | --- |
| See the layers, packages, binaries and the rules that police them | [Architecture](/docs/overview/architecture) |
| Run it and make the first calls | [Quickstart](/docs/overview/quickstart) |
| Know every setting | [Configuration](/docs/operations/configuration) |
| Deploy and operate it | [Runbook](/docs/operations/runbook), [Observability](/docs/operations/observability), [Before the first release](/docs/operations/before-first-release) |
| Fix something that broke | [Troubleshooting](/docs/operations/troubleshooting) |
| Know what tests and CI gates exist | [Testing](/docs/development/testing) |
| See who talks to whom | [Integration](/docs/ecosystem/integration) |
| See the application use cases and their invariants | [Use cases](/docs/ddd/use-cases) |
| Read the design in ddd-crew artifacts | [DDD artifacts](/docs/ddd/ddd-artifacts), [Ubiquitous language](/docs/ddd/ubiquitous-language) |
| Read the contracts | [API overview](/docs/api-reference), [Event catalogue](/docs/api-reference/events) |
| Read the decisions | [ADR index](/docs/adr) |

Published at [https://iqvo.github.io/product-content/](https://iqvo.github.io/product-content/).

:::note[Disclaimer]
This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.
:::
