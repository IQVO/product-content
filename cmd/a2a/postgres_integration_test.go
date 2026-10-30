//go:build integration

// Integration test for cmd/a2a over REAL Postgres: buildDeps (the same
// migrations + pool + ping the binary runs at boot) behind newAgent, the same
// composition run() serves, driven by the SDK's own client through the real
// router. It proves the A2A skills answer from Postgres-backed use cases, end
// to end, and that the read-only agent leaves the outbox untouched.
//
// Postgres comes from testcontainers (internal/testing/pgtest); never an
// external DATABASE_URL, never t.Skip.
package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"github.com/IQVO/product-content/internal/adapters/outbound/clock"
	"github.com/IQVO/product-content/internal/adapters/outbound/ids"
	outboundkafka "github.com/IQVO/product-content/internal/adapters/outbound/kafka"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

// seedWriteSide builds the write-side use cases the REST API uses (this test
// seeds through them; the agent under test never does) and publishes one item.
func seedWriteSide(t *testing.T, url string) (catID string) {
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
	meta := usecases.Meta{EventID: "ev-sku-a2a", Type: "type.A", At: time.Now()}
	if err := (usecases.Consumer{Deps: d}).ApplyCatalogItemCreated(ctx, meta, usecases.CatalogItemCreated{CatalogItemID: "sku-a2a"}); err != nil {
		t.Fatal(err)
	}
	cat, err := usecases.CreateCategory{Deps: d}.Execute(ctx, usecases.CreateCategoryInput{Name: "Bottles"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = usecases.CreateProductContent{Deps: d}.Execute(ctx, usecases.ContentInput{CatalogItemID: "sku-a2a", Body: content.Body{
		Description: "A steel bottle.", Bullets: []string{"Double-wall"},
		Attributes:        []content.Attribute{{Name: "material", Value: "steel"}},
		Media:             []content.Media{{MediaID: "m1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary}},
		PrimaryCategoryID: cat.CategoryID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (usecases.PublishContent{Deps: d}).Execute(ctx, "sku-a2a"); err != nil {
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

func dataOf(t *testing.T, resp a2a.SendMessageResult) map[string]any {
	t.Helper()
	msg, ok := resp.(*a2a.Message)
	if !ok || len(msg.Parts) < 1 {
		t.Fatalf("reply = %T, want a message", resp)
	}
	d, ok := msg.Parts[0].Content.(a2a.Data)
	if !ok {
		t.Fatalf("first part is %T, want data", msg.Parts[0].Content)
	}
	raw, _ := json.Marshal(d.Value)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestA2AReadsFromRealPostgresAndWritesNothing(t *testing.T) {
	url := pgtest.URL(t)
	catID := seedWriteSide(t, url)
	totalBefore, unpublishedBefore := outboxCounts(t, url)
	if totalBefore == 0 {
		t.Fatal("seeding published nothing: the proof below would be vacuous")
	}

	// The binary's own boot path: migrations (a no-op on the migrated
	// template) + pool + ping, then the composition run() serves.
	logger := newLogger("error")
	deps, closeAdapters, err := buildDeps(context.Background(), logger, url, url)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	t.Cleanup(closeAdapters)
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	srv.Config.Handler = newAgent(deps, srv.URL+"/", "test", "product-content-a2a")

	// Discovery through the real router: the card is fetched from the
	// well-known path and the client is built from the FETCHED card.
	card, err := agentcard.DefaultResolver.Resolve(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("resolve the agent card: %v", err)
	}
	client, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatal(err)
	}
	send := func(skill string, args map[string]any) map[string]any {
		resp, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": skill, "arguments": args})),
		})
		if err != nil {
			t.Fatalf("SendMessage %s: %v", skill, err)
		}
		return dataOf(t, resp)
	}

	got := send("get_product_content", map[string]any{"catalogItemId": "sku-a2a"})
	pc, _ := got["data"].(map[string]any)["productContent"].(map[string]any)
	if pc["state"] != "PUBLISHED" || pc["primaryCategoryId"] != catID || pc["description"] != "A steel bottle." {
		t.Fatalf("get_product_content = %v", got)
	}
	tree, _ := send("list_categories", map[string]any{})["data"].(map[string]any)["categoryTree"].(map[string]any)
	if cats, _ := tree["categories"].([]any); len(cats) != 1 || tree["version"] != float64(1) {
		t.Fatalf("list_categories = %v", tree)
	}
	missing := send("get_product_content", map[string]any{"catalogItemId": "ghost"})
	if p, _ := missing["problem"].(map[string]any); p["status"] != float64(404) || !strings.HasSuffix(p["type"].(string), "/not-found") {
		t.Fatalf("an unknown item must be a 404 problem, got %v", missing)
	}

	totalAfter, unpublishedAfter := outboxCounts(t, url)
	if totalAfter != totalBefore || unpublishedAfter != unpublishedBefore {
		t.Fatalf("outbox rows %d/%d unpublished before, %d/%d after: a read-only agent wrote", totalBefore, unpublishedBefore, totalAfter, unpublishedAfter)
	}
}
