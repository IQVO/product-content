---
id: subdomain-classification
title: Subdomain classification
sidebar_label: Subdomain classification
sidebar_position: 2
---

# Subdomain classification

`product-content` is a **Generic** subdomain in the **Storefront** area (CloudEvents
tier segment `storefront`). The verdict is the one in the storefront table of the
warehouse-docs aggregator
([Subdomain classification](https://iqvo.github.io/warehouse-docs/storefront/strategic-design/subdomain-classification),
`storefront/docs/strategic-design/subdomain-classification.md` on `main`), and it
matches the strategic classification the model gives the context
(`classification: Generic`, business model *engagement enabler*, evolution
*commodity*). The neighbour rows below are copied from that table, not recalled.

## Why Generic

- **Not Core**: no shopper chooses this storefront for how its content records
  work. The context makes no selling decision: it never prices, never promises
  delivery and never decides what is in stock. Its rules are ordinary
  content-management rules, listed on [Use cases](/docs/ddd/use-cases): publish
  only complete content (a description, exactly one `PRIMARY` medium, a category),
  keep the category tree acyclic, at most four levels deep and free of duplicate
  sibling names, and take content down when the catalogue item is discontinued.
- **Not Supporting**: describing a product is a commodity problem. A production
  shop buys a product information management (PIM) system; the study builds it
  anyway so that the storefront's event contracts are explicit and testable. The
  aggregator's rationale is "buy a PIM in production".
- **Business model**: engagement enabler. The context earns nothing by itself; its
  value is the conversion it does not lose, because `product-discovery` counts
  PUBLISHED content as one condition of a purchasable item.
- **Evolution**: commodity. The expected path for a Generic subdomain applies: in
  production this context would shrink to an adapter that republishes a bought
  PIM's content as `ProductContentPublished`, `ProductContentRetired` and
  `CategoryTreePublished`. The interface to keep stable is those three events and
  the three reads `product-discovery` uses to rebuild its projection
  ([Integration](/docs/ecosystem/integration)).

The model complexity is low to moderate, see the
[Core domain chart](/docs/ddd/core-domain-chart): two aggregates, one lifecycle of
three states, one structural tree, one consumed topic and no synchronous call to
a sibling.

## Neighbours (from the storefront table)

| Bounded context | Classification | CloudEvents tier | Relation to this context |
| --- | --- | --- | --- |
| `product-discovery` | Supporting | `storefront` | downstream: consumes this context's events and reads its REST pages |
| `offer-availability` | Supporting | network, commerce tier (`com.retailnetwork.commerce.`) | upstream: item master, source of `CatalogItemCreated` and `CatalogItemDiscontinued` |
| `pricing` | Core | `storefront` | no edge: a price never names content |
| `shopping-cart` | Supporting | `storefront` | no edge |
| `checkout` | Supporting | `storefront` | no edge |
| `customer-payments` | Generic | `storefront` | no edge |
| `product-content` | **Generic** | `storefront` | this context |

`offer-availability` is a network context: it is not in the storefront table, so
its row is taken from the network's own documentation and the commerce segment of
the event types this repository consumes
(`internal/adapters/inbound/kafka/offer_availability.go`). The storefront table
counts three Supporting, two Generic and one Core context. Order management and
customer accounts are deliberately absent from the storefront: the network owns
them. No context of the warehouse-systems fleet (WMS, WES) is a neighbour: this
context consumes no `warehouse.*` topic and calls no fleet service.

## Tier is not classification

`storefront` is the tier segment of the event types
(`com.retailnetwork.storefront.product-content.<entity>.<EventName>`, constants
in `internal/adapters/kafka/cloudevents/cloudevents.go`). It groups contexts by
area; it does not say Core, Supporting or Generic. For this context the two axes
read: area Storefront, class Generic.
