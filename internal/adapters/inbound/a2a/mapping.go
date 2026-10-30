package a2a

import (
	"fmt"
	"net/http"
	"time"

	"github.com/IQVO/product-content/internal/application/views"
)

// problemBase namespaces this context's problem types, the same namespace as
// the REST adapter's RFC 9457 `type` URIs (ADR 0006); the shared classes reuse
// the REST slugs so a delegating agent sees the outcome a REST caller would.
const problemBase = "/problems/"

// Problem slugs. The first three are the REST adapter's own; the last three
// are adapter-owned request-shape problems (HTTP class 400, like REST's
// malformed-request).
const (
	slugNotFound         = "not-found"
	slugValidationFailed = "validation-failed"
	slugInternal         = "internal-error"
	slugMalformedSkill   = "malformed-skill-request"
	slugUnknownSkill     = "unknown-skill"
	slugInvalidArgument  = "invalid-argument"
)

// problem is the RFC 7807/9457-shaped rejection an agent client receives
// instead of a result: domain rejections and request-shape failures are
// business answers, not transport errors, so they travel in the message
// envelope. Status carries the class the REST adapter would have answered.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

// summary renders the one-line form used for the reply's text part.
func (p *problem) summary() string {
	return fmt.Sprintf("%s (%d): %s", p.Title, p.Status, p.Detail)
}

func problemMalformedRequest(detail string) *problem {
	return &problem{Type: problemBase + slugMalformedSkill, Title: "Malformed skill request", Detail: detail, Status: http.StatusBadRequest}
}

func problemUnknownSkill(skill string) *problem {
	return &problem{Type: problemBase + slugUnknownSkill, Title: "Unknown skill", Detail: fmt.Sprintf("skill %q is not offered by this agent; the Agent Card lists the supported skills", skill), Status: http.StatusBadRequest}
}

func problemInvalidArgument(detail string) *problem {
	return &problem{Type: problemBase + slugInvalidArgument, Title: "Invalid skill argument", Detail: detail, Status: http.StatusBadRequest}
}

func problemNotFound(detail string) *problem {
	return &problem{Type: problemBase + slugNotFound, Title: "Not found", Detail: detail, Status: http.StatusNotFound}
}

func problemValidation(detail string) *problem {
	return &problem{Type: problemBase + slugValidationFailed, Title: "Validation failed", Detail: detail, Status: http.StatusUnprocessableEntity}
}

func problemInternal() *problem {
	return &problem{Type: problemBase + slugInternal, Title: "Internal server error", Detail: "an unexpected error occurred", Status: http.StatusInternalServerError}
}

// envelope is the structured payload of every skill answer: exactly one of
// Data (a skill DTO) or Problem (why there is no result), plus the skill id
// the answer belongs to (empty when the request named none).
type envelope struct {
	Skill   string    `json:"skill"`
	Data    *skillDTO `json:"data,omitempty"`
	Problem *problem  `json:"problem,omitempty"`
}

// skillDTO is the union of the skill result DTOs. A2A parts are
// machine-readable JSON; each skill fills its own field, keeping the envelope
// flat and versioned as one contract.
type skillDTO struct {
	ProductContent *productContent `json:"productContent,omitempty"`
	CategoryTree   *categoryTree   `json:"categoryTree,omitempty"`
}

// result is a skill's DTO plus the one-line summary of the reply's text part.
type result struct {
	dto     skillDTO
	summary string
}

// The DTOs below mirror the REST adapter's response bodies field for field
// (internal/adapters/inbound/http/dto.go): the A2A reads expose what the REST
// GETs expose and nothing more.

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

// productContent is the get_product_content answer.
type productContent struct {
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

type categoryDTO struct {
	CategoryID string `json:"categoryId"`
	ParentID   string `json:"parentId,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
}

// categoryTree is the list_categories answer.
type categoryTree struct {
	Version    int64         `json:"version"`
	Categories []categoryDTO `json:"categories"`
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// toProductContent maps the use case view to the skill DTO and its one-line
// summary. The description is left out of the summary: it can be long.
func toProductContent(v views.Content) result {
	out := productContent{
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
	summary := fmt.Sprintf("%s: %s, revision %d (published revision %d), %d bullets, %d attributes, %d media",
		out.CatalogItemID, out.State, out.Revision, out.PublishedRevision, len(out.Bullets), len(out.Attributes), len(out.Media))
	return result{dto: skillDTO{ProductContent: &out}, summary: summary}
}

// toCategoryTree maps the use case view to the skill DTO and its summary.
func toCategoryTree(v views.CategoryTree) result {
	out := categoryTree{Version: v.Version, Categories: make([]categoryDTO, len(v.Categories))}
	for i, c := range v.Categories {
		out.Categories[i] = categoryDTO{CategoryID: c.CategoryID, ParentID: c.ParentID, Name: c.Name, Path: c.Path}
	}
	summary := fmt.Sprintf("category tree version %d: %d categories", out.Version, len(out.Categories))
	return result{dto: skillDTO{CategoryTree: &out}, summary: summary}
}
