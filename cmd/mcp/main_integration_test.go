//go:build integration

// Integration test for cmd/mcp over REAL Postgres: buildDeps (the same
// migrations + pool + ping the binary runs at boot) behind the real router,
// the same composition run() serves, driven by the SDK's own client over
// Streamable HTTP. It proves the binary boots, lists exactly the read-only
// tools, reads what the write use cases persisted, answers /healthz and adds
// no outbox row.
//
// Postgres comes from testcontainers (internal/testing/pgtest); never an
// external DATABASE_URL, never t.Skip.
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/IQVO/product-content/internal/adapters/inbound/mcp"
	"github.com/IQVO/product-content/internal/adapters/outbound/clock"
	"github.com/IQVO/product-content/internal/adapters/outbound/ids"
	outboundkafka "github.com/IQVO/product-content/internal/adapters/outbound/kafka"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Shutdown()
	os.Exit(code)
}

// seedWriteSide builds the write-side use cases the REST API uses (this test
// seeds through them; the MCP binary under test never does) and publishes one
// item. It returns the category id.
func seedWriteSide(t *testing.T, url string) string {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	d := usecases.Deps{
		Contents: postgres.NewContentRepository(pool), Categories: postgres.NewTreeRepository(pool),
		Items: postgres.NewCatalogItemRepository(pool), Inbox: postgres.NewInbox(pool),
		Outbox: postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder()), UoW: postgres.NewUnitOfWork(pool),
		Clock: clock.System{}, IDs: ids.Generator{},
	}
	meta := usecases.Meta{EventID: "ev-sku-mcp", Type: "type.A", At: time.Now()}
	if err := (usecases.Consumer{Deps: d}).ApplyCatalogItemCreated(ctx, meta, usecases.CatalogItemCreated{CatalogItemID: "sku-mcp"}); err != nil {
		t.Fatal(err)
	}
	cat, err := usecases.CreateCategory{Deps: d}.Execute(ctx, usecases.CreateCategoryInput{Name: "Bottles"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = usecases.CreateProductContent{Deps: d}.Execute(ctx, usecases.ContentInput{CatalogItemID: "sku-mcp", Body: content.Body{
		Description: "A steel bottle.", Bullets: []string{"Double-wall"},
		Attributes:        []content.Attribute{{Name: "material", Value: "steel"}},
		Media:             []content.Media{{MediaID: "m1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary}},
		PrimaryCategoryID: cat.CategoryID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (usecases.PublishContent{Deps: d}).Execute(ctx, "sku-mcp"); err != nil {
		t.Fatal(err)
	}
	return cat.CategoryID
}

func outboxCounts(t *testing.T, url string) (total, unpublished int) {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	row := pool.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE published_at IS NULL) FROM outbox_events`)
	if err := row.Scan(&total, &unpublished); err != nil {
		t.Fatal(err)
	}
	return total, unpublished
}

func TestMCPAgainstPostgres_ReadsWhatTheAPIWroteAndNeverWrites(t *testing.T) {
	url := pgtest.URL(t) // a fresh, already migrated database
	ctx := context.Background()
	catID := seedWriteSide(t, url)
	totalBefore, unpublishedBefore := outboxCounts(t, url)
	if totalBefore == 0 {
		t.Fatal("seeding published nothing: the proof below would be vacuous")
	}

	// The binary's own boot path: migrations (a no-op on the migrated
	// template) + pool + ping, then the composition run() serves.
	deps, closeAdapters, err := buildDeps(ctx, newLogger("error"), url, url)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	t.Cleanup(closeAdapters)
	srv := httptest.NewServer(newRouter(inboundmcp.Handler(inboundmcp.NewServer(deps)), newLogger("error"), "product-content-mcp"))
	t.Cleanup(srv.Close)

	// /healthz is open.
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != `{"status":"ok"}` {
		t.Fatalf("GET /healthz = %d %q", resp.StatusCode, body)
	}

	session, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	for _, tool := range tools.Tools {
		if tool.Meta["com.warehouse/authority"] != "L1" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q is not an L1 read-only tool: meta=%v annotations=%+v", tool.Name, tool.Meta, tool.Annotations)
		}
	}
	if got := strings.Join(names, ","); got != "get_product_content,list_categories" {
		t.Fatalf("tools/list = %s, want exactly get_product_content,list_categories", got)
	}

	got := callOK(t, session, "get_product_content", map[string]any{"catalogItemId": "sku-mcp"})
	if got["state"] != "PUBLISHED" || got["primaryCategoryId"] != catID || got["description"] != "A steel bottle." {
		t.Fatalf("get_product_content = %v", got)
	}
	tree := callOK(t, session, "list_categories", map[string]any{})
	if cats, _ := tree["categories"].([]any); len(cats) != 1 || tree["version"] != float64(1) {
		t.Fatalf("list_categories = %v", tree)
	}
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "get_product_content", Arguments: map[string]any{"catalogItemId": "ghost"}})
	if err != nil || !res.IsError {
		t.Fatalf("an unknown item must be a tool error, got %+v, %v", res, err)
	}

	totalAfter, unpublishedAfter := outboxCounts(t, url)
	if totalAfter != totalBefore || unpublishedAfter != unpublishedBefore {
		t.Fatalf("outbox rows %d/%d unpublished before, %d/%d after: a read-only server wrote", totalBefore, unpublishedBefore, totalAfter, unpublishedAfter)
	}
}

func callOK(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: transport error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned a tool error", name)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s: structured content = %#v, want an object", name, res.StructuredContent)
	}
	return m
}
