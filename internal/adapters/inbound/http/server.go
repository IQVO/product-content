// Package http is the REST inbound adapter: every route of apis/openapi.yaml on
// a chi router, RFC 9457 problem documents for every error and the liveness and
// readiness endpoints. It has no auth middleware (fleet decision 2026-09-11;
// internal/architecture's TestNoAuthMiddlewareReintroduced).
package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/riandyrn/otelchi"
	otelchimetric "github.com/riandyrn/otelchi/metric"

	"github.com/IQVO/product-content/internal/adapters/inbound/requestlog"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/usecases"
)

// Server holds the use cases the handlers call. The six POST routes sit behind
// the Idempotency-Key middleware; PUT and DELETE are idempotent by method and
// take none (a key sent to them is accepted and ignored).
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
	// POST routes (the key is global, not per route).
	IdempotencyStore ports.IdempotencyStore
	Clock            ports.Clock
	// Readiness backs GET /readyz; nil means always ready (unit tests).
	Readiness *Readiness
}

// DefaultServiceName names the server spans and the HTTP metrics when the
// composition root passes no service name.
const DefaultServiceName = "product-content"

// NewRouter builds the chi router for every endpoint of apis/openapi.yaml. A nil
// logger defaults to slog.Default(); an empty serviceName to DefaultServiceName.
//
// Middleware order is the fleet observability standard: RequestID, then otelchi
// (the server span, named after the chi route pattern, so one span name per
// route and not per catalog item id), then the request-duration histogram, then
// RequestLogger (it runs inside the span, so its record carries trace_id and
// span_id), then Recoverer. telemetry.Setup must have run before this call:
// otelchimetric binds the global MeterProvider when it is constructed.
func NewRouter(s *Server, logger *slog.Logger, serviceName string) http.Handler {
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(otelchi.Middleware(serviceName, otelchi.WithChiRoutes(r)))
	r.Use(otelchimetric.NewServerRequestDuration(otelchimetric.NewBaseConfig(serviceName)))
	r.Use(requestlog.RequestLogger(logger))
	r.Use(middleware.Recoverer)
	// A NUL (U+0000) in the path, the query or a body is a uniform client error,
	// never a Postgres 22021 -> 500; it runs before the Idempotency-Key
	// middleware so a rejected request never records its key.
	r.Use(rejectNUL)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.With(s.idempotent).Post("/product-contents", s.handleCreate)
	r.Get("/product-contents", s.handleList)
	r.Get("/product-contents/{catalogItemId}", s.handleGet)
	r.With(s.idempotent).Post("/product-contents/{catalogItemId}/revisions", s.handleRevise)
	r.With(s.idempotent).Post("/product-contents/{catalogItemId}/publications", s.handlePublish)
	r.With(s.idempotent).Post("/product-contents/{catalogItemId}/retirements", s.handleRetire)
	r.Delete("/product-contents/{catalogItemId}/draft", s.handleDiscard)

	r.With(s.idempotent).Post("/categories", s.handleCreateCategory)
	r.Get("/categories", s.handleListCategories)
	r.With(s.idempotent).Post("/category-tree-publications", s.handlePublishCategories)
	r.Put("/categories/{categoryId}/parent", s.handleMoveCategory)
	r.Put("/categories/{categoryId}/name", s.handleRenameCategory)
	return r
}

func (s *Server) idempotent(next http.Handler) http.Handler {
	return RequireIdempotencyKey(s.IdempotencyStore, s.Clock)(next)
}
