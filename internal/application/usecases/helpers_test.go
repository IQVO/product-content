package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/adapters/outbound/memory"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/application/views"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

func at(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

var bg = context.Background

type fx struct {
	t     *testing.T
	store *memory.Store
	clock *memory.Clock
	deps  usecases.Deps
}

func newFx(t *testing.T) *fx {
	t.Helper()
	f := &fx{t: t, store: memory.NewStore(memory.NewClock(t0)), clock: memory.NewClock(t0)}
	s := f.store
	f.deps = usecases.Deps{
		Contents: s.Contents(), Categories: s.Categories(), Items: s.Items(), Inbox: s.Inbox(), Outbox: s.Outbox(),
		UoW: s.UnitOfWork(), Clock: f.clock, IDs: &memory.SequentialIDs{},
	}
	return f
}

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

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// item makes the catalogue item known (ACTIVE) through the consumer.
func (f *fx) item(id string) {
	f.t.Helper()
	must(f.t, usecases.Consumer{Deps: f.deps}.ApplyCatalogItemCreated(bg(), meta("ev-created-"+id), usecases.CatalogItemCreated{CatalogItemID: id}))
}

func meta(eventID string) usecases.Meta {
	return usecases.Meta{EventID: eventID, Type: "type.A", At: t0}
}

// category creates a root category and returns its id.
func (f *fx) category(name string) string {
	f.t.Helper()
	c, err := usecases.CreateCategory{Deps: f.deps}.Execute(bg(), usecases.CreateCategoryInput{Name: name})
	must(f.t, err)
	return c.CategoryID
}

func body(categoryID string) content.Body {
	return content.Body{
		Description: "A 2-litre insulated steel bottle.",
		Bullets:     []string{"Double-wall"},
		Attributes:  []content.Attribute{{Name: "capacity", Value: "2", Unit: "l"}},
		Media:       []content.Media{{MediaID: "med-1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: content.RolePrimary}},

		PrimaryCategoryID: categoryID,
	}
}

// create makes a known item and a DRAFT with a full body; it returns the category id.
func (f *fx) create(id string) (catID string) {
	f.t.Helper()
	f.item(id)
	catID = f.category("Bottles " + id)
	_, err := usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: content.ID(id), Body: body(catID)})
	must(f.t, err)
	return catID
}

// publish creates and publishes content.
func (f *fx) publish(id string) (catID string) {
	f.t.Helper()
	catID = f.create(id)
	_, err := usecases.PublishContent{Deps: f.deps}.Execute(bg(), content.ID(id))
	must(f.t, err)
	return catID
}

func (f *fx) events() []shared.Event { return f.store.Outbox().Events() }

func (f *fx) get(id string) views.Content {
	f.t.Helper()
	v, err := usecases.GetProductContent{Deps: f.deps}.Execute(bg(), content.ID(id))
	must(f.t, err)
	return v
}
