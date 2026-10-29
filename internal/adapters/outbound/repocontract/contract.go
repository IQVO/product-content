// Package repocontract is the repository contract suite: the behaviours every
// persistence adapter (the in-memory doubles and Postgres) must show. It is
// test support (it imports "testing") kept out of _test.go files so both
// adapters can run it; the shipped binary never links it (enforced by
// internal/architecture).
package repocontract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/content"
)

// Env is what an adapter hands to the suite. Every test gets a fresh Env.
type Env struct {
	Contents    ports.ContentRepository
	Categories  ports.TreeRepository
	Items       ports.CatalogItemRepository
	Inbox       ports.Inbox
	Outbox      ports.EventOutbox
	UoW         ports.UnitOfWork
	Idempotency ports.IdempotencyStore
	// Clock is the store's time authority: testIdempotency stamps records
	// with it, so the expired-record case expires relative to the SAME clock
	// the implementation under test judges TTL with (the memory double is
	// driven by a frozen clock; the Postgres store by the database's now()).
	Clock ports.Clock
	// OutboxCount returns how many events the outbox holds.
	OutboxCount func(t *testing.T) int
}

// Run executes the whole suite; newEnv must return an empty Env each time.
func Run(t *testing.T, newEnv func(t *testing.T) Env) {
	t.Helper()
	suites := map[string]func(*testing.T, Env){
		"contents":    testContents,
		"tree":        testTree,
		"items":       testItems,
		"inbox":       testInbox,
		"unitofwork":  testUnitOfWork,
		"idempotency": testIdempotency,
	}
	for name, fn := range suites {
		t.Run(name, func(t *testing.T) { fn(t, newEnv(t)) })
	}
}

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

// at returns t0 plus m minutes.
func at(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

var bg = context.Background

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantErr(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// treeOf is the category lookup of the fixtures: every id is known.
type treeOf map[string]bool

func (c treeOf) Has(id string) bool { return c[id] }

var knownCategories = treeOf{"cat-bottles": true, "cat-cups": true}

func fullBody() content.Body {
	return content.Body{
		Description: "A 2-litre insulated steel bottle.",
		Bullets:     []string{"Double-wall", "Dishwasher-safe cap"},
		Attributes:  []content.Attribute{{Name: "capacity", Value: "2", Unit: "l"}, {Name: "material", Value: "steel"}},
		Media: []content.Media{
			{MediaID: "med-1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary},
			{MediaID: "med-2", URL: "https://static.example.invalid/b.jpg", AltText: "cap", Role: content.RoleGallery},
		},
		PrimaryCategoryID: "cat-bottles",
	}
}

// newContent builds a DRAFT with a full body.
func newContent(t *testing.T, id string) *content.ProductContent {
	t.Helper()
	c, err := content.Create(content.ID(id), fullBody(), content.ItemActive, knownCategories, t0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return c
}
