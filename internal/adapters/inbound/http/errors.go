package http

import (
	"errors"
	"net/http"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// problemBase is the namespace of this service's RFC 9457 `type` URIs.
const problemBase = "/problems/"

// errMalformedRequest marks a body or parameter the adapter itself rejects
// (bad JSON, unknown field, wrong type).
var errMalformedRequest = errors.New("request is not valid for this operation")

// problem is the fixed (status, slug, title) of one error category; the detail
// comes from the error at write time.
type problem struct {
	status int
	slug   string
	title  string
}

var internalProblem = problem{http.StatusInternalServerError, "internal-error", "Internal server error"}

// Problems the adapter writes itself, before any use case runs.
var (
	idempotencyKeyRequired = problem{http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key header is required"}
	idempotencyKeyReused   = problem{http.StatusUnprocessableEntity, "idempotency-key-reused", "Idempotency-Key was already used with a different request"}
)

func unprocessable(slug, title string) problem {
	return problem{http.StatusUnprocessableEntity, slug, title}
}

func conflict(slug, title string) problem {
	return problem{http.StatusConflict, slug, title}
}

// problemCatalogue maps every typed error to its problem, in match order. The
// slugs are the ones of apis/openapi.yaml and .claude/rules/rest-api.md.
var problemCatalogue = []struct {
	err error
	p   problem
}{
	{errMalformedRequest, problem{http.StatusBadRequest, "malformed-request", "Malformed request"}},
	{ports.ErrNotFound, problem{http.StatusNotFound, "not-found", "Not found"}},
	{category.ErrCategoryNotFound, problem{http.StatusNotFound, "not-found", "Not found"}},
	{content.ErrContentRetired, conflict("content-retired", "Retired content never changes or publishes again")},
	{category.ErrCategoryInUse, conflict("category-in-use", "A category in use by published content cannot change")},
	{content.ErrUnknownCatalogItem, unprocessable("unknown-catalog-item", "The catalogue item is not known to this service")},
	{content.ErrItemDiscontinued, unprocessable("item-discontinued", "The catalogue item is discontinued")},
	{content.ErrIncompleteContent, unprocessable("incomplete-content", "Publishing needs a description, one PRIMARY medium and a category")},
	{content.ErrUnknownCategory, unprocessable("unknown-category", "The category is not in the category tree")},
	{category.ErrUnknownParent, unprocessable("unknown-category", "The category is not in the category tree")},
	{content.ErrTooManyBullets, unprocessable("too-many-bullets", "Content holds at most 10 bullets")},
	{content.ErrTooManyAttributes, unprocessable("too-many-attributes", "Content holds at most 50 attributes")},
	{content.ErrTooManyMedia, unprocessable("too-many-media", "Content holds at most 12 media")},
	{category.ErrCategoryCycle, unprocessable("category-cycle", "A category never moves under its own subtree")},
	{category.ErrMaxDepth, unprocessable("max-depth", "The category tree is at most four levels deep")},
	{category.ErrDuplicateSiblingName, unprocessable("duplicate-sibling-name", "Sibling categories never share a name")},
	{ports.ErrConcurrentUpdate, conflict("concurrent-update", "The resource was modified by another request; retry")},
	{shared.ErrIllegalTransition, conflict("illegal-transition", "The content does not allow this change in its current state")},
	{shared.ErrValidation, unprocessable("validation-failed", "Validation failed")},
}

// problemFor returns the problem of the first catalogue entry err matches, or
// the internal-error problem.
func problemFor(err error) problem {
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return entry.p
		}
	}
	return internalProblem
}
