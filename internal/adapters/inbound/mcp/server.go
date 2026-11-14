// Package mcp is the inbound Model Context Protocol adapter: it exposes this
// bounded context to the AI ecosystem as a driving adapter over the same
// application-layer READ use cases the REST adapter and the A2A agent use. It
// is built on the official MCP Go SDK and served over Streamable HTTP only.
//
// Per docs/adr/0008-read-only-mcp-server.md the surface is READ-ONLY: every
// tool reads product-content data (get_product_content, list_categories);
// creating, revising, publishing, retiring and discarding content and every
// category write stay on REST and on the outbox, and governance_test.go fails
// the build on a write-verb tool name and on a write seam referenced from this
// package. This package depends inward on the application layer and the
// domain only, never on an outbound adapter or the REST adapter. The
// composition root (cmd/mcp) wires concrete repositories into the use cases.
// There is no auth of any kind (fleet-wide revert 2026-09-11): access control
// is the in-cluster ClusterIP boundary.
package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName and serverVersion identify this server in the MCP initialize
// handshake.
const (
	serverName    = "product-content-mcp"
	serverVersion = "1.0.0"
)

// instructions is what an MCP client is told about this server at initialize.
const instructions = "Product Content of the digital storefront (read-only): the rich catalogue content of each catalogue item (description, bullets, attributes, media, primary category) and the category tree. " +
	"Read one item's content with get_product_content (state DRAFT, PUBLISHED or RETIRED; revision is the current shape and publishedRevision is the one shoppers see, 0 when never published; " +
	"a PUBLISHED content whose revision is above publishedRevision waits for a publication), and read the whole live category tree with list_categories (the tree version and every category with its parent and path). " +
	"The content resource (warehouse://product-content/contents/{catalogItemId}) loads one item's content without a tool call, and the review_product_content prompt hands you this context's operator procedure. " +
	"Nothing here changes data: creating, revising, publishing, retiring and discarding content, and creating, moving, renaming and publishing categories are REST operations of product-content."

// NewServer builds the MCP server for this bounded context with every tool,
// resource and workflow prompt registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions},
	)
	deps.registerTools(server)
	deps.registerResources(server)
	deps.registerPrompts(server)
	return server
}

// Handler returns the Streamable HTTP handler for the MCP server.
func Handler(server *mcp.Server) http.Handler {
	// Stateless (Agentic Standard v2, AS-4): no Mcp-Session-Id is issued, so
	// the server also answers the 2026-07-28 revision's per-request
	// handshake (server/discover) while legacy initialize clients keep
	// working. Nothing here relies on cross-call session state.
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}
