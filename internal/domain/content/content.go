// Package content is the ProductContent aggregate of the product-content
// bounded context: the sellable content (description, bullets, attributes and
// media) of one catalogue item, identified by the catalogItemId.
//
// Lifecycle (ADR 0002): DRAFT -> PUBLISHED -> RETIRED, plus DRAFT -> DRAFT
// (ReviseDraft), PUBLISHED -> PUBLISHED (ReviseContent, then PublishContent
// again with the new revision) and DRAFT -> RETIRED (DiscardDraft and the
// RetireOnDiscontinue policy). RETIRED is terminal.
//
// The package imports only the shared domain package and the standard library.
// What it needs from the rest of the context (whether the catalogue item is
// known and discontinued, whether a category exists) is handed in by the
// caller as plain values.
package content

import (
	"errors"
	"time"
)

// Domain errors, one per Err of the model (problem types are their kebab-case
// names, see apis/openapi.yaml).
var (
	// ErrUnknownCatalogItem: the item is not in the local CatalogItemView.
	ErrUnknownCatalogItem = errors.New("unknown catalog item")
	// ErrItemDiscontinued: the item is discontinued, so content is neither
	// created nor published for it.
	ErrItemDiscontinued = errors.New("item discontinued")
	// ErrIncompleteContent: publishing needs a description, exactly one
	// PRIMARY medium and a category.
	ErrIncompleteContent = errors.New("incomplete content")
	// ErrUnknownCategory: primaryCategoryId names no category of the current tree.
	ErrUnknownCategory = errors.New("unknown category")
	// ErrContentRetired: RETIRED content never changes or publishes again.
	ErrContentRetired = errors.New("content retired")
	// ErrTooManyBullets: more than MaxBullets bullets.
	ErrTooManyBullets = errors.New("too many bullets")
	// ErrTooManyAttributes: more than MaxAttributes attributes.
	ErrTooManyAttributes = errors.New("too many attributes")
	// ErrTooManyMedia: more than MaxMedia media.
	ErrTooManyMedia = errors.New("too many media")
)

// Collection bounds and field limits.
const (
	MaxBullets    = 10
	MaxAttributes = 50
	MaxMedia      = 12

	MaxDescription = 4000
	MaxBullet      = 300
	MaxAttrName    = 100
	MaxAttrValue   = 300
	MaxAttrUnit    = 20
	MaxAltText     = 200
	MaxURL         = 2048
)

// ID is the catalogItemId, the aggregate root id.
type ID string

// State is the lifecycle state of the content.
type State string

// The states.
const (
	StateDraft     State = "DRAFT"
	StatePublished State = "PUBLISHED"
	StateRetired   State = "RETIRED"
)

// Valid reports whether s is one of the three states.
func (s State) Valid() bool {
	return s == StateDraft || s == StatePublished || s == StateRetired
}

// RetiredReason says why content is RETIRED.
type RetiredReason string

// The reasons.
const (
	// ReasonManual: the content manager retired PUBLISHED content.
	ReasonManual RetiredReason = "MANUAL"
	// ReasonDiscarded: the content manager discarded a DRAFT (no event).
	ReasonDiscarded RetiredReason = "DISCARDED"
	// ReasonDiscontinued: RetireOnDiscontinue, the network discontinued the item.
	ReasonDiscontinued RetiredReason = "DISCONTINUED"
)

// Valid reports whether r is one of the three reasons.
func (r RetiredReason) Valid() bool {
	return r == ReasonManual || r == ReasonDiscarded || r == ReasonDiscontinued
}

// Role is the role of a medium.
type Role string

// The roles.
const (
	RolePrimary Role = "PRIMARY"
	RoleGallery Role = "GALLERY"
)

// Valid reports whether r is PRIMARY or GALLERY.
func (r Role) Valid() bool { return r == RolePrimary || r == RoleGallery }

// ItemStatus is what the local CatalogItemView knows about the item; the
// caller (the use case) reads it from the view.
type ItemStatus int

// The item statuses. The zero value is ItemUnknown on purpose.
const (
	ItemUnknown ItemStatus = iota
	ItemActive
	ItemDiscontinued
)

// Categories answers whether a category is part of the current tree. The
// category.Tree aggregate implements it.
type Categories interface {
	Has(categoryID string) bool
}

// Attribute is one named fact of the item. Unit is optional ("" = none).
type Attribute struct {
	Name  string
	Value string
	Unit  string
}

// Media is a URL to a static asset (upload is out of scope).
type Media struct {
	MediaID string
	URL     string
	AltText string
	Role    Role
}

// Body is the editable part of the content: what Create and the revise
// commands replace as a whole.
type Body struct {
	Description       string
	Bullets           []string
	Attributes        []Attribute
	Media             []Media
	PrimaryCategoryID string
}

func (b Body) clone() Body {
	b.Bullets = append([]string{}, b.Bullets...)
	b.Attributes = append([]Attribute{}, b.Attributes...)
	b.Media = append([]Media{}, b.Media...)
	return b
}

// ProductContent is the aggregate root.
type ProductContent struct {
	id                ID
	state             State
	reason            RetiredReason
	body              Body
	revision          int
	publishedRevision int
	createdAt         time.Time
	updatedAt         time.Time
	publishedAt       time.Time
	retiredAt         time.Time
	version           int64
	loaded            int64
}

// Snapshot is the persisted form of the content. Restore validates it.
type Snapshot struct {
	ID                ID
	State             State
	RetiredReason     RetiredReason
	Body              Body
	Revision          int
	PublishedRevision int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PublishedAt       time.Time
	RetiredAt         time.Time
	Version           int64
}

// ID returns the catalogItemId.
func (c *ProductContent) ID() ID { return c.id }

// State returns the lifecycle state.
func (c *ProductContent) State() State { return c.state }

// RetiredReason returns why the content is RETIRED ("" otherwise).
func (c *ProductContent) RetiredReason() RetiredReason { return c.reason }

// Body returns a copy of the editable part.
func (c *ProductContent) Body() Body { return c.body.clone() }

// Revision returns the revision of the current shape: 1 at creation, plus one
// per revise. Publishing publishes the current revision.
func (c *ProductContent) Revision() int { return c.revision }

// PublishedRevision returns the revision of the last publication, 0 when the
// content was never published.
func (c *ProductContent) PublishedRevision() int { return c.publishedRevision }

// CreatedAt returns when the content was created.
func (c *ProductContent) CreatedAt() time.Time { return c.createdAt }

// UpdatedAt returns when the content last changed.
func (c *ProductContent) UpdatedAt() time.Time { return c.updatedAt }

// PublishedAt returns when the content was last published (zero before).
func (c *ProductContent) PublishedAt() time.Time { return c.publishedAt }

// RetiredAt returns when the content was retired (zero before).
func (c *ProductContent) RetiredAt() time.Time { return c.retiredAt }

// Version returns the aggregate version: it grows by one for every command
// that changed the content.
func (c *ProductContent) Version() int64 { return c.version }

// LoadedVersion returns the version the content had when it was loaded from
// the repository, 0 for content that was never stored. A repository saves
// under the rule "stored version == LoadedVersion".
func (c *ProductContent) LoadedVersion() int64 { return c.loaded }

// MarkPersisted records that the current version is stored. Repositories call
// it after a successful Save.
func (c *ProductContent) MarkPersisted() { c.loaded = c.version }

// Snapshot returns the persisted form.
func (c *ProductContent) Snapshot() Snapshot {
	return Snapshot{
		ID: c.id, State: c.state, RetiredReason: c.reason, Body: c.body.clone(), Revision: c.revision,
		PublishedRevision: c.publishedRevision, CreatedAt: c.createdAt, UpdatedAt: c.updatedAt,
		PublishedAt: c.publishedAt, RetiredAt: c.retiredAt, Version: c.version,
	}
}
