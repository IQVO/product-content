package content

import (
	"errors"
	"fmt"

	"github.com/IQVO/product-content/internal/domain/shared"
)

// Restore rebuilds persisted content, validating every invariant a stored row
// must keep. A violation is shared.ErrInvalidSnapshot.
func Restore(s Snapshot) (*ProductContent, error) {
	if err := checkSnapshot(s); err != nil {
		return nil, shared.Corrupt("content %q: %v", s.ID, err)
	}
	return &ProductContent{
		id: s.ID, state: s.State, reason: s.RetiredReason, body: s.Body.clone(), revision: s.Revision,
		publishedRevision: s.PublishedRevision, createdAt: shared.UTC(s.CreatedAt), updatedAt: shared.UTC(s.UpdatedAt),
		publishedAt: shared.UTC(s.PublishedAt), retiredAt: shared.UTC(s.RetiredAt), version: s.Version, loaded: s.Version,
	}, nil
}

func checkSnapshot(s Snapshot) error {
	if !shared.ValidID(string(s.ID)) {
		return errors.New("catalogItemId must be a valid id")
	}
	if !s.State.Valid() {
		return fmt.Errorf("state %q is unknown", s.State)
	}
	if s.Version < 1 || s.Revision < 1 || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		return errors.New("version, revision, createdAt and updatedAt are required")
	}
	if len(s.Body.Bullets) > MaxBullets || len(s.Body.Attributes) > MaxAttributes || len(s.Body.Media) > MaxMedia {
		return errors.New("a collection exceeds its bound")
	}
	if err := checkFields(s.Body); err != nil {
		return err
	}
	if s.PublishedRevision < 0 || s.PublishedRevision > s.Revision {
		return errors.New("publishedRevision must be between 0 and the revision")
	}
	return checkLifecycle(s)
}

func checkLifecycle(s Snapshot) error {
	if (s.PublishedRevision > 0) != !s.PublishedAt.IsZero() {
		return errors.New("publishedAt is set exactly when a revision was published")
	}
	if (s.State == StateRetired) != !s.RetiredAt.IsZero() || (s.State == StateRetired) != (s.RetiredReason != "") {
		return errors.New("retiredAt and the retired reason are set exactly when the content is RETIRED")
	}
	switch s.State {
	case StateDraft:
		if s.PublishedRevision != 0 {
			return errors.New("a DRAFT was never published")
		}
	case StatePublished:
		if s.PublishedRevision < 1 || !complete(s.Body) {
			return errors.New("PUBLISHED content is published and complete")
		}
	case StateRetired:
		if !s.RetiredReason.Valid() {
			return fmt.Errorf("retired reason %q is unknown", s.RetiredReason)
		}
		if s.PublishedRevision > 0 && s.RetiredReason == ReasonDiscarded {
			return errors.New("only a DRAFT is discarded")
		}
	}
	return nil
}
