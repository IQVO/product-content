// Package a2a is the inbound Agent2Agent adapter: it exposes this bounded
// context to the fleet's agent ecosystem as a driving adapter over the same
// application-layer READ use cases the REST adapter uses. It is built on the
// official A2A Go SDK (a2aproject/a2a-go) and speaks JSON-RPC 2.0 over HTTP,
// with the Agent Card served at the well-known path.
//
// Per ADR 0006, A2A is the DELEGATION channel between bounded contexts: an
// agent client sends a goal-shaped skill request as a structured message part
// and this agent answers from its own state through its own use cases. It is
// not a data-sync channel; facts between contexts still travel exclusively as
// CloudEvents on Kafka (ADR 0004).
//
// This agent is READ-ONLY. The context has no MCP surface to derive a write
// from, so both skills are backed by a use case a REST GET already exposes
// (GetProductContent, ListCategories) and nothing here can change state, enqueue
// an outbox row or publish an event. It depends inward on the application layer
// and the domain only, never on an outbound adapter, and it holds no outbound
// A2A client. The composition root (cmd/a2a) wires the concrete repositories
// into the use cases. Domain structs never leak across the message boundary.
package a2a

import (
	"context"
	"iter"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/IQVO/product-content/internal/application/usecases"
)

// Config carries the deployment-specific identity of this agent: the absolute
// URL clients use to reach its JSON-RPC endpoint (advertised on the Agent
// Card) and the card version. It is supplied by the composition root from
// A2A_AGENT_URL; the card MUST advertise a URL the fleet's agent clients can
// actually dial (in-cluster: http://<release>-a2a.<ns>.svc.cluster.local:8093/).
type Config struct {
	AgentURL string
	Version  string
}

// Deps is everything the A2A skills need, injected by the composition root.
// Both are existing READ use cases, reused unchanged; the adapter never
// constructs an outbound adapter itself and holds no write use case.
type Deps struct {
	// GetProductContent is the query behind GET /product-contents/{catalogItemId}.
	GetProductContent *usecases.GetProductContent
	// ListCategories is the query behind GET /categories.
	ListCategories *usecases.ListCategories
}

// Card builds this agent's Agent Card (ADR 0006). The surface is two curated,
// goal-shaped read skills, not one skill per endpoint. The card declares no
// security schemes on purpose: this surface is unauthenticated by the same
// fleet-wide decision as REST; schemes are added here, not in a redesign, if
// that decision is ever reversed.
func Card(cfg Config) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "product-content",
		Description: "Product Content agent for the storefront domain: answers what a product page needs for one catalogue item (description, bullets, attributes, media and primary category, with its draft/published/retired state and revisions) and what the category tree looks like (every category with its parent and path, at the current tree version). Read-only: it never creates, revises, publishes or retires content and never changes the category tree.",
		Version:     cfg.Version,
		Provider: &a2a.AgentProvider{
			Org: "IQVO",
			URL: "https://github.com/IQVO/product-content",
		},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(cfg.AgentURL, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "text/plain"},
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
		Skills: []a2a.AgentSkill{
			{
				ID:          skillGetProductContent,
				Name:        "Product content of a catalogue item",
				Description: "Reads the product content of one catalogue item exactly as GET /product-contents/{catalogItemId} returns it: state (DRAFT, PUBLISHED or RETIRED), description, bullets, attributes, media, primary category, revision and published revision. Answers a not-found problem when the item has no content. Changes nothing.",
				Tags:        []string{"product-content", "catalogue", "read"},
				InputModes:  []string{"application/json"},
				Examples: []string{
					`{"skill":"get_product_content","arguments":{"catalogItemId":"item-123"}}`,
				},
			},
			{
				ID:          skillListCategories,
				Name:        "The category tree",
				Description: "Reads the whole live category tree exactly as GET /categories returns it: the tree version and every category with its id, parent id, name and path from the root. Before the first category the tree is version 0 and empty. Takes no arguments. Changes nothing.",
				Tags:        []string{"product-content", "categories", "read"},
				InputModes:  []string{"application/json"},
				Examples: []string{
					`{"skill":"list_categories","arguments":{}}`,
				},
			},
		},
	}
}

// NewExecutor returns the AgentExecutor that maps an incoming message to a
// skill and answers with a single agent Message (a message-only agent in A2A
// terms): both curated skills are transactional request/response reads, so
// there is no long-running Task lifecycle to track. The response carries the
// caller's contextId (and taskId when the client referenced one) so a
// delegating saga can group its interactions.
func NewExecutor(deps Deps) a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
		return func(yield func(a2a.Event, error) bool) {
			yield(answer(ctx, deps, ec), nil)
		}
	})
}

// Handler wires the executor into the transport-agnostic A2A request handler
// and wraps it in the JSON-RPC 2.0 HTTP handler, the only transport this agent
// ships (ADR 0006).
func Handler(executor a2asrv.AgentExecutor) http.Handler {
	return a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor))
}
