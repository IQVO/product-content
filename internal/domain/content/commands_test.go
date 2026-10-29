package content

import (
	"reflect"
	"testing"

	"github.com/IQVO/product-content/internal/domain/shared"
)

func TestReviseDraftTakesANewRevision(t *testing.T) {
	c := mustCreate(t, emptyDoc)
	nb := completeBody()
	if err := c.ReviseDraft(nb, tree, t1); err != nil {
		t.Fatalf("ReviseDraft: %v", err)
	}
	if c.State() != StateDraft || c.Revision() != 2 || c.Version() != 2 {
		t.Fatalf("state %v rev %d version %d, want DRAFT 2 2", c.State(), c.Revision(), c.Version())
	}
	if !reflect.DeepEqual(c.Body(), nb) || !c.UpdatedAt().Equal(t1) || !c.CreatedAt().Equal(t0) {
		t.Fatalf("body %+v updatedAt %v createdAt %v", c.Body(), c.UpdatedAt(), c.CreatedAt())
	}
}

func TestReviseDraftRefusals(t *testing.T) {
	published := mustPublished(t)
	retired := mustCreate(t, emptyDoc)
	if err := retired.DiscardDraft(t1); err != nil {
		t.Fatal(err)
	}
	draft := mustCreate(t, emptyDoc)
	tests := []struct {
		name string
		c    *ProductContent
		body Body
		want error
	}{
		{"published", published, completeBody(), shared.ErrIllegalTransition},
		{"retired", retired, completeBody(), ErrContentRetired},
		{"unknown category", draft, Body{PrimaryCategoryID: "cat-nope"}, ErrUnknownCategory},
		{"too many bullets", draft, Body{Bullets: make([]string, MaxBullets+1)}, ErrTooManyBullets},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rev, ver := tt.c.Revision(), tt.c.Version()
			wantErr(t, tt.c.ReviseDraft(tt.body, tree, t2), tt.want)
			if tt.c.Revision() != rev || tt.c.Version() != ver {
				t.Fatal("a refused revise changed the aggregate")
			}
		})
	}
}

func TestReviseContentKeepsPublishedAndTakesANewRevision(t *testing.T) {
	c := mustPublished(t)
	nb := completeBody()
	nb.Description = "Now with a stronger cap."
	if err := c.ReviseContent(nb, tree, t2); err != nil {
		t.Fatalf("ReviseContent: %v", err)
	}
	if c.State() != StatePublished || c.Revision() != 2 || c.PublishedRevision() != 1 || c.Version() != 3 {
		t.Fatalf("state %v rev %d published %d version %d", c.State(), c.Revision(), c.PublishedRevision(), c.Version())
	}
	if c.Body().Description != nb.Description || !c.UpdatedAt().Equal(t2) || !c.PublishedAt().Equal(t1) {
		t.Fatalf("body %q updatedAt %v publishedAt %v", c.Body().Description, c.UpdatedAt(), c.PublishedAt())
	}
}

func TestReviseContentRefusals(t *testing.T) {
	draft := mustCreate(t, completeBody())
	retired := mustPublished(t)
	if _, err := retired.Retire(t2); err != nil {
		t.Fatal(err)
	}
	incomplete := func(f func(*Body)) Body { b := completeBody(); f(&b); return b }
	pub := mustPublished(t)
	tests := []struct {
		name string
		c    *ProductContent
		body Body
		want error
	}{
		{"draft", draft, completeBody(), shared.ErrIllegalTransition},
		{"retired", retired, completeBody(), ErrContentRetired},
		{"no description", pub, incomplete(func(b *Body) { b.Description = "" }), ErrIncompleteContent},
		{"no category", pub, incomplete(func(b *Body) { b.PrimaryCategoryID = "" }), ErrIncompleteContent},
		{"no primary medium", pub, incomplete(func(b *Body) { b.Media = b.Media[1:] }), ErrIncompleteContent},
		{"two primary media", pub, incomplete(func(b *Body) { b.Media[1].Role = RolePrimary }), ErrIncompleteContent},
		{"unknown category", pub, incomplete(func(b *Body) { b.PrimaryCategoryID = "cat-nope" }), ErrUnknownCategory},
		{"invalid field", pub, incomplete(func(b *Body) { b.Bullets = []string{""} }), shared.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rev, ver := tt.c.Revision(), tt.c.Version()
			wantErr(t, tt.c.ReviseContent(tt.body, tree, t2), tt.want)
			if tt.c.Revision() != rev || tt.c.Version() != ver {
				t.Fatal("a refused revise changed the aggregate")
			}
		})
	}
}

func TestPublishADraft(t *testing.T) {
	c := mustCreate(t, completeBody())
	events, err := c.Publish(ItemActive, tree, t1)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	eq(t, "state", c.State(), StatePublished)
	eq(t, "publishedRevision", c.PublishedRevision(), 1)
	eq(t, "revision", c.Revision(), 1)
	eq(t, "version", c.Version(), int64(2))
	eq(t, "publishedAt", c.PublishedAt(), t1)
	eq(t, "updatedAt", c.UpdatedAt(), t1)
	if len(events) != 1 {
		t.Fatalf("events = %v, want one", events)
	}
	e, ok := events[0].(ProductContentPublished)
	if !ok {
		t.Fatalf("event = %T", events[0])
	}
	want := completeBody()
	eq(t, "event item", e.CatalogItemID, ID("item-1"))
	eq(t, "event revision", e.Revision, 1)
	eq(t, "event description", e.Description, want.Description)
	eq(t, "event category", e.PrimaryCategoryID, "cat-bottles")
	if !reflect.DeepEqual(e.Bullets, want.Bullets) || !reflect.DeepEqual(e.Attributes, want.Attributes) || !reflect.DeepEqual(e.Media, want.Media) {
		t.Fatalf("event = %+v", e)
	}
	eq(t, "event version", e.AggregateVersion(), int64(2))
	eq(t, "event time", e.OccurredAt(), t1)
	eq(t, "event name", e.EventName(), "ProductContentPublished")
	eq(t, "event entity", e.Entity(), "productcontent")
	eq(t, "event subject", e.SubjectID(), "item-1")
}

func TestPublishEventDoesNotShareMemory(t *testing.T) {
	c := mustCreate(t, completeBody())
	events, _ := c.Publish(ItemActive, tree, t1)
	e := events[0].(ProductContentPublished)
	e.Bullets[0], e.Media[0].AltText, e.Attributes[0].Name = "x", "x", "x"
	b := c.Body()
	if b.Bullets[0] != "Double-wall" || b.Media[0].AltText != "side" || b.Attributes[0].Name != "capacity" {
		t.Fatalf("the event aliases the aggregate: %+v", b)
	}
}

func TestPublishARevisionOfPublishedContent(t *testing.T) {
	c := mustPublished(t)
	nb := completeBody()
	nb.Bullets = []string{"New bullet"}
	if err := c.ReviseContent(nb, tree, t1); err != nil {
		t.Fatal(err)
	}
	events, err := c.Publish(ItemActive, tree, t2)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(events) != 1 || events[0].(ProductContentPublished).Revision != 2 {
		t.Fatalf("events = %v, want one at revision 2", events)
	}
	if c.PublishedRevision() != 2 || c.Revision() != 2 || c.Version() != 4 || !c.PublishedAt().Equal(t2) {
		t.Fatalf("published %d rev %d version %d at %v", c.PublishedRevision(), c.Revision(), c.Version(), c.PublishedAt())
	}
}

func TestPublishWithNothingNewIsANoOp(t *testing.T) {
	c := mustPublished(t)
	events, err := c.Publish(ItemActive, tree, t2)
	if err != nil || len(events) != 0 {
		t.Fatalf("Publish = %v, %v; want nothing", events, err)
	}
	if c.Version() != 2 || !c.UpdatedAt().Equal(t1) || !c.PublishedAt().Equal(t1) || c.PublishedRevision() != 1 {
		t.Fatalf("a no-op publish changed the aggregate: version %d updated %v published %v", c.Version(), c.UpdatedAt(), c.PublishedAt())
	}
}

func TestPublishRefusals(t *testing.T) {
	retired := mustCreate(t, emptyDoc)
	if err := retired.DiscardDraft(t1); err != nil {
		t.Fatal(err)
	}
	mk := func(f func(*Body)) *ProductContent { b := completeBody(); f(&b); return mustCreate(t, b) }
	tests := []struct {
		name string
		c    *ProductContent
		item ItemStatus
		cats Categories
		want error
	}{
		{"retired", retired, ItemActive, tree, ErrContentRetired},
		{"retired and discontinued", retired, ItemDiscontinued, tree, ErrContentRetired},
		{"discontinued", mk(func(*Body) {}), ItemDiscontinued, tree, ErrItemDiscontinued},
		{"unknown item", mk(func(*Body) {}), ItemUnknown, tree, ErrUnknownCatalogItem},
		{"no description", mk(func(b *Body) { b.Description = "" }), ItemActive, tree, ErrIncompleteContent},
		{"no category", mk(func(b *Body) { b.PrimaryCategoryID = "" }), ItemActive, tree, ErrIncompleteContent},
		{"no media", mk(func(b *Body) { b.Media = nil }), ItemActive, tree, ErrIncompleteContent},
		{"gallery only", mk(func(b *Body) { b.Media = b.Media[1:] }), ItemActive, tree, ErrIncompleteContent},
		{"two primary", mk(func(b *Body) { b.Media[1].Role = RolePrimary }), ItemActive, tree, ErrIncompleteContent},
		{"category left the tree", mk(func(*Body) {}), ItemActive, cats{}, ErrUnknownCategory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, st := tt.c.Version(), tt.c.State()
			events, err := tt.c.Publish(tt.item, tt.cats, t2)
			wantErr(t, err, tt.want)
			if len(events) != 0 || tt.c.Version() != ver || tt.c.State() != st {
				t.Fatal("a refused publish changed the aggregate or raised events")
			}
		})
	}
}

func TestRetirePublishedContent(t *testing.T) {
	c := mustPublished(t)
	nb := completeBody()
	nb.Description = "Edited"
	if err := c.ReviseContent(nb, tree, t1); err != nil {
		t.Fatal(err)
	}
	events, err := c.Retire(t2)
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	eq(t, "state", c.State(), StateRetired)
	eq(t, "reason", c.RetiredReason(), ReasonManual)
	eq(t, "retiredAt", c.RetiredAt(), t2)
	eq(t, "updatedAt", c.UpdatedAt(), t2)
	eq(t, "version", c.Version(), int64(4))
	if len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
	e, ok := events[0].(ProductContentRetired)
	if !ok {
		t.Fatalf("event = %T", events[0])
	}
	eq(t, "event item", e.CatalogItemID, ID("item-1"))
	eq(t, "event revision", e.Revision, 2)
	eq(t, "event version", e.AggregateVersion(), int64(4))
	eq(t, "event time", e.OccurredAt(), t2)
	eq(t, "event name", e.EventName(), "ProductContentRetired")
	eq(t, "event entity", e.Entity(), "productcontent")
	eq(t, "event subject", e.SubjectID(), "item-1")
}

func TestRetireRefusals(t *testing.T) {
	retired := mustPublished(t)
	if _, err := retired.Retire(t2); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*ProductContent{"draft": mustCreate(t, emptyDoc), "retired": retired} {
		t.Run(name, func(t *testing.T) {
			ver := c.Version()
			events, err := c.Retire(t2)
			wantErr(t, err, shared.ErrIllegalTransition)
			if len(events) != 0 || c.Version() != ver {
				t.Fatal("a refused retire changed the aggregate")
			}
		})
	}
}

func TestDiscardDraft(t *testing.T) {
	c := mustCreate(t, emptyDoc)
	if err := c.DiscardDraft(t1); err != nil {
		t.Fatalf("DiscardDraft: %v", err)
	}
	if c.State() != StateRetired || c.RetiredReason() != ReasonDiscarded || !c.RetiredAt().Equal(t1) || !c.UpdatedAt().Equal(t1) || c.Version() != 2 || c.PublishedRevision() != 0 {
		t.Fatalf("state %v reason %q retiredAt %v version %d", c.State(), c.RetiredReason(), c.RetiredAt(), c.Version())
	}
}

func TestDiscardDraftRefusals(t *testing.T) {
	discarded := mustCreate(t, emptyDoc)
	_ = discarded.DiscardDraft(t1)
	for name, c := range map[string]*ProductContent{"published": mustPublished(t), "retired": discarded} {
		t.Run(name, func(t *testing.T) {
			ver := c.Version()
			wantErr(t, c.DiscardDraft(t2), shared.ErrIllegalTransition)
			if c.Version() != ver {
				t.Fatal("a refused discard changed the aggregate")
			}
		})
	}
}

func TestRetireOnDiscontinue(t *testing.T) {
	t.Run("published content retires with an event", func(t *testing.T) {
		c := mustPublished(t)
		events := c.RetireOnDiscontinue(t2)
		if c.State() != StateRetired || c.RetiredReason() != ReasonDiscontinued || !c.RetiredAt().Equal(t2) || c.Version() != 3 {
			t.Fatalf("state %v reason %q version %d", c.State(), c.RetiredReason(), c.Version())
		}
		if len(events) != 1 || events[0].(ProductContentRetired).Revision != 1 {
			t.Fatalf("events = %v", events)
		}
	})
	t.Run("a draft retires silently", func(t *testing.T) {
		c := mustCreate(t, completeBody())
		events := c.RetireOnDiscontinue(t2)
		if c.State() != StateRetired || c.RetiredReason() != ReasonDiscontinued || len(events) != 0 || c.Version() != 2 {
			t.Fatalf("state %v reason %q events %v version %d", c.State(), c.RetiredReason(), events, c.Version())
		}
	})
	t.Run("retired content is left alone", func(t *testing.T) {
		c := mustCreate(t, emptyDoc)
		_ = c.DiscardDraft(t1)
		events := c.RetireOnDiscontinue(t2)
		if len(events) != 0 || c.RetiredReason() != ReasonDiscarded || c.Version() != 2 || !c.RetiredAt().Equal(t1) {
			t.Fatalf("a retired content changed: %v %q %d", events, c.RetiredReason(), c.Version())
		}
	})
}

func TestEnumsValid(t *testing.T) {
	for _, s := range []State{StateDraft, StatePublished, StateRetired} {
		if !s.Valid() {
			t.Fatalf("%s invalid", s)
		}
	}
	for _, r := range []RetiredReason{ReasonManual, ReasonDiscarded, ReasonDiscontinued} {
		if !r.Valid() {
			t.Fatalf("%s invalid", r)
		}
	}
	for _, r := range []Role{RolePrimary, RoleGallery} {
		if !r.Valid() {
			t.Fatalf("%s invalid", r)
		}
	}
	if State("X").Valid() || RetiredReason("").Valid() || Role("").Valid() {
		t.Fatal("unknown values are invalid")
	}
}
