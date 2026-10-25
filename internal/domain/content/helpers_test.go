package content

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type cats map[string]bool

func (c cats) Has(id string) bool { return c[id] }

var (
	t0       = time.Date(2026, 10, 20, 9, 0, 0, 123456000, time.UTC)
	t1       = t0.Add(time.Hour)
	t2       = t1.Add(time.Hour)
	tree     = cats{"cat-bottles": true, "cat-cups": true}
	emptyDoc = Body{}
)

func completeBody() Body {
	return Body{
		Description:       "A 2-litre insulated steel bottle.",
		Bullets:           []string{"Double-wall", "Dishwasher-safe cap"},
		Attributes:        []Attribute{{Name: "capacity", Value: "2", Unit: "l"}, {Name: "material", Value: "steel"}},
		Media:             []Media{{MediaID: "med-1", URL: "https://static.example.invalid/a.jpg", AltText: "side", Role: RolePrimary}, {MediaID: "med-2", URL: "http://static.example.invalid/b.jpg", AltText: "cap", Role: RoleGallery}},
		PrimaryCategoryID: "cat-bottles",
	}
}

func mustCreate(t *testing.T, b Body) *ProductContent {
	t.Helper()
	c, err := Create("item-1", b, ItemActive, tree, t0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return c
}

func mustPublished(t *testing.T) *ProductContent {
	t.Helper()
	c := mustCreate(t, completeBody())
	if _, err := c.Publish(ItemActive, tree, t1); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return c
}

func wantErr(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func repeat(n int) string { return strings.Repeat("x", n) }

var zeroTime time.Time

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}
