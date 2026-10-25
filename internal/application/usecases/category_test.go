package usecases_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/readmodel"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

func TestEveryCategoryCommandPublishesANewTreeVersion(t *testing.T) {
	f := newFx(t)
	home, err := usecases.CreateCategory{Deps: f.deps}.Execute(bg(), usecases.CreateCategoryInput{Name: "Home"})
	must(t, err)
	eq(t, "id", home.CategoryID, "cat-1")
	eq(t, "path", home.Path, "Home")
	kitchen, err := usecases.CreateCategory{Deps: f.deps}.Execute(bg(), usecases.CreateCategoryInput{ParentID: home.CategoryID, Name: "Kitchen"})
	must(t, err)
	eq(t, "child path", kitchen.Path, "Home/Kitchen")
	moved, err := usecases.MoveCategory{Deps: f.deps}.Execute(bg(), usecases.MoveCategoryInput{CategoryID: kitchen.CategoryID})
	must(t, err)
	eq(t, "moved path", moved.Path, "Kitchen")
	renamed, err := usecases.RenameCategory{Deps: f.deps}.Execute(bg(), usecases.RenameCategoryInput{CategoryID: kitchen.CategoryID, Name: "Cooking"})
	must(t, err)
	eq(t, "renamed path", renamed.Path, "Cooking")
	tree, err := usecases.PublishCategoryTree{Deps: f.deps}.Execute(bg())
	must(t, err)
	eq(t, "version", tree.Version, int64(5))
	eq(t, "categories", len(tree.Categories), 2)

	evs := f.events()
	eq(t, "events", len(evs), 5)
	for i, ev := range evs {
		e, ok := ev.(category.CategoryTreePublished)
		if !ok || e.AggregateVersion() != int64(i+1) {
			t.Fatalf("event %d = %#v, want CategoryTreePublished version %d", i, ev, i+1)
		}
	}
}

func TestCategoryRefusalsStoreAndPublishNothing(t *testing.T) {
	f := newFx(t)
	home := f.category("Home")
	counts := f.store.Counts()
	_, err := usecases.CreateCategory{Deps: f.deps}.Execute(bg(), usecases.CreateCategoryInput{ParentID: "cat-nope", Name: "X"})
	wantErr(t, err, category.ErrUnknownParent)
	_, err = usecases.CreateCategory{Deps: f.deps}.Execute(bg(), usecases.CreateCategoryInput{Name: "home"})
	wantErr(t, err, category.ErrDuplicateSiblingName)
	_, err = usecases.MoveCategory{Deps: f.deps}.Execute(bg(), usecases.MoveCategoryInput{CategoryID: "cat-nope"})
	wantErr(t, err, category.ErrCategoryNotFound)
	_, err = usecases.MoveCategory{Deps: f.deps}.Execute(bg(), usecases.MoveCategoryInput{CategoryID: home, ParentID: home})
	wantErr(t, err, category.ErrCategoryCycle)
	_, err = usecases.RenameCategory{Deps: f.deps}.Execute(bg(), usecases.RenameCategoryInput{CategoryID: "cat-nope", Name: "X"})
	wantErr(t, err, category.ErrCategoryNotFound)
	_, err = usecases.RenameCategory{Deps: f.deps}.Execute(bg(), usecases.RenameCategoryInput{CategoryID: home, Name: ""})
	wantErr(t, err, shared.ErrValidation)
	eq(t, "counts", f.store.Counts(), counts)
}

func TestPublishingAnEmptyTree(t *testing.T) {
	f := newFx(t)
	empty, err := usecases.ListCategories{Deps: f.deps}.Execute(bg())
	must(t, err)
	eq(t, "empty version", empty.Version, int64(0))
	if empty.Categories == nil || len(empty.Categories) != 0 {
		t.Fatalf("categories = %#v, want an empty non-nil list", empty.Categories)
	}
	tree, err := usecases.PublishCategoryTree{Deps: f.deps}.Execute(bg())
	must(t, err)
	eq(t, "version", tree.Version, int64(1))
	eq(t, "event", len(f.events()), 1)
}

func TestListCategoriesReturnsTheLiveTree(t *testing.T) {
	f := newFx(t)
	f.category("Zeta")
	f.category("Alpha")
	tree, err := usecases.ListCategories{Deps: f.deps}.Execute(bg())
	must(t, err)
	eq(t, "version", tree.Version, int64(2))
	eq(t, "first", tree.Categories[0].Name, "Alpha")
	eq(t, "second", tree.Categories[1].Name, "Zeta")
}

func TestListProductContentsPages(t *testing.T) {
	f := newFx(t)
	for i := 1; i <= 5; i++ {
		f.create(fmt.Sprintf("item-%d", i))
	}
	uc := usecases.ListProductContents{Deps: f.deps}
	p1, err := uc.Execute(bg(), usecases.ListProductContentsInput{Limit: 2})
	must(t, err)
	eq(t, "page 1", len(p1.Items), 2)
	eq(t, "next 1", p1.Next, "item-2")
	p2, err := uc.Execute(bg(), usecases.ListProductContentsInput{After: p1.Next, Limit: 2})
	must(t, err)
	eq(t, "page 2 first", p2.Items[0].CatalogItemID, "item-3")
	eq(t, "next 2", p2.Next, "item-4")
	p3, err := uc.Execute(bg(), usecases.ListProductContentsInput{After: p2.Next, Limit: 2})
	must(t, err)
	eq(t, "page 3", len(p3.Items), 1)
	eq(t, "no next", p3.Next, "")
	exact, err := uc.Execute(bg(), usecases.ListProductContentsInput{After: "item-3", Limit: 2})
	must(t, err)
	eq(t, "exact page", len(exact.Items), 2)
	eq(t, "an exactly full last page has no next", exact.Next, "")

	def, err := uc.Execute(bg(), usecases.ListProductContentsInput{})
	must(t, err)
	eq(t, "default page", len(def.Items), 5)
	empty, err := uc.Execute(bg(), usecases.ListProductContentsInput{After: "zzz"})
	must(t, err)
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("items = %#v, want an empty non-nil list", empty.Items)
	}
}

func TestListProductContentsLimitBounds(t *testing.T) {
	f := newFx(t)
	uc := usecases.ListProductContents{Deps: f.deps}
	for _, limit := range []int{-1, usecases.MaxPageSize + 1} {
		_, err := uc.Execute(bg(), usecases.ListProductContentsInput{Limit: limit})
		wantErr(t, err, shared.ErrValidation)
	}
	for _, limit := range []int{1, usecases.MaxPageSize} {
		_, err := uc.Execute(bg(), usecases.ListProductContentsInput{Limit: limit})
		must(t, err)
	}
}

func TestGetProductContentMissIsNotFound(t *testing.T) {
	f := newFx(t)
	_, err := usecases.GetProductContent{Deps: f.deps}.Execute(bg(), "item-none")
	wantErr(t, err, ports.ErrNotFound)
}

func TestViewsNeverCarryNilArrays(t *testing.T) {
	f := newFx(t)
	f.item("item-1")
	v, err := usecases.CreateProductContent{Deps: f.deps}.Execute(bg(), usecases.ContentInput{CatalogItemID: "item-1"})
	must(t, err)
	if v.Attributes == nil || v.Media == nil {
		t.Fatalf("view = %#v", v)
	}
}

// --- consumer ---------------------------------------------------------------

func TestCatalogItemCreatedMakesTheItemKnown(t *testing.T) {
	f := newFx(t)
	c := usecases.Consumer{Deps: f.deps}
	must(t, c.ApplyCatalogItemCreated(bg(), usecases.Meta{EventID: "ev-1", Type: "t", At: at(5)}, usecases.CatalogItemCreated{CatalogItemID: "item-1"}))
	it, err := f.store.Items().Get(bg(), "item-1")
	must(t, err)
	eq(t, "state", it.State, readmodel.ItemActive)
	eq(t, "at", it.UpdatedAt, at(5))
	eq(t, "inbox", f.store.Counts().Inbox, 1)
}

func TestConsumerMeetsAMessageWithoutATimeWithTheClock(t *testing.T) {
	f := newFx(t)
	f.clock.Set(at(30))
	must(t, usecases.Consumer{Deps: f.deps}.ApplyCatalogItemCreated(bg(), usecases.Meta{EventID: "ev-1", Type: "t"}, usecases.CatalogItemCreated{CatalogItemID: "item-1"}))
	it, _ := f.store.Items().Get(bg(), "item-1")
	eq(t, "at", it.UpdatedAt, at(30))
}

func TestConsumerIsIdempotentOnTheEventID(t *testing.T) {
	f := newFx(t)
	f.publish("item-1")
	c := usecases.Consumer{Deps: f.deps}
	m := meta("ev-dup")
	must(t, c.ApplyCatalogItemDiscontinued(bg(), m, usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"}))
	events := len(f.events())
	counts := f.store.Counts()
	must(t, c.ApplyCatalogItemDiscontinued(bg(), m, usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"}))
	eq(t, "no new event", len(f.events()), events)
	eq(t, "counts", f.store.Counts(), counts)
}

func TestDiscontinuedRetiresPublishedContentWithAnEvent(t *testing.T) {
	f := newFx(t)
	f.publish("item-1")
	before := len(f.events())
	must(t, usecases.Consumer{Deps: f.deps}.ApplyCatalogItemDiscontinued(bg(), meta("ev-d"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"}))
	v := f.get("item-1")
	eq(t, "state", v.State, "RETIRED")
	eq(t, "reason", v.RetiredReason, "DISCONTINUED")
	evs := f.events()
	eq(t, "one event", len(evs), before+1)
	if r, ok := evs[before].(content.ProductContentRetired); !ok || r.CatalogItemID != "item-1" {
		t.Fatalf("event = %#v", evs[before])
	}
	it, _ := f.store.Items().Get(bg(), "item-1")
	eq(t, "view", it.State, readmodel.ItemDiscontinued)
}

func TestDiscontinuedRetiresADraftSilently(t *testing.T) {
	f := newFx(t)
	f.create("item-1")
	before := len(f.events())
	must(t, usecases.Consumer{Deps: f.deps}.ApplyCatalogItemDiscontinued(bg(), meta("ev-d"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"}))
	v := f.get("item-1")
	eq(t, "state", v.State, "RETIRED")
	eq(t, "reason", v.RetiredReason, "DISCONTINUED")
	eq(t, "no event", len(f.events()), before)
}

func TestDiscontinuedLeavesRetiredContentAndUnknownItemsAlone(t *testing.T) {
	f := newFx(t)
	f.create("item-1")
	_, err := usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	version := f.store.Counts().VersionSum
	c := usecases.Consumer{Deps: f.deps}
	must(t, c.ApplyCatalogItemDiscontinued(bg(), meta("ev-1"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"}))
	eq(t, "retired content untouched", f.store.Counts().VersionSum, version)
	eq(t, "still discarded", f.get("item-1").RetiredReason, "DISCARDED")

	// an item never seen: the event may overtake its Created
	must(t, c.ApplyCatalogItemDiscontinued(bg(), meta("ev-2"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-new"}))
	it, err := f.store.Items().Get(bg(), "item-new")
	must(t, err)
	eq(t, "state", it.State, readmodel.ItemDiscontinued)
	must(t, c.ApplyCatalogItemCreated(bg(), meta("ev-3"), usecases.CatalogItemCreated{CatalogItemID: "item-new"}))
	it, _ = f.store.Items().Get(bg(), "item-new")
	eq(t, "never revived", it.State, readmodel.ItemDiscontinued)
}

func TestConsumerRejectsPoisonIDs(t *testing.T) {
	f := newFx(t)
	c := usecases.Consumer{Deps: f.deps}
	for _, id := range []string{"", "-x", "has space"} {
		wantErr(t, c.ApplyCatalogItemCreated(bg(), meta("ev"), usecases.CatalogItemCreated{CatalogItemID: id}), shared.ErrValidation)
		wantErr(t, c.ApplyCatalogItemDiscontinued(bg(), meta("ev"), usecases.CatalogItemDiscontinued{CatalogItemID: id}), shared.ErrValidation)
	}
	eq(t, "nothing claimed", f.store.Counts().Inbox, 0)
}

// --- infrastructure failures -----------------------------------------------

type failingOutbox struct {
	ports.EventOutbox
	err error
}

func (o *failingOutbox) Append(context.Context, ...shared.Event) error { return o.err }

// A failing outbox append fails the command and rolls the save back with it
// (transactional outbox).
func TestAFailingOutboxAppendRollsTheCommandBack(t *testing.T) {
	f := newFx(t)
	f.create("item-1")
	counts := f.store.Counts()
	injected := errors.New("outbox insert: disk full")
	f.deps.Outbox = &failingOutbox{EventOutbox: f.deps.Outbox, err: injected}
	_, err := usecases.PublishContent{Deps: f.deps}.Execute(bg(), "item-1")
	wantErr(t, err, injected)
	eq(t, "state", f.get("item-1").State, "DRAFT")
	eq(t, "counts", f.store.Counts(), counts)

	_, err = usecases.CreateCategory{Deps: f.deps}.Execute(bg(), usecases.CreateCategoryInput{Name: "X"})
	wantErr(t, err, injected)
	eq(t, "counts after category", f.store.Counts(), counts)

	err = usecases.Consumer{Deps: f.deps}.ApplyCatalogItemDiscontinued(bg(), meta("ev-x"), usecases.CatalogItemDiscontinued{CatalogItemID: "item-1"})
	wantErr(t, err, injected)
	eq(t, "inbox rolled back", f.store.Counts().Inbox, counts.Inbox)
}

// racingContents makes the first saves lose to a rival writer.
type racingContents struct {
	ports.ContentRepository
	races int
}

func (r *racingContents) Save(ctx context.Context, c *content.ProductContent) error {
	if r.races > 0 {
		r.races--
		return fmt.Errorf("rival got in first: %w", ports.ErrConcurrentUpdate)
	}
	return r.ContentRepository.Save(ctx, c)
}

func TestAConcurrentUpdateIsRetriedOnceThenReported(t *testing.T) {
	f := newFx(t)
	f.create("item-1")
	f.create("item-2")
	rc := &racingContents{ContentRepository: f.deps.Contents, races: 1}
	f.deps.Contents = rc
	v, err := usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-1")
	must(t, err)
	eq(t, "retried and won", v.State, "RETIRED")

	rc.races = 2
	_, err = usecases.DiscardDraft{Deps: f.deps}.Execute(bg(), "item-2")
	wantErr(t, err, ports.ErrConcurrentUpdate)
	eq(t, "still a draft", f.get("item-2").State, "DRAFT")
}
