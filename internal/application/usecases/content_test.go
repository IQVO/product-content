package usecases_test

import (
	"testing"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

func TestCreateProductContent(t *testing.T) {
	f := newFx(t)
	f.item("item-1")
	cat := f.category("Bottles")
	before := len(f.events())
	v, err := usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: body(cat)})
	must(t, err)
	eq(t, "state", v.State, "DRAFT")
	eq(t, "revision", v.Revision, 1)
	eq(t, "category", v.PrimaryCategoryID, cat)
	eq(t, "media", len(v.Media), 1)
	eq(t, "attributes", len(v.Attributes), 1)
	eq(t, "no event", len(f.events()), before)
	eq(t, "stored", f.get("item-1").Revision, 1)
}

func TestCreateProductContentRefusals(t *testing.T) {
	f := newFx(t)
	f.item("item-1")
	cat := f.category("Bottles")
	uc := usecases.CreateProductContent{Deps: f.deps}
	counts := f.store.Counts()

	_, err := uc.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-unknown", Body: body(cat)})
	wantErr(t, err, content.ErrUnknownCatalogItem)
	_, err = uc.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: body("cat-nope")})
	wantErr(t, err, content.ErrUnknownCategory)
	must(t, usecases.Consumer{Deps: f.deps}.ApplyCatalogItemDiscontinued(bg(), meta("ev-d"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"}))
	_, err = uc.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: body(cat)})
	wantErr(t, err, content.ErrItemDiscontinued)
	after := f.store.Counts()
	eq(t, "contents", after.Contents, counts.Contents)
}

func TestCreateProductContentTwiceIsAConflict(t *testing.T) {
	f := newFx(t)
	cat := f.create("item-1")
	_, err := usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: body(cat)})
	wantErr(t, err, ports.ErrConcurrentUpdate)
}

func TestCreateWithoutAnyCategoryTreeIsAnUnknownCategory(t *testing.T) {
	f := newFx(t)
	f.item("item-1")
	_, err := usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: body("cat-1")})
	wantErr(t, err, content.ErrUnknownCategory)
	_, err = usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: content.Body{}})
	must(t, err) // a bare draft needs no tree
}

func TestReviseDispatchesOnTheState(t *testing.T) {
	f := newFx(t)
	cat := f.create("item-1")
	uc := usecases.ReviseContent{Deps: f.deps}
	nb := body(cat)
	nb.Description = "Draft edit."
	v, err := uc.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: nb})
	must(t, err)
	eq(t, "draft state", v.State, "DRAFT")
	eq(t, "draft revision", v.Revision, 2)

	_, err = usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	nb.Description = "Published edit."
	v, err = uc.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: nb})
	must(t, err)
	eq(t, "published state", v.State, "PUBLISHED")
	eq(t, "published revision", v.Revision, 3)
	eq(t, "publishedRevision", v.PublishedRevision, 2)
	eq(t, "description", v.Description, "Published edit.")

	nb.Description = ""
	_, err = uc.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: nb})
	wantErr(t, err, content.ErrIncompleteContent)
}

func TestReviseRefusals(t *testing.T) {
	f := newFx(t)
	_, err := usecases.ReviseContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-none", Body: content.Body{}})
	wantErr(t, err, ports.ErrNotFound)

	cat := f.create("item-1")
	_, err = usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	_, err = usecases.ReviseContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: body(cat)})
	wantErr(t, err, content.ErrContentRetired)

	f.create("item-2")
	_, err = usecases.ReviseContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-2", Body: body("cat-nope")})
	wantErr(t, err, content.ErrUnknownCategory)
	eq(t, "unchanged revision", f.get("item-2").Revision, 1)
}

func TestPublishRaisesTheEventAndIsIdempotentPerRevision(t *testing.T) {
	f := newFx(t)
	cat := f.create("item-1")
	v, err := usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "state", v.State, "PUBLISHED")
	eq(t, "publishedRevision", v.PublishedRevision, 1)
	evs := f.events()
	eq(t, "events", len(evs), 2) // CategoryTreePublished (the category) + ProductContentPublished
	e, ok := evs[1].(content.ProductContentPublished)
	if !ok || e.Revision != 1 || e.PrimaryCategoryID != cat || e.CatalogItemID != "item-1" {
		t.Fatalf("event = %#v", evs[1])
	}

	again, err := usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "no-op version", again.UpdatedAt, v.UpdatedAt)
	eq(t, "no new event", len(f.events()), 2)

	nb := body(cat)
	nb.Bullets = []string{"New bullet"}
	_, err = usecases.ReviseContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: nb})
	must(t, err)
	v, err = usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "second publishedRevision", v.PublishedRevision, 2)
	eq(t, "second event", len(f.events()), 3)
	eq(t, "event revision", f.events()[2].(content.ProductContentPublished).Revision, 2)
}

func TestPublishRefusals(t *testing.T) {
	f := newFx(t)
	_, err := usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-none")
	wantErr(t, err, ports.ErrNotFound)

	f.item("item-1")
	_, err = usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: content.Body{Description: "only text"}})
	must(t, err)
	_, err = usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	wantErr(t, err, content.ErrIncompleteContent)

	f.create("item-2")
	must(t, usecases.Consumer{Deps: f.deps}.ApplyCatalogItemDiscontinued(bg(), meta("ev-d"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-2"}))
	// the policy retired the draft, so the content is RETIRED before the item check
	_, err = usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-2")
	wantErr(t, err, content.ErrContentRetired)
}

func TestPublishOfADiscontinuedItemIsRefused(t *testing.T) {
	f := newFx(t)
	f.create("item-1")
	// A DRAFT that survived because the view changed without the policy: store the
	// DISCONTINUED mark directly.
	must(t, f.store.Items().MarkDiscontinued(bg(), "item-1", at(1)))
	_, err := usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	wantErr(t, err, content.ErrItemDiscontinued)
}

func TestRetireContent(t *testing.T) {
	f := newFx(t)
	f.publish("item-1")
	v, err := usecases.RetireContent{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "state", v.State, "RETIRED")
	eq(t, "reason", v.RetiredReason, "MANUAL")
	last := f.events()[len(f.events())-1]
	if _, ok := last.(content.ProductContentRetired); !ok {
		t.Fatalf("last event = %#v", last)
	}
	_, err = usecases.RetireContent{Deps: f.deps}.Execute(bg(), "item-1")
	wantErr(t, err, shared.ErrIllegalTransition)
	_, err = usecases.RetireContent{Deps: f.deps}.Execute(bg(), "item-none")
	wantErr(t, err, ports.ErrNotFound)
}

func TestDiscardDraftRaisesNoEvent(t *testing.T) {
	f := newFx(t)
	f.create("item-1")
	before := len(f.events())
	v, err := usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "state", v.State, "RETIRED")
	eq(t, "reason", v.RetiredReason, "DISCARDED")
	eq(t, "no event", len(f.events()), before)
	_, err = usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-1")
	wantErr(t, err, shared.ErrIllegalTransition)
	_, err = usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-none")
	wantErr(t, err, ports.ErrNotFound)

	f.publish("item-2")
	_, err = usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-2")
	wantErr(t, err, shared.ErrIllegalTransition)
}

func TestACategoryRenameKeepsPublishedContentValid(t *testing.T) {
	f := newFx(t)
	cat := f.publish("item-1")
	_, err := usecases.RenameCategory{Deps: f.deps}.Execute(bg(), usecases.RenameCategoryInput{CategoryID: cat, Name: "Renamed"})
	must(t, err)
	nb := body(cat)
	nb.Description = "Edited"
	_, err = usecases.ReviseContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1", Body: nb})
	must(t, err)
	_, err = usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
}
