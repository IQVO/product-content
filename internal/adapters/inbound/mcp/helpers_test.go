package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/IQVO/product-content/internal/adapters/inbound/mcp"
	"github.com/IQVO/product-content/internal/adapters/outbound/memory"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/content"
)

// now is the fixture's fixed "now".
var now = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// world is the seeded product-content every tool test reads: one category,
// one PUBLISHED content (revised once, so revision 2 is waiting), one DRAFT
// content and one RETIRED (discarded) content.
type world struct {
	categoryID, publishedID, draftID, retiredID string
}

// harness is a connected in-memory MCP client over the real server wired to
// the read use cases over in-memory repositories, exactly as cmd/mcp wires
// them (minus Postgres). The write use cases exist ONLY here, to seed data
// the way the REST adapter would: the MCP surface itself has none.
type harness struct {
	session *sdk.ClientSession
	store   *memory.Store
	deps    usecases.Deps
}

func readDeps(d usecases.Deps) inboundmcp.Deps {
	return inboundmcp.Deps{
		GetProductContent: &usecases.GetProductContent{Deps: d},
		ListCategories:    &usecases.ListCategories{Deps: d},
	}
}

func newDeps() (*memory.Store, usecases.Deps) {
	clock := memory.NewClock(now)
	s := memory.NewStore(clock)
	return s, usecases.Deps{
		Contents: s.Contents(), Categories: s.Categories(), Items: s.Items(), Inbox: s.Inbox(), Outbox: s.Outbox(),
		UoW: s.UnitOfWork(), Clock: clock, IDs: &memory.SequentialIDs{},
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, deps := newDeps()
	return &harness{session: connectSession(t, readDeps(deps)), store: store, deps: deps}
}

func connectSession(t *testing.T, deps inboundmcp.Deps) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := inboundmcp.NewServer(deps).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// draft makes the catalogue item known and creates its DRAFT content.
func (h *harness) draft(t *testing.T, id, categoryID string) {
	t.Helper()
	ctx := context.Background()
	meta := usecases.Meta{EventID: "ev-" + id, Type: "type.A", At: now}
	must(t, usecases.Consumer{Deps: h.deps}.ApplyCatalogItemCreated(ctx, meta, usecases.CatalogItemCreated{CatalogItemID: id}))
	_, err := usecases.CreateProductContent{Deps: h.deps}.Execute(ctx, usecases.ContentInput{
		CatalogItemID: content.ID(id),
		Body: content.Body{
			Description: "A 2-litre insulated steel bottle.", Bullets: []string{"Double-wall", "Leak-proof"},
			Attributes:        []content.Attribute{{Name: "capacity", Value: "2", Unit: "l"}, {Name: "colour", Value: "steel"}},
			Media:             []content.Media{{MediaID: "med-1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary}},
			PrimaryCategoryID: categoryID,
		},
	})
	must(t, err)
}

// seedWorld populates the fixture the way the REST adapter's write use cases
// would and returns the ids.
func (h *harness) seedWorld(t *testing.T) world {
	t.Helper()
	ctx := context.Background()
	c, err := usecases.CreateCategory{Deps: h.deps}.Execute(ctx, usecases.CreateCategoryInput{Name: "Drinkware"})
	must(t, err)
	if _, err := (usecases.CreateCategory{Deps: h.deps}).Execute(ctx, usecases.CreateCategoryInput{ParentID: c.CategoryID, Name: "Bottles"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.draft(t, "item-pub", c.CategoryID)
	_, err = usecases.PublishContent{Deps: h.deps}.Execute(ctx, "item-pub")
	must(t, err)
	_, err = usecases.ReviseContent{Deps: h.deps}.Execute(ctx, usecases.ContentInput{
		CatalogItemID: "item-pub",
		Body: content.Body{
			Description: "A 2-litre insulated steel bottle, now in matte.", Bullets: []string{"Double-wall"},
			Media:             []content.Media{{MediaID: "med-1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary}},
			PrimaryCategoryID: c.CategoryID,
		},
	})
	must(t, err)
	h.draft(t, "item-draft", c.CategoryID)
	h.draft(t, "item-gone", c.CategoryID)
	_, err = usecases.DiscardDraft{Deps: h.deps}.Execute(ctx, "item-gone")
	must(t, err)
	return world{categoryID: c.CategoryID, publishedID: "item-pub", draftID: "item-draft", retiredID: "item-gone"}
}

// withWorld is a harness whose world is already seeded.
func withWorld(t *testing.T) (*harness, world) {
	t.Helper()
	h := newHarness(t)
	return h, h.seedWorld(t)
}

// call invokes a tool and fails the test on a TRANSPORT/protocol error;
// tool-level failures come back as res.IsError.
func call(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: transport/protocol error (want a tool result): %v", name, err)
	}
	return res
}

// ok calls a tool, requires success, and returns its structured content.
func (h *harness) ok(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	res := call(t, h.session, name, args)
	if res.IsError {
		t.Fatalf("%s returned a tool error: %s", name, text(res))
	}
	sc, isMap := res.StructuredContent.(map[string]any)
	if !isMap {
		t.Fatalf("%s: structured content = %#v, want an object", name, res.StructuredContent)
	}
	return sc
}

// failWith calls a tool and requires an isError result whose text starts
// with the slug want.
func failWith(t *testing.T, session *sdk.ClientSession, name string, args map[string]any, want string) {
	t.Helper()
	res := call(t, session, name, args)
	if !res.IsError {
		t.Fatalf("%s: expected an isError tool result, got %#v", name, res.StructuredContent)
	}
	if got := text(res); !strings.HasPrefix(got, want+": ") {
		t.Fatalf("%s: error text %q does not start with %q", name, got, want+": ")
	}
}

func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, isText := c.(*sdk.TextContent); isText {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
