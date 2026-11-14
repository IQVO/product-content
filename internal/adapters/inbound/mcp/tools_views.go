package mcp

import (
	"context"
	"time"

	"github.com/IQVO/product-content/internal/application/views"
	"github.com/IQVO/product-content/internal/domain/content"
)

// --- outputs (the REST bodies, camelCase like the REST contract) --------------

type attributeView struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type mediaView struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

// contentView is the wire shape of one product content (the REST body): the
// arrays are always present, the optional scalars and instants are omitted
// when unset.
type contentView struct {
	CatalogItemID     string          `json:"catalogItemId"`
	State             string          `json:"state"`
	RetiredReason     string          `json:"retiredReason,omitempty"`
	Description       string          `json:"description"`
	Bullets           []string        `json:"bullets"`
	Attributes        []attributeView `json:"attributes"`
	Media             []mediaView     `json:"media"`
	PrimaryCategoryID string          `json:"primaryCategoryId,omitempty"`
	Revision          int             `json:"revision"`
	PublishedRevision int             `json:"publishedRevision"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	PublishedAt       *time.Time      `json:"publishedAt,omitempty"`
	RetiredAt         *time.Time      `json:"retiredAt,omitempty"`
}

type categoryView struct {
	CategoryID string `json:"categoryId"`
	ParentID   string `json:"parentId,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
}

// categoryTreeView is the wire shape of GET /categories.
type categoryTreeView struct {
	Version    int64          `json:"version"`
	Categories []categoryView `json:"categories"`
}

// --- mapping ------------------------------------------------------------------

func toContent(v views.Content) contentView {
	out := contentView{
		CatalogItemID: v.CatalogItemID, State: v.State, RetiredReason: v.RetiredReason, Description: v.Description,
		Bullets: v.Bullets, PrimaryCategoryID: v.PrimaryCategoryID, Revision: v.Revision, PublishedRevision: v.PublishedRevision,
		CreatedAt: v.CreatedAt.UTC(), UpdatedAt: v.UpdatedAt.UTC(), PublishedAt: instant(v.PublishedAt), RetiredAt: instant(v.RetiredAt),
		Attributes: make([]attributeView, len(v.Attributes)), Media: make([]mediaView, len(v.Media)),
	}
	if out.Bullets == nil {
		out.Bullets = []string{}
	}
	for i, a := range v.Attributes {
		out.Attributes[i] = attributeView{Name: a.Name, Value: a.Value, Unit: a.Unit}
	}
	for i, m := range v.Media {
		out.Media[i] = mediaView{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: m.Role}
	}
	return out
}

func toCategoryTree(v views.CategoryTree) categoryTreeView {
	out := categoryTreeView{Version: v.Version, Categories: make([]categoryView, len(v.Categories))}
	for i, c := range v.Categories {
		out.Categories[i] = categoryView{CategoryID: c.CategoryID, ParentID: c.ParentID, Name: c.Name, Path: c.Path}
	}
	return out
}

// --- handlers -----------------------------------------------------------------

func (d Deps) getProductContent(ctx context.Context, in catalogItemInput) (contentView, error) {
	if err := checkItemID(in.CatalogItemID); err != nil {
		return contentView{}, err
	}
	v, err := d.GetProductContent.Execute(ctx, content.ID(in.CatalogItemID))
	if err != nil {
		return contentView{}, mapError(ctx, err)
	}
	return toContent(v), nil
}

func (d Deps) listCategories(ctx context.Context, _ noInput) (categoryTreeView, error) {
	v, err := d.ListCategories.Execute(ctx)
	if err != nil {
		return categoryTreeView{}, mapError(ctx, err)
	}
	return toCategoryTree(v), nil
}
