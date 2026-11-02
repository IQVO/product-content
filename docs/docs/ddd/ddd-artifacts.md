---
id: ddd-artifacts
title: DDD artifacts (ddd-crew)
sidebar_label: DDD artifacts index
sidebar_position: 10
---

# DDD artifacts (ddd-crew)

The strategic and tactical design of `product-content`, drawn with the
[ddd-crew](https://github.com/ddd-crew) tools plus UML and ER diagrams. Every
diagram is Mermaid, derived from the code and ADRs on this branch, and carries a
`Source:` line naming the files it came from. The starting material was the
hand-written pack of the warehouse-docs aggregator
([Product Content](https://iqvo.github.io/warehouse-docs/storefront/contexts/product-content/)),
re-checked against the code: where the model and the code differ, these pages follow
the code and say so.

| Artifact | ddd-crew tool / notation | What it shows |
| --- | --- | --- |
| [Core domain chart](/docs/ddd/core-domain-chart) | [Core Domain Charts](https://github.com/ddd-crew/core-domain-charts) | Where this context sits on business differentiation vs model complexity, and why it is Generic. |
| [Bounded context canvas](/docs/ddd/bounded-context-canvas) | [Bounded Context Canvas v5](https://github.com/ddd-crew/bounded-context-canvas) | Purpose, classification, roles, every inbound and outbound message, business decisions. |
| [Context map](/docs/ddd/context-map) | [Context Mapping](https://github.com/ddd-crew/context-mapping) | This context's slice of the storefront map: patterns per edge, with the built status of each side. |
| [Aggregate design canvas](/docs/ddd/aggregate-design-canvas) | [Aggregate Design Canvas v1.1](https://github.com/ddd-crew/aggregate-design-canvas) | `ProductContent` and `CategoryTree`: states, invariants, commands, events, concurrency. |
| [Domain message flow](/docs/ddd/domain-message-flow) | [Domain Message Flow Modelling](https://github.com/ddd-crew/domain-message-flow-modelling) | The key scenarios as numbered command / event / query flows across contexts. |
| [EventStorming](/docs/ddd/eventstorming) | [EventStorming glossary and cheat sheet](https://github.com/ddd-crew/eventstorming-glossary-cheat-sheet) | Process-level boards with the sticky-note colours, plus hotspots and their resolution. |
| [Ubiquitous language](/docs/ddd/ubiquitous-language) | Glossary | Every term with the code identifier that implements it. |
| [Class diagrams](/docs/ddd/class-diagram) | UML class diagram | Domain types, and the hexagonal ports with the adapters that exist. |
| [Entity-relationship diagram](/docs/ddd/entity-relationship) | ER diagram | The Postgres schema created by `0001_product_content_schema.up.sql`: six tables and their keys. |
| [Sequence diagrams](/docs/ddd/sequence-diagrams) | UML sequence diagram | Create and publish, a rejected publish, a category edit, retire on discontinue and the consumer retry path. |
| [Domain events](/docs/ddd/domain-events) | Event catalogue | Every event published and consumed: full CloudEvents type, topic, key, payload, producer, consumers. |

Related pages outside the pack: [Use cases](/docs/ddd/use-cases),
[Subdomain classification](/docs/ddd/subdomain-classification),
[Integration](/docs/ecosystem/integration) and the
[event catalogue](/docs/api-reference/events).

:::note[Sources of truth]
The code wins. Domain rules come from `internal/domain/content`,
`internal/domain/category` and `internal/domain/shared`, behaviour from
`internal/application/usecases`, the envelope from
`internal/adapters/kafka/cloudevents`, the schema from
`internal/adapters/outbound/postgres/migrations/`, the REST contract from
`apis/openapi.yaml`, the event contract from `apis/asyncapi.yaml`, and decisions
from the ADRs in `docs/adr/`. Where a page and the code disagree, the page is
wrong.
:::

:::info Not in this pack
The A2A agent ([ADR 0006](/docs/adr/0006-a2a-inbound-adapter)) is a read surface
over two existing queries and adds no aggregate, entity or event of its own, so the
pack has no page for it; it is on the [API overview](/docs/api-reference). There is
no MCP server and no web remote. The application layer, the HTTP and Kafka
adapters, the outbox relay and the database schema are covered by the pages above.
:::
