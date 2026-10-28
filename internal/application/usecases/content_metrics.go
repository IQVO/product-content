package usecases

import (
	"errors"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// publishRejectionReasons maps a publish failure to the problem slug the REST
// adapter answers with (same vocabulary as apis/openapi.yaml), in match order.
var publishRejectionReasons = []struct {
	err    error
	reason string
}{
	{ports.ErrNotFound, "not-found"},
	{content.ErrContentRetired, "content-retired"},
	{content.ErrUnknownCatalogItem, "unknown-catalog-item"},
	{content.ErrItemDiscontinued, "item-discontinued"},
	{content.ErrIncompleteContent, "incomplete-content"},
	{content.ErrUnknownCategory, "unknown-category"},
	{category.ErrUnknownParent, "unknown-category"},
	{ports.ErrConcurrentUpdate, "concurrent-update"},
	{shared.ErrIllegalTransition, "illegal-transition"},
	{shared.ErrValidation, "validation-failed"},
}

// internalErrorReason is the reason of every failure that is not a refusal the
// caller can act on (a database error, a bug).
const internalErrorReason = "internal-error"

func publishRejectionReason(err error) string {
	for _, r := range publishRejectionReasons {
		if errors.Is(err, r.err) {
			return r.reason
		}
	}
	return internalErrorReason
}
