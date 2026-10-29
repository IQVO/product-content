// Package views holds the read DTOs the use cases return. They are plain data
// (no behaviour beyond the From... mappers) so the ports package can stay
// interfaces-only and the HTTP adapter can encode them directly.
package views

import (
	"time"

	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
)

// Attribute is one named fact of an item.
type Attribute struct {
	Name  string
	Value string
	Unit  string
}

// Media is one picture of an item.
type Media struct {
	MediaID string
	URL     string
	AltText string
	Role    string
}

// Content is the product content of one catalogue item.
type Content struct {
	CatalogItemID     string
	State             string
	RetiredReason     string
	Description       string
	Bullets           []string
	Attributes        []Attribute
	Media             []Media
	PrimaryCategoryID string
	Revision          int
	PublishedRevision int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PublishedAt       time.Time
	RetiredAt         time.Time
}

// ContentFrom maps the aggregate to its view; the arrays are never nil.
func ContentFrom(c *content.ProductContent) Content {
	b := c.Body()
	v := Content{
		CatalogItemID: string(c.ID()), State: string(c.State()), RetiredReason: string(c.RetiredReason()),
		Description: b.Description, Bullets: b.Bullets, PrimaryCategoryID: b.PrimaryCategoryID,
		Revision: c.Revision(), PublishedRevision: c.PublishedRevision(), CreatedAt: c.CreatedAt(), UpdatedAt: c.UpdatedAt(),
		PublishedAt: c.PublishedAt(), RetiredAt: c.RetiredAt(),
		Attributes: make([]Attribute, len(b.Attributes)), Media: make([]Media, len(b.Media)),
	}
	for i, a := range b.Attributes {
		v.Attributes[i] = Attribute{Name: a.Name, Value: a.Value, Unit: a.Unit}
	}
	for i, m := range b.Media {
		v.Media[i] = Media{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: string(m.Role)}
	}
	return v
}

// ContentPage is one page of GET /product-contents. Next is the cursor of the
// next page, "" on the last one.
type ContentPage struct {
	Items []Content
	Next  string
}

// Category is one node of the tree.
type Category struct {
	CategoryID string
	ParentID   string
	Name       string
	Path       string
}

// CategoryFrom maps a category.
func CategoryFrom(c category.Category) Category {
	return Category{CategoryID: c.ID, ParentID: c.ParentID, Name: c.Name, Path: c.Path}
}

// CategoryTree is the tree at one version.
type CategoryTree struct {
	Version    int64
	Categories []Category
}

// CategoryTreeFrom maps the tree; the list is never nil.
func CategoryTreeFrom(t *category.Tree) CategoryTree {
	cs := t.Categories()
	v := CategoryTree{Version: t.Version(), Categories: make([]Category, len(cs))}
	for i, c := range cs {
		v.Categories[i] = CategoryFrom(c)
	}
	return v
}
