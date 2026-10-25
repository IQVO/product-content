// Package http is the REST inbound adapter: every route of apis/openapi.yaml on
// a chi router, RFC 9457 problem documents for every error and the liveness and
// readiness endpoints. It has no auth middleware (fleet decision 2026-09-11;
// internal/architecture's TestNoAuthMiddlewareReintroduced).
package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/usecases"
)

// Route names under which the Idempotency-Key records are stored.
const (
	routeCreate          = "POST /product-contents"
	routeRevise          = "POST /product-contents/{catalogItemId}:revise"
	routePublish         = "POST /product-contents/{catalogItemId}:publish"
	routeRetire          = "POST /product-contents/{catalogItemId}:retire"
	routeDiscard         = "POST /product-contents/{catalogItemId}:discard"
	routeCreateCategory  = "POST /categories"
	routePublishCategory = "POST /categories:publish"
	routeMoveCategory    = "POST /categories/{categoryId}:move"
	routeRenameCategory  = "POST /categories/{categoryId}:rename"
)

// Server holds the use cases the handlers call.
type Server struct {
	CreateProductContent usecases.CreateProductContent
	ReviseContent        usecases.ReviseContent
	PublishContent       usecases.PublishContent
	RetireContent        usecases.RetireContent
	DiscardDraft         usecases.DiscardDraft
	GetProductContent    usecases.GetProductContent
	ListProductContents  usecases.ListProductContents

	CreateCategory      usecases.CreateCategory
	MoveCategory        usecases.MoveCategory
	RenameCategory      usecases.RenameCategory
	PublishCategoryTree usecases.PublishCategoryTree
	ListCategories      usecases.ListCategories

	// IdempotencyStore and Clock back the Idempotency-Key middleware of the
	// POST routes.
	IdempotencyStore ports.IdempotencyStore
	Clock            ports.Clock
	// Readiness backs GET /readyz; nil means always ready (unit tests).
	Readiness *Readiness
}

// NewRouter builds the chi router for every endpoint of apis/openapi.yaml.
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	// A NUL (U+0000) in the path, the query or a body is a uniform client error,
	// never a Postgres 22021 -> 500; it runs before the Idempotency-Key
	// middleware so a rejected request never records its key.
	r.Use(rejectNUL)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.With(s.idempotent(routeCreate)).Post("/product-contents", s.handleCreate)
	r.Get("/product-contents", s.handleList)
	r.Get("/product-contents/{catalogItemId}", s.handleGet)
	r.With(s.idempotent(routeRevise)).Post("/product-contents/{catalogItemId}:revise", s.handleRevise)
	r.With(s.idempotent(routePublish)).Post("/product-contents/{catalogItemId}:publish", s.handlePublish)
	r.With(s.idempotent(routeRetire)).Post("/product-contents/{catalogItemId}:retire", s.handleRetire)
	r.With(s.idempotent(routeDiscard)).Post("/product-contents/{catalogItemId}:discard", s.handleDiscard)

	r.With(s.idempotent(routeCreateCategory)).Post("/categories", s.handleCreateCategory)
	r.Get("/categories", s.handleListCategories)
	r.With(s.idempotent(routePublishCategory)).Post("/categories:publish", s.handlePublishCategories)
	r.With(s.idempotent(routeMoveCategory)).Post("/categories/{categoryId}:move", s.handleMoveCategory)
	r.With(s.idempotent(routeRenameCategory)).Post("/categories/{categoryId}:rename", s.handleRenameCategory)
	return r
}

func (s *Server) idempotent(route string) func(http.Handler) http.Handler {
	return RequireIdempotencyKey(s.IdempotencyStore, s.Clock, route)
}
