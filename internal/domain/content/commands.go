package content

import (
	"net/url"
	"strings"
	"time"

	"github.com/IQVO/product-content/internal/domain/shared"
)

// checkBody validates a body: collection bounds first (their own errors), then
// every field, then the category against the current tree. An empty
// description, no media and an empty category are all allowed here: a DRAFT may
// be incomplete (completeness is the publish gate, see complete).
func checkBody(b Body, cats Categories) error {
	if len(b.Bullets) > MaxBullets {
		return ErrTooManyBullets
	}
	if len(b.Attributes) > MaxAttributes {
		return ErrTooManyAttributes
	}
	if len(b.Media) > MaxMedia {
		return ErrTooManyMedia
	}
	if err := checkFields(b); err != nil {
		return err
	}
	if b.PrimaryCategoryID != "" && !cats.Has(b.PrimaryCategoryID) {
		return ErrUnknownCategory
	}
	return nil
}

func checkFields(b Body) error {
	if b.Description != "" && !shared.ValidTextN(b.Description, MaxDescription) {
		return shared.Invalid("description must be trimmed text of at most %d characters", MaxDescription)
	}
	for i, s := range b.Bullets {
		if !shared.ValidTextN(s, MaxBullet) {
			return shared.Invalid("bullet %d must be non-blank trimmed text of at most %d characters", i+1, MaxBullet)
		}
	}
	for i, a := range b.Attributes {
		if !shared.ValidTextN(a.Name, MaxAttrName) || !shared.ValidTextN(a.Value, MaxAttrValue) {
			return shared.Invalid("attribute %d needs a name (at most %d) and a value (at most %d)", i+1, MaxAttrName, MaxAttrValue)
		}
		if a.Unit != "" && !shared.ValidTextN(a.Unit, MaxAttrUnit) {
			return shared.Invalid("attribute %d unit must be at most %d characters", i+1, MaxAttrUnit)
		}
	}
	if err := checkMedia(b.Media); err != nil {
		return err
	}
	if b.PrimaryCategoryID != "" && !shared.ValidID(b.PrimaryCategoryID) {
		return shared.Invalid("primaryCategoryId is not a valid id")
	}
	return nil
}

func checkMedia(media []Media) error {
	seen := map[string]bool{}
	for i, m := range media {
		if !shared.ValidID(m.MediaID) || seen[m.MediaID] {
			return shared.Invalid("medium %d needs a unique valid mediaId", i+1)
		}
		seen[m.MediaID] = true
		if !validURL(m.URL) {
			return shared.Invalid("medium %d url must be an absolute http(s) URL of at most %d characters", i+1, MaxURL)
		}
		if !shared.ValidTextN(m.AltText, MaxAltText) {
			return shared.Invalid("medium %d needs altText of at most %d characters", i+1, MaxAltText)
		}
		if !m.Role.Valid() {
			return shared.Invalid("medium %d role must be PRIMARY or GALLERY", i+1)
		}
	}
	return nil
}

func validURL(s string) bool {
	if s == "" || len(s) > MaxURL || s != strings.TrimSpace(s) {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// complete reports whether the body satisfies the publish gate: a description,
// exactly one PRIMARY medium and a category.
func complete(b Body) bool {
	if b.Description == "" || b.PrimaryCategoryID == "" {
		return false
	}
	primary := 0
	for _, m := range b.Media {
		if m.Role == RolePrimary {
			primary++
		}
	}
	return primary == 1
}

func checkItem(s ItemStatus) error {
	switch s {
	case ItemActive:
		return nil
	case ItemDiscontinued:
		return ErrItemDiscontinued
	}
	return ErrUnknownCatalogItem
}

// Create is CreateProductContent: new DRAFT content at revision 1. The item
// must be known and not discontinued; the body is validated as for a revise.
func Create(id ID, b Body, item ItemStatus, cats Categories, now time.Time) (*ProductContent, error) {
	if !shared.ValidID(string(id)) {
		return nil, shared.Invalid("catalogItemId is not a valid id")
	}
	if err := checkItem(item); err != nil {
		return nil, err
	}
	if err := checkBody(b, cats); err != nil {
		return nil, err
	}
	now = shared.UTC(now)
	return &ProductContent{
		id: id, state: StateDraft, body: b.clone(), revision: 1, createdAt: now, updatedAt: now, version: 1,
	}, nil
}

func (c *ProductContent) touch(now time.Time) {
	c.updatedAt = shared.UTC(now)
	c.version++
}

// ReviseDraft is DRAFT -> DRAFT: it replaces the body and takes a new
// revision. RETIRED content is ErrContentRetired; PUBLISHED content is revised
// by ReviseContent (ErrIllegalTransition here).
func (c *ProductContent) ReviseDraft(b Body, cats Categories, now time.Time) error {
	switch c.state {
	case StateRetired:
		return ErrContentRetired
	case StatePublished:
		return shared.ErrIllegalTransition
	}
	if err := checkBody(b, cats); err != nil {
		return err
	}
	c.body = b.clone()
	c.revision++
	c.touch(now)
	return nil
}

// ReviseContent is PUBLISHED -> PUBLISHED: it replaces the body of live
// content and takes a new revision; shoppers see it once PublishContent
// publishes the revision. The body must stay complete, so PUBLISHED content is
// always publishable. A DRAFT is revised by ReviseDraft (ErrIllegalTransition
// here).
func (c *ProductContent) ReviseContent(b Body, cats Categories, now time.Time) error {
	switch c.state {
	case StateRetired:
		return ErrContentRetired
	case StateDraft:
		return shared.ErrIllegalTransition
	}
	if err := checkBody(b, cats); err != nil {
		return err
	}
	if !complete(b) {
		return ErrIncompleteContent
	}
	c.body = b.clone()
	c.revision++
	c.touch(now)
	return nil
}

// Publish is PublishContent: DRAFT -> PUBLISHED, or PUBLISHED -> PUBLISHED
// when a revision is waiting. It raises ProductContentPublished for the current
// revision. PUBLISHED content whose current revision is already published has
// nothing to publish: the call changes nothing and raises no event.
func (c *ProductContent) Publish(item ItemStatus, cats Categories, now time.Time) ([]shared.Event, error) {
	if c.state == StateRetired {
		return nil, ErrContentRetired
	}
	if err := checkItem(item); err != nil {
		return nil, err
	}
	if !complete(c.body) {
		return nil, ErrIncompleteContent
	}
	if !cats.Has(c.body.PrimaryCategoryID) {
		return nil, ErrUnknownCategory
	}
	if c.state == StatePublished && c.publishedRevision == c.revision {
		return nil, nil
	}
	c.state = StatePublished
	c.publishedRevision = c.revision
	c.publishedAt = shared.UTC(now)
	c.touch(now)
	b := c.body.clone()
	return []shared.Event{ProductContentPublished{
		Header: shared.Header{Version: c.version, At: c.updatedAt}, CatalogItemID: c.id, Revision: c.revision,
		Description: b.Description, Bullets: b.Bullets, Attributes: b.Attributes, Media: b.Media,
		PrimaryCategoryID: b.PrimaryCategoryID,
	}}, nil
}

func (c *ProductContent) retire(reason RetiredReason, now time.Time) {
	c.state = StateRetired
	c.reason = reason
	c.retiredAt = shared.UTC(now)
	c.touch(now)
}

func (c *ProductContent) retiredEvent() shared.Event {
	return ProductContentRetired{Header: shared.Header{Version: c.version, At: c.updatedAt}, CatalogItemID: c.id, Revision: c.revision}
}

// Retire is RetireContent: PUBLISHED -> RETIRED (reason MANUAL). A DRAFT is
// discarded instead (ErrIllegalTransition), RETIRED is terminal.
func (c *ProductContent) Retire(now time.Time) ([]shared.Event, error) {
	if c.state != StatePublished {
		return nil, shared.ErrIllegalTransition
	}
	c.retire(ReasonManual, now)
	return []shared.Event{c.retiredEvent()}, nil
}

// DiscardDraft is DRAFT -> RETIRED (reason DISCARDED). It raises no event:
// nothing was ever published.
func (c *ProductContent) DiscardDraft(now time.Time) error {
	if c.state != StateDraft {
		return shared.ErrIllegalTransition
	}
	c.retire(ReasonDiscarded, now)
	return nil
}

// RetireOnDiscontinue is the policy of the same name: the network discontinued
// the item. PUBLISHED content retires with ProductContentRetired, a DRAFT
// retires silently (nothing was published), RETIRED content is left alone.
func (c *ProductContent) RetireOnDiscontinue(now time.Time) []shared.Event {
	switch c.state {
	case StatePublished:
		c.retire(ReasonDiscontinued, now)
		return []shared.Event{c.retiredEvent()}
	case StateDraft:
		c.retire(ReasonDiscontinued, now)
	}
	return nil
}
