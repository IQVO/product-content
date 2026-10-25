package content

import (
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/domain/shared"
)

func TestCreateStartsADraftAtRevisionOne(t *testing.T) {
	c, err := Create("item-1", completeBody(), ItemActive, tree, t0.Add(789*time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if c.ID() != "item-1" || c.State() != StateDraft || c.Revision() != 1 || c.PublishedRevision() != 0 {
		t.Fatalf("got %v %v rev %d pub %d", c.ID(), c.State(), c.Revision(), c.PublishedRevision())
	}
	if c.Version() != 1 || c.LoadedVersion() != 0 {
		t.Fatalf("version %d loaded %d, want 1 and 0", c.Version(), c.LoadedVersion())
	}
	if !c.CreatedAt().Equal(t0) || !c.UpdatedAt().Equal(c.CreatedAt()) || c.CreatedAt().Nanosecond() != 123456000 {
		t.Fatalf("createdAt %v updatedAt %v: want %v truncated to the microsecond", c.CreatedAt(), c.UpdatedAt(), t0)
	}
	if !c.PublishedAt().IsZero() || !c.RetiredAt().IsZero() || c.RetiredReason() != "" {
		t.Fatal("a new draft is neither published nor retired")
	}
	c.MarkPersisted()
	if c.LoadedVersion() != 1 {
		t.Fatalf("MarkPersisted: loaded = %d, want 1", c.LoadedVersion())
	}
}

func TestCreateAcceptsAnIncompleteDraft(t *testing.T) {
	c, err := Create("item-1", emptyDoc, ItemActive, tree, t0)
	if err != nil {
		t.Fatalf("Create(empty body): %v", err)
	}
	if got := c.Body(); got.Description != "" || len(got.Media) != 0 {
		t.Fatalf("body = %+v", got)
	}
}

func TestCreateCopiesTheBody(t *testing.T) {
	b := completeBody()
	c := mustCreate(t, b)
	b.Bullets[0] = "changed"
	b.Media[0].AltText = "changed"
	got := c.Body()
	got.Bullets[1] = "changed"
	if c.Body().Bullets[0] != "Double-wall" || c.Body().Bullets[1] != "Dishwasher-safe cap" || c.Body().Media[0].AltText != "side" {
		t.Fatalf("the aggregate shares memory with its callers: %+v", c.Body())
	}
}

func TestCreateRefusals(t *testing.T) {
	tests := []struct {
		name string
		id   ID
		item ItemStatus
		body Body
		want error
	}{
		{"invalid id", "-bad", ItemActive, completeBody(), shared.ErrValidation},
		{"empty id", "", ItemActive, completeBody(), shared.ErrValidation},
		{"unknown item", "item-1", ItemUnknown, completeBody(), ErrUnknownCatalogItem},
		{"discontinued item", "item-1", ItemDiscontinued, completeBody(), ErrItemDiscontinued},
		{"unknown category", "item-1", ItemActive, Body{PrimaryCategoryID: "cat-nope"}, ErrUnknownCategory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Create(tt.id, tt.body, tt.item, tree, t0)
			wantErr(t, err, tt.want)
			if c != nil {
				t.Fatal("a refused Create returns no content")
			}
		})
	}
}

func TestBoundsAreInclusive(t *testing.T) {
	bullets := func(n int) Body {
		b := Body{}
		for i := 0; i < n; i++ {
			b.Bullets = append(b.Bullets, "b")
		}
		return b
	}
	attrs := func(n int) Body {
		b := Body{}
		for i := 0; i < n; i++ {
			b.Attributes = append(b.Attributes, Attribute{Name: "n", Value: "v"})
		}
		return b
	}
	media := func(n int) Body {
		b := Body{}
		for i := 0; i < n; i++ {
			b.Media = append(b.Media, Media{MediaID: "m" + string(rune('a'+i)), URL: "https://static.example.invalid/x", AltText: "a", Role: RoleGallery})
		}
		return b
	}
	tests := []struct {
		name string
		body Body
		want error
	}{
		{"10 bullets", bullets(MaxBullets), nil},
		{"11 bullets", bullets(MaxBullets + 1), ErrTooManyBullets},
		{"50 attributes", attrs(MaxAttributes), nil},
		{"51 attributes", attrs(MaxAttributes + 1), ErrTooManyAttributes},
		{"12 media", media(MaxMedia), nil},
		{"13 media", media(MaxMedia + 1), ErrTooManyMedia},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Create("item-1", tt.body, ItemActive, tree, t0)
			if tt.want == nil && err != nil {
				t.Fatalf("Create: %v", err)
			}
			if tt.want != nil {
				wantErr(t, err, tt.want)
			}
		})
	}
}

func TestFieldLimitsAreInclusive(t *testing.T) {
	with := func(f func(*Body)) Body { b := completeBody(); f(&b); return b }
	tests := []struct {
		name string
		body Body
		ok   bool
	}{
		{"description at limit", with(func(b *Body) { b.Description = repeat(MaxDescription) }), true},
		{"description over limit", with(func(b *Body) { b.Description = repeat(MaxDescription + 1) }), false},
		{"description untrimmed", with(func(b *Body) { b.Description = " x" }), false},
		{"bullet at limit", with(func(b *Body) { b.Bullets = []string{repeat(MaxBullet)} }), true},
		{"bullet over limit", with(func(b *Body) { b.Bullets = []string{repeat(MaxBullet + 1)} }), false},
		{"blank bullet", with(func(b *Body) { b.Bullets = []string{""} }), false},
		{"attr name at limit", with(func(b *Body) { b.Attributes = []Attribute{{Name: repeat(MaxAttrName), Value: "v"}} }), true},
		{"attr name over limit", with(func(b *Body) { b.Attributes = []Attribute{{Name: repeat(MaxAttrName + 1), Value: "v"}} }), false},
		{"attr empty name", with(func(b *Body) { b.Attributes = []Attribute{{Name: "", Value: "v"}} }), false},
		{"attr value at limit", with(func(b *Body) { b.Attributes = []Attribute{{Name: "n", Value: repeat(MaxAttrValue)}} }), true},
		{"attr value over limit", with(func(b *Body) { b.Attributes = []Attribute{{Name: "n", Value: repeat(MaxAttrValue + 1)}} }), false},
		{"attr empty value", with(func(b *Body) { b.Attributes = []Attribute{{Name: "n", Value: ""}} }), false},
		{"attr unit at limit", with(func(b *Body) { b.Attributes = []Attribute{{Name: "n", Value: "v", Unit: repeat(MaxAttrUnit)}} }), true},
		{"attr unit over limit", with(func(b *Body) { b.Attributes = []Attribute{{Name: "n", Value: "v", Unit: repeat(MaxAttrUnit + 1)}} }), false},
		{"alt at limit", with(func(b *Body) { b.Media[0].AltText = repeat(MaxAltText) }), true},
		{"alt over limit", with(func(b *Body) { b.Media[0].AltText = repeat(MaxAltText + 1) }), false},
		{"alt empty", with(func(b *Body) { b.Media[0].AltText = "" }), false},
		{"url at limit", with(func(b *Body) { b.Media[0].URL = "https://h.invalid/" + repeat(MaxURL-len("https://h.invalid/")) }), true},
		{"url over limit", with(func(b *Body) { b.Media[0].URL = "https://h.invalid/" + repeat(MaxURL-len("https://h.invalid/")+1) }), false},
		{"url empty", with(func(b *Body) { b.Media[0].URL = "" }), false},
		{"url relative", with(func(b *Body) { b.Media[0].URL = "/a.jpg" }), false},
		{"url ftp", with(func(b *Body) { b.Media[0].URL = "ftp://h.invalid/a.jpg" }), false},
		{"url no host", with(func(b *Body) { b.Media[0].URL = "https:///a.jpg" }), false},
		{"url unparsable", with(func(b *Body) { b.Media[0].URL = "https://h.invalid/%zz" }), false},
		{"url padded", with(func(b *Body) { b.Media[0].URL = " https://h.invalid/a.jpg" }), false},
		{"bad role", with(func(b *Body) { b.Media[0].Role = "HERO" }), false},
		{"bad media id", with(func(b *Body) { b.Media[0].MediaID = "-1" }), false},
		{"duplicate media id", with(func(b *Body) { b.Media[1].MediaID = b.Media[0].MediaID }), false},
		{"bad category id", with(func(b *Body) { b.PrimaryCategoryID = "bad id" }), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Create("item-1", tt.body, ItemActive, cats{"cat-bottles": true, "bad id": true}, t0)
			if tt.ok && err != nil {
				t.Fatalf("Create: %v", err)
			}
			if !tt.ok {
				wantErr(t, err, shared.ErrValidation)
			}
		})
	}
}

func TestCollectionBoundsWinOverFieldErrors(t *testing.T) {
	b := Body{Description: " bad"}
	for i := 0; i <= MaxBullets; i++ {
		b.Bullets = append(b.Bullets, "b")
	}
	_, err := Create("item-1", b, ItemActive, tree, t0)
	wantErr(t, err, ErrTooManyBullets)
}

func TestFieldErrorsWinOverTheCategory(t *testing.T) {
	_, err := Create("item-1", Body{Description: " bad", PrimaryCategoryID: "cat-nope"}, ItemActive, tree, t0)
	wantErr(t, err, shared.ErrValidation)
}

func TestItemStateWinsOverTheBody(t *testing.T) {
	_, err := Create("item-1", Body{PrimaryCategoryID: "cat-nope"}, ItemDiscontinued, tree, t0)
	wantErr(t, err, ErrItemDiscontinued)
}
