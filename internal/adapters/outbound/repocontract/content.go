package repocontract

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/readmodel"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
)

func testContents(t *testing.T, e Env) {
	t.Run("a miss is ErrNotFound", func(t *testing.T) {
		_, err := e.Contents.Get(bg(), "item-nope")
		wantErr(t, err, ports.ErrNotFound)
	})
	t.Run("a saved content reads back identically", func(t *testing.T) {
		c := newContent(t, "item-rt")
		must(t, e.Contents.Save(bg(), c))
		if c.LoadedVersion() != 1 {
			t.Fatalf("Save must MarkPersisted: loaded = %d", c.LoadedVersion())
		}
		got, err := e.Contents.Get(bg(), "item-rt")
		must(t, err)
		if !reflect.DeepEqual(got.Snapshot(), c.Snapshot()) {
			t.Fatalf("round trip changed the content:\n got %+v\nwant %+v", got.Snapshot(), c.Snapshot())
		}
	})
	t.Run("every state round-trips with its facts", func(t *testing.T) { contentsRoundTripEveryState(t, e) })
	t.Run("an incomplete draft and empty collections round-trip", func(t *testing.T) {
		c, err := content.Create("item-empty", content.Body{}, content.ItemActive, knownCategories, t0)
		must(t, err)
		must(t, e.Contents.Save(bg(), c))
		got, err := e.Contents.Get(bg(), "item-empty")
		must(t, err)
		if !reflect.DeepEqual(got.Snapshot(), c.Snapshot()) {
			t.Fatalf("round trip changed the content:\n got %+v\nwant %+v", got.Snapshot(), c.Snapshot())
		}
	})
	t.Run("optimistic concurrency", func(t *testing.T) {
		c := newContent(t, "item-cc")
		must(t, e.Contents.Save(bg(), c))
		dup := newContent(t, "item-cc")
		wantErr(t, e.Contents.Save(bg(), dup), ports.ErrConcurrentUpdate) // a create expects the id to be free
		a, err := e.Contents.Get(bg(), "item-cc")
		must(t, err)
		b, err := e.Contents.Get(bg(), "item-cc")
		must(t, err)
		must(t, a.ReviseDraft(fullBody(), knownCategories, at(1)))
		must(t, e.Contents.Save(bg(), a))
		must(t, b.ReviseDraft(fullBody(), knownCategories, at(2)))
		wantErr(t, e.Contents.Save(bg(), b), ports.ErrConcurrentUpdate) // b loaded a version that is gone
		must(t, a.ReviseDraft(fullBody(), knownCategories, at(3)))
		must(t, e.Contents.Save(bg(), a))                                          // a refreshed LoadedVersion with MarkPersisted
		wantErr(t, e.Contents.Save(bg(), orphan(t, c)), ports.ErrConcurrentUpdate) // loaded at a version that was never stored
	})
	t.Run("List pages by catalogItemId after the cursor", func(t *testing.T) {
		for i := 1; i <= 5; i++ {
			must(t, e.Contents.Save(bg(), newContent(t, fmt.Sprintf("z-list-%d", i))))
		}
		page, err := e.Contents.List(bg(), "z-", 2)
		must(t, err)
		if ids := idsOf(page); !reflect.DeepEqual(ids, []string{"z-list-1", "z-list-2"}) {
			t.Fatalf("first page = %v", ids)
		}
		page, err = e.Contents.List(bg(), "z-list-2", 2)
		must(t, err)
		if ids := idsOf(page); !reflect.DeepEqual(ids, []string{"z-list-3", "z-list-4"}) {
			t.Fatalf("second page = %v", ids)
		}
		page, err = e.Contents.List(bg(), "z-list-4", 10)
		must(t, err)
		if ids := idsOf(page); !reflect.DeepEqual(ids, []string{"z-list-5"}) {
			t.Fatalf("last page = %v", ids)
		}
		page, err = e.Contents.List(bg(), "z-list-5", 10)
		must(t, err)
		if len(page) != 0 {
			t.Fatalf("past the end = %v", idsOf(page))
		}
	})
}

func idsOf(cs []*content.ProductContent) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(c.ID())
	}
	return out
}

// orphan returns an aggregate that claims a stored version that does not exist.
func orphan(t *testing.T, c *content.ProductContent) *content.ProductContent {
	t.Helper()
	s := c.Snapshot()
	s.ID = "item-never-stored"
	s.Version = 5
	o, err := content.Restore(s)
	must(t, err)
	must(t, o.ReviseDraft(fullBody(), knownCategories, at(9)))
	return o
}

func contentsRoundTripEveryState(t *testing.T, e Env) {
	published := newContent(t, "item-pub")
	_, err := published.Publish(content.ItemActive, knownCategories, at(1))
	must(t, err)
	revised := newContent(t, "item-rev")
	_, err = revised.Publish(content.ItemActive, knownCategories, at(1))
	must(t, err)
	nb := fullBody()
	nb.Description = "Edited after publication."
	must(t, revised.ReviseContent(nb, knownCategories, at(2)))
	retired := newContent(t, "item-ret")
	_, err = retired.Publish(content.ItemActive, knownCategories, at(1))
	must(t, err)
	_, err = retired.Retire(at(3))
	must(t, err)
	discarded := newContent(t, "item-dis")
	must(t, discarded.DiscardDraft(at(1)))
	discontinued := newContent(t, "item-disc")
	discontinued.RetireOnDiscontinue(at(1))
	for _, c := range []*content.ProductContent{published, revised, retired, discarded, discontinued} {
		must(t, e.Contents.Save(bg(), c))
		got, err := e.Contents.Get(bg(), c.ID())
		must(t, err)
		if !reflect.DeepEqual(got.Snapshot(), c.Snapshot()) {
			t.Fatalf("%s round trip changed the content:\n got %+v\nwant %+v", c.ID(), got.Snapshot(), c.Snapshot())
		}
	}
}

func testTree(t *testing.T, e Env) {
	_, err := e.Categories.Get(bg())
	wantErr(t, err, ports.ErrNotFound) // a never stored tree

	tr := category.NewTree()
	for _, c := range [][3]string{{"home", "", "Home"}, {"kitchen", "home", "Kitchen"}, {"bottles", "kitchen", "Bottles"}, {"outdoor", "", "Outdoor"}} {
		_, _, err := tr.CreateCategory(c[0], c[1], c[2], at(1))
		must(t, err)
	}
	rival := category.NewTree() // also loaded as "never stored"
	_, _, err = rival.CreateCategory("garden", "", "Garden", at(1))
	must(t, err)

	// The first save of a tree that already ran several commands: version 4, loaded 0.
	must(t, e.Categories.Save(bg(), tr))
	if tr.LoadedVersion() != tr.Version() {
		t.Fatalf("Save must MarkPersisted: loaded %d version %d", tr.LoadedVersion(), tr.Version())
	}
	wantErr(t, e.Categories.Save(bg(), rival), ports.ErrConcurrentUpdate) // a create expects no stored tree
	got, err := e.Categories.Get(bg())
	must(t, err)
	if !reflect.DeepEqual(got.Snapshot(), tr.Snapshot()) {
		t.Fatalf("round trip changed the tree:\n got %+v\nwant %+v", got.Snapshot(), tr.Snapshot())
	}

	a, err := e.Categories.Get(bg())
	must(t, err)
	b, err := e.Categories.Get(bg())
	must(t, err)
	_, _, err = a.CreateCategory("a", "", "A", at(2))
	must(t, err)
	must(t, e.Categories.Save(bg(), a))
	_, _, err = b.CreateCategory("b", "", "B", at(3))
	must(t, err)
	wantErr(t, e.Categories.Save(bg(), b), ports.ErrConcurrentUpdate)
	got, err = e.Categories.Get(bg())
	must(t, err)
	if got.Version() != 5 || !got.Has("a") || got.Has("b") {
		t.Fatalf("the losing save changed the tree: version %d", got.Version())
	}
}

func testItems(t *testing.T, e Env) {
	_, err := e.Items.Get(bg(), "item-x")
	wantErr(t, err, ports.ErrNotFound)

	must(t, e.Items.MarkActive(bg(), "item-x", at(1)))
	it, err := e.Items.Get(bg(), "item-x")
	must(t, err)
	if it.CatalogItemID != "item-x" || it.State != readmodel.ItemActive || !it.UpdatedAt.Equal(at(1)) {
		t.Fatalf("item = %+v", it)
	}
	must(t, e.Items.MarkActive(bg(), "item-x", at(2))) // a repeat changes nothing
	it, _ = e.Items.Get(bg(), "item-x")
	if !it.UpdatedAt.Equal(at(1)) {
		t.Fatalf("MarkActive on a known item must not touch it: %+v", it)
	}

	must(t, e.Items.MarkDiscontinued(bg(), "item-x", at(3)))
	it, _ = e.Items.Get(bg(), "item-x")
	if it.State != readmodel.ItemDiscontinued || !it.UpdatedAt.Equal(at(3)) {
		t.Fatalf("item = %+v", it)
	}
	must(t, e.Items.MarkActive(bg(), "item-x", at(4))) // a late Created never revives
	must(t, e.Items.MarkDiscontinued(bg(), "item-x", at(5)))
	it, _ = e.Items.Get(bg(), "item-x")
	if it.State != readmodel.ItemDiscontinued || !it.UpdatedAt.Equal(at(3)) {
		t.Fatalf("DISCONTINUED is terminal and keeps its instant: %+v", it)
	}

	must(t, e.Items.MarkDiscontinued(bg(), "item-y", at(6))) // Discontinued overtook Created
	it, err = e.Items.Get(bg(), "item-y")
	must(t, err)
	if it.State != readmodel.ItemDiscontinued {
		t.Fatalf("item = %+v", it)
	}
	must(t, e.Items.MarkActive(bg(), "item-y", at(7)))
	it, _ = e.Items.Get(bg(), "item-y")
	if it.State != readmodel.ItemDiscontinued {
		t.Fatalf("a Created after a Discontinued must not revive: %+v", it)
	}
}
