package mcp

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/IQVO/product-content/internal/application/usecases"
)

// maxIDLength bounds a catalogue item id, as the REST and A2A adapters do: a
// longer one cannot name a resource.
const maxIDLength = 128

// Deps is everything the MCP tools need, injected by the composition root.
// Both are existing READ use cases, reused unchanged: GetProductContent backs
// GET /product-contents/{catalogItemId} and ListCategories backs GET
// /categories (the same two the A2A agent uses). The adapter holds these and
// nothing that can write (docs/adr/0008): the composition root (cmd/mcp)
// wires no write seam at all.
type Deps struct {
	GetProductContent *usecases.GetProductContent
	ListCategories    *usecases.ListCategories
}

// --- inputs -------------------------------------------------------------------

// catalogItemInput mirrors the path parameter of GET
// /product-contents/{catalogItemId}.
type catalogItemInput struct {
	CatalogItemID string `json:"catalogItemId" jsonschema:"the catalogue item id whose product content is read, e.g. item-123"`
}

// noInput is the input of a tool that takes no argument: GET /categories has
// no parameter, the tree is one versioned aggregate.
type noInput struct{}

// --- registry -----------------------------------------------------------------

// registerTools adds the two read-only tools, one per read use case that
// backs a REST GET of apis/openapi.yaml (the paged GET /product-contents is
// the projection bootstrap of product-discovery and is deliberately not a
// tool, ADR 0008). Every tool is annotated read-only, idempotent and
// closed-world (the annotation literal is inlined per tool so the fleet's
// static authority sensor can prove ReadOnlyHint) and declares its authority
// level L1 under com.warehouse/authority (Agentic Standard v2): each is a pure
// read of this context's own state. TestToolSurface pins the set and fails the
// build on any write-verb name.
func (d Deps) registerTools(server *mcp.Server) {
	closedWorld := false

	addTool(server, &mcp.Tool{
		Name: "get_product_content",
		Description: "Read the product content of one catalogue item exactly as GET /product-contents/{catalogItemId} returns it: the state " +
			"(DRAFT, PUBLISHED or RETIRED, with retiredReason MANUAL, DISCARDED or DISCONTINUED when retired), the description, bullets, " +
			"attributes (name, value, optional unit), media (mediaId, url, altText, role PRIMARY or GALLERY), the primary category id, " +
			"the revision of the current shape and the publishedRevision shoppers see (0 when never published), with the created, updated, " +
			"published and retired instants. DRAFT and RETIRED content are answered too. Read-only; an unknown item fails with not-found.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.getProductContent)

	addTool(server, &mcp.Tool{
		Name: "list_categories",
		Description: "Read the whole live category tree exactly as GET /categories returns it: the tree version and every category with its " +
			"id, parent id (omitted for a root), name and path from the root, parents before children. Before the first category the tree " +
			"is version 0 and empty. Takes no arguments. Read-only.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.listCategories)
}

// addTool registers one tool. A handler error is returned to the SDK as the
// handler's error, which the SDK turns into an isError tool result (never a
// transport/protocol failure), carrying the error text.
func addTool[In, Out any](
	server *mcp.Server,
	tool *mcp.Tool,
	handle func(context.Context, In) (Out, error),
) {
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := handle(ctx, in)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		return nil, out, nil
	})
}

// checkItemID mirrors the REST route's identifier rule: an id that is blank
// is a missing argument; one over 128 characters or carrying a NUL (which
// Postgres text cannot hold) can never name a resource, so it is a not-found
// that never reaches the repository.
func checkItemID(id string) error {
	if strings.TrimSpace(id) == "" {
		return toolError("invalid-argument", "catalogItemId is required")
	}
	if len(id) > maxIDLength || strings.ContainsRune(id, 0) {
		return toolError("not-found", "no product content matches the catalogItemId")
	}
	return nil
}

// instant formats an optional instant; the zero time is nil (omitted), like
// the REST adapter's tptr.
func instant(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}
