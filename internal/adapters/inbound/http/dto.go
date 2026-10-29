package http

import (
	"time"

	"github.com/IQVO/product-content/internal/application/views"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// Request bodies (apis/openapi.yaml ProductContentInput, ReviseInput,
// CategoryInput, MoveCategoryRequest, RenameCategoryRequest).

type attributeDTO struct {
	Name  string  `json:"name"`
	Value string  `json:"value"`
	Unit  *string `json:"unit,omitempty"`
}

type mediaDTO struct {
	MediaID string `json:"mediaId"`
	URL     string `json:"url"`
	AltText string `json:"altText"`
	Role    string `json:"role"`
}

// contentRequest is both ProductContentInput and ReviseInput: the revise body
// may leave catalogItemId out.
type contentRequest struct {
	CatalogItemID     string         `json:"catalogItemId"`
	Description       string         `json:"description"`
	Bullets           []string       `json:"bullets"`
	Attributes        []attributeDTO `json:"attributes"`
	Media             []mediaDTO     `json:"media"`
	PrimaryCategoryID string         `json:"primaryCategoryId"`
}

type categoryRequest struct {
	ParentID *string `json:"parentId"`
	Name     string  `json:"name"`
}

type moveRequest struct {
	ParentID *string `json:"parentId"`
}

// parentOrRoot maps an absent or null parentId to the root ("").
func parentOrRoot(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// body maps the request to the domain's editable body.
func (r contentRequest) body() content.Body {
	b := content.Body{Description: r.Description, Bullets: r.Bullets, PrimaryCategoryID: r.PrimaryCategoryID}
	for _, a := range r.Attributes {
		attr := content.Attribute{Name: a.Name, Value: a.Value}
		if a.Unit != nil {
			attr.Unit = *a.Unit
		}
		b.Attributes = append(b.Attributes, attr)
	}
	for _, m := range r.Media {
		b.Media = append(b.Media, content.Media{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: content.Role(m.Role)})
	}
	return b
}

// forRevise checks the optional body id against the path id.
func (r contentRequest) forRevise(pathID string) (content.ID, content.Body, error) {
	if r.CatalogItemID != "" && r.CatalogItemID != pathID {
		return "", content.Body{}, shared.Invalid("catalogItemId in the body must match the path")
	}
	return content.ID(pathID), r.body(), nil
}

// Responses.

type contentResponse struct {
	CatalogItemID     string         `json:"catalogItemId"`
	State             string         `json:"state"`
	RetiredReason     string         `json:"retiredReason,omitempty"`
	Description       string         `json:"description"`
	Bullets           []string       `json:"bullets"`
	Attributes        []attributeDTO `json:"attributes"`
	Media             []mediaDTO     `json:"media"`
	PrimaryCategoryID string         `json:"primaryCategoryId,omitempty"`
	Revision          int            `json:"revision"`
	PublishedRevision int            `json:"publishedRevision"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	PublishedAt       *time.Time     `json:"publishedAt,omitempty"`
	RetiredAt         *time.Time     `json:"retiredAt,omitempty"`
}

type contentListResponse struct {
	Items []contentResponse `json:"items"`
	Next  string            `json:"next,omitempty"`
}

type categoryResponse struct {
	CategoryID string `json:"categoryId"`
	ParentID   string `json:"parentId,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
}

type categoryListResponse struct {
	Version    int64              `json:"version"`
	Categories []categoryResponse `json:"categories"`
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func contentDTO(v views.Content) contentResponse {
	out := contentResponse{
		CatalogItemID: v.CatalogItemID, State: v.State, RetiredReason: v.RetiredReason, Description: v.Description,
		Bullets: v.Bullets, PrimaryCategoryID: v.PrimaryCategoryID, Revision: v.Revision, PublishedRevision: v.PublishedRevision,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, PublishedAt: optTime(v.PublishedAt), RetiredAt: optTime(v.RetiredAt),
		Attributes: make([]attributeDTO, len(v.Attributes)), Media: make([]mediaDTO, len(v.Media)),
	}
	if out.Bullets == nil {
		out.Bullets = []string{}
	}
	for i, a := range v.Attributes {
		out.Attributes[i] = attributeDTO{Name: a.Name, Value: a.Value}
		if a.Unit != "" {
			u := a.Unit
			out.Attributes[i].Unit = &u
		}
	}
	for i, m := range v.Media {
		out.Media[i] = mediaDTO{MediaID: m.MediaID, URL: m.URL, AltText: m.AltText, Role: m.Role}
	}
	return out
}

func categoryDTO(v views.Category) categoryResponse {
	return categoryResponse{CategoryID: v.CategoryID, ParentID: v.ParentID, Name: v.Name, Path: v.Path}
}

func categoryListDTO(v views.CategoryTree) categoryListResponse {
	out := categoryListResponse{Version: v.Version, Categories: make([]categoryResponse, len(v.Categories))}
	for i, c := range v.Categories {
		out.Categories[i] = categoryDTO(c)
	}
	return out
}
