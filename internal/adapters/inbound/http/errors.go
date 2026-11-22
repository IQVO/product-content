package http

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/IQVO/product-content/internal/adapters/inbound/http/page"
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

// errUnknownQueryParameter marks a query parameter a collection GET does not
// declare (REST guideline 4.2): a consumer still sending a retired paging
// parameter fails loudly instead of reading page one forever.
var errUnknownQueryParameter = errors.New("unknown query parameter")

func errUnknownQueryParameterNamed(name string) error {
	return fmt.Errorf("%w: %s", errUnknownQueryParameter, name)
}

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
	idempotencyKeyInvalid  = problem{http.StatusBadRequest, "idempotency-key-invalid", "Idempotency-Key header is invalid"}
	idempotencyKeyReused   = problem{http.StatusConflict, "idempotency-key-reused", "Idempotency-Key was already used with a different request"}
)

func unprocessable(slug, title string) problem {
	return problem{http.StatusUnprocessableEntity, slug, title}
}

func conflict(slug, title string) problem {
	return problem{http.StatusConflict, slug, title}
}

// replayClass is what the Idempotency-Key middleware does with the answer of a
// problem type (ADR 0009): FINAL and OUTCOME answers are stored and replayed, a
// RETRYABLE one is not stored and its key is released. A 2xx is stored and a 5xx
// never is, whatever the class. The empty class is "unclassified" and is
// released like RETRYABLE (the fail-safe direction).
type replayClass string

const (
	// replayFinal: the answer is a function of the request (or of state that
	// only ever moves one way) alone; stored and replayed.
	replayFinal replayClass = "FINAL"
	// replayOutcome: the command committed state and then refused; the writes
	// commit with the stored answer. No problem type of this service is one
	// (every keyed use case refuses without committing: its unit of work is a
	// savepoint of the request's transaction, rolled back on the refusal).
	replayOutcome replayClass = "OUTCOME"
	// replayRetryable: nothing committed and the answer depends on state that
	// can change; not stored, the key is released, a retry executes again.
	replayRetryable replayClass = "RETRYABLE"
)

// stored reports whether an error answer of this class is remembered under its key.
func (c replayClass) stored() bool { return c == replayFinal || c == replayOutcome }

// catalogueEntry is one typed error, its problem and the replay class of that
// problem.
type catalogueEntry struct {
	err    error
	p      problem
	replay replayClass
}

// problemCatalogue maps every typed error to its problem and its replay class,
// in match order. The slugs are the ones of apis/openapi.yaml and
// .claude/rules/rest-api.md. ADR 0009 holds the reason of every class and a
// test keeps its table equal to this one; a new entry without a class fails
// TestEveryProblemTypeDeclaresAReplayClass. When unsure the class is RETRYABLE:
// a refusal that rolled everything back costs one more execution, a wrongly
// stored one pins a stale answer for 24 hours.
var problemCatalogue = []catalogueEntry{
	// The body is not valid JSON for the operation: a function of the request (decodeJSON, handlers.go).
	{errMalformedRequest, problem{http.StatusBadRequest, "malformed-request", "Malformed request"}, replayFinal},
	// GET only (a collection query parameter); classified for completeness.
	{errUnknownQueryParameter, problem{http.StatusBadRequest, "unknown-query-parameter", "Unknown query parameter"}, replayFinal},
	// GET only.
	{page.ErrInvalidLimit, problem{http.StatusBadRequest, "invalid-limit", "limit must be an integer between 1 and 500"}, replayFinal},
	// GET only.
	{page.ErrInvalidCursor, problem{http.StatusBadRequest, "invalid-cursor", "cursor is not a valid page token"}, replayFinal},
	// Shared slug, mixed causes: the content of the path (another request can create it under the same item id later) and a
	// category (ids are minted by the caller, category.go 222, so another request can create it too): RETRYABLE.
	{ports.ErrNotFound, problem{http.StatusNotFound, "not-found", "Not found"}, replayRetryable},
	// The other not-found: a category the tree does not hold yet, created later by any caller-minted id.
	{category.ErrCategoryNotFound, problem{http.StatusNotFound, "not-found", "Not found"}, replayRetryable},
	// Retirement is terminal and one-way: every command guards RETIRED by refusing (commands.go 144, 165, 187), only Retire
	// sets it (215), nothing unretires or deletes content, and this service owns the state. A retry can never pass it.
	{content.ErrContentRetired, conflict("content-retired", "Retired content never changes or publishes again"), replayFinal},
	// Never raised (it guards a future category removal, category.go 8); its cause, published content under the category,
	// clears when that content is retired, so it can change: RETRYABLE.
	{category.ErrCategoryInUse, conflict("category-in-use", "A category in use by published content cannot change"), replayRetryable},
	// The item is a fact of the item master this service only learns from CatalogItemCreated events (ADR 0003): the check
	// reads the local read model (commands.go 107-113), which another event fills a moment later.
	{content.ErrUnknownCatalogItem, unprocessable("unknown-catalog-item", "The catalogue item is not known to this service"), replayRetryable},
	// The same read model, fed by CatalogItemDiscontinued; the owning service's facts can change again.
	{content.ErrItemDiscontinued, unprocessable("item-discontinued", "The catalogue item is discontinued"), replayRetryable},
	// The publish gate (description, one PRIMARY medium, a category) is satisfied by other requests' revisions first.
	{content.ErrIncompleteContent, unprocessable("incomplete-content", "Publishing needs a description, one PRIMARY medium and a category"), replayRetryable},
	// The primary category must be in the live tree (commands.go 28-30), and the tree changes with every category write.
	{content.ErrUnknownCategory, unprocessable("unknown-category", "The category is not in the category tree"), replayRetryable},
	// The parent of a create or a move is in that same live tree.
	{category.ErrUnknownParent, unprocessable("unknown-category", "The category is not in the category tree"), replayRetryable},
	// The body of the request alone (commands.go 14-16).
	{content.ErrTooManyBullets, unprocessable("too-many-bullets", "Content holds at most 10 bullets"), replayFinal},
	// The body of the request alone (commands.go 17-19).
	{content.ErrTooManyAttributes, unprocessable("too-many-attributes", "Content holds at most 50 attributes"), replayFinal},
	// The body of the request alone (commands.go 20-22).
	{content.ErrTooManyMedia, unprocessable("too-many-media", "Content holds at most 12 media"), replayFinal},
	// The subtree of the category changes with every move of another request.
	{category.ErrCategoryCycle, unprocessable("category-cycle", "A category never moves under its own subtree"), replayRetryable},
	// The tree grows and shrinks around the parent; the depth under it changes.
	{category.ErrMaxDepth, unprocessable("max-depth", "The category tree is at most four levels deep"), replayRetryable},
	// A sibling rename (an unkeyed PUT) clears the clash.
	{category.ErrDuplicateSiblingName, unprocessable("duplicate-sibling-name", "Sibling categories never share a name"), replayRetryable},
	// The optimistic-lock guard after the use case's own commandAttempts (deps.go transact).
	{ports.ErrConcurrentUpdate, conflict("concurrent-update", "The resource was modified by another request; retry"), replayRetryable},
	// The aggregate state the request names (DRAFT vs PUBLISHED vs RETIRED) is moved by other requests.
	{shared.ErrIllegalTransition, conflict("illegal-transition", "The content does not allow this change in its current state"), replayRetryable},
	// Mixed: the fields of the request (commands.go checkFields) but also the tree's size limit (category.go 54) - one class
	// per slug, so RETRYABLE; splitting the slug is a recorded follow-up.
	{shared.ErrValidation, unprocessable("validation-failed", "Validation failed"), replayRetryable},
}

// entryFor returns the first catalogue entry err matches; an unmatched error is
// the internal-error problem with no class.
func entryFor(err error) catalogueEntry {
	for _, entry := range problemCatalogue {
		if errors.Is(err, entry.err) {
			return entry
		}
	}
	return catalogueEntry{p: internalProblem}
}
