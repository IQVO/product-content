package repocontract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/application/idempotency"
	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

func testInbox(t *testing.T, e Env) {
	first, err := e.Inbox.Claim(bg(), "ev-1", "type.A")
	must(t, err)
	again, err := e.Inbox.Claim(bg(), "ev-1", "type.A")
	must(t, err)
	other, err := e.Inbox.Claim(bg(), "ev-2", "type.A")
	must(t, err)
	if !first || again || !other {
		t.Fatalf("claims = %v %v %v, want true false true", first, again, other)
	}
}

var errBoom = errors.New("boom")

func testUnitOfWork(t *testing.T, e Env) {
	t.Run("a unit that fails rolls back every table", func(t *testing.T) {
		err := e.UoW.Do(bg(), func(ctx context.Context) error {
			must(t, e.Contents.Save(ctx, newContent(t, "item-rb")))
			tr := category.NewTree()
			_, _, cerr := tr.CreateCategory("rb", "", "Rollback", at(1))
			must(t, cerr)
			must(t, e.Categories.Save(ctx, tr))
			must(t, e.Items.MarkActive(ctx, "item-rb", at(1)))
			first, err := e.Inbox.Claim(ctx, "ev-rb", "type.A")
			must(t, err)
			if !first {
				t.Fatal("the claim inside the unit must be the first")
			}
			must(t, e.Outbox.Append(ctx, mustEvents(t)...))
			return errBoom
		})
		wantErr(t, err, errBoom)
		_, err = e.Contents.Get(bg(), "item-rb")
		wantErr(t, err, ports.ErrNotFound)
		_, err = e.Categories.Get(bg())
		wantErr(t, err, ports.ErrNotFound)
		_, err = e.Items.Get(bg(), "item-rb")
		wantErr(t, err, ports.ErrNotFound)
		if first, _ := e.Inbox.Claim(bg(), "ev-rb", "type.A"); !first {
			t.Fatal("the rolled back claim must be free again")
		}
		if n := e.OutboxCount(t); n != 0 {
			t.Fatalf("outbox holds %d events after a rollback", n)
		}
	})
	t.Run("a unit that succeeds commits everything", func(t *testing.T) {
		must(t, e.UoW.Do(bg(), func(ctx context.Context) error {
			must(t, e.Contents.Save(ctx, newContent(t, "item-ok")))
			must(t, e.Outbox.Append(ctx, mustEvents(t)...))
			return nil
		}))
		if _, err := e.Contents.Get(bg(), "item-ok"); err != nil {
			t.Fatalf("committed content missing: %v", err)
		}
		if n := e.OutboxCount(t); n != 2 {
			t.Fatalf("outbox holds %d events, want 2", n)
		}
	})
	t.Run("a nested unit joins the outer one", func(t *testing.T) {
		err := e.UoW.Do(bg(), func(ctx context.Context) error {
			must(t, e.UoW.Do(ctx, func(ctx context.Context) error { return e.Contents.Save(ctx, newContent(t, "item-nest")) }))
			return errBoom
		})
		wantErr(t, err, errBoom)
		_, err = e.Contents.Get(bg(), "item-nest")
		wantErr(t, err, ports.ErrNotFound)
	})
}

// mustEvents returns one event of each published aggregate.
func mustEvents(t *testing.T) []shared.Event {
	t.Helper()
	c := newContent(t, "item-ev")
	evs, err := c.Publish(content.ItemActive, knownCategories, t0)
	must(t, err)
	tr := category.NewTree()
	_, tevs, err := tr.CreateCategory("ev", "", "Events", t0)
	must(t, err)
	return append(evs, tevs...)
}

func testIdempotency(t *testing.T, e Env) {
	_, err := e.Idempotency.Get(bg(), "POST /product-contents", "k1")
	wantErr(t, err, ports.ErrNotFound)
	rec := idempotency.Record{RequestHash: "h1", Status: 201, ContentType: "application/json", Body: []byte(`{"a":1}`), CreatedAt: e.Clock.Now().UTC()}
	stored, err := e.Idempotency.Put(bg(), "POST /product-contents", "k1", rec)
	must(t, err)
	if !stored {
		t.Fatal("the first Put must store")
	}
	stored, err = e.Idempotency.Put(bg(), "POST /product-contents", "k1", idempotency.Record{RequestHash: "h2", Status: 500, CreatedAt: e.Clock.Now().UTC()})
	must(t, err)
	if stored {
		t.Fatal("a second Put must not overwrite")
	}
	got, err := e.Idempotency.Get(bg(), "POST /product-contents", "k1")
	must(t, err)
	if got.RequestHash != "h1" || got.Status != 201 || got.ContentType != "application/json" || string(got.Body) != `{"a":1}` {
		t.Fatalf("stored record = %+v", got)
	}
	if _, err := e.Idempotency.Get(bg(), "POST /categories", "k1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("the route is part of the key: %v", err)
	}
	old := idempotency.Record{RequestHash: "old", Status: 200, CreatedAt: e.Clock.Now().UTC().Add(-idempotency.TTL - time.Minute)}
	if _, err := e.Idempotency.Put(bg(), "POST /categories", "k-old", old); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Idempotency.Get(bg(), "POST /categories", "k-old"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("an expired record must read as absent: %v", err)
	}
	replaced, err := e.Idempotency.Put(bg(), "POST /categories", "k-old", idempotency.Record{RequestHash: "new", Status: 201, CreatedAt: e.Clock.Now().UTC()})
	must(t, err)
	if !replaced {
		t.Fatal("an expired record is replaceable")
	}
}
