package content

import "github.com/IQVO/product-content/internal/domain/shared"

const entity = "productcontent"

// ProductContentPublished is raised by Publish. Its fields are the model's data
// list: the full content, so product-discovery never calls back, and the
// revision that orders it.
type ProductContentPublished struct {
	shared.Header
	CatalogItemID     ID
	Revision          int
	Description       string
	Bullets           []string
	Attributes        []Attribute
	Media             []Media
	PrimaryCategoryID string
}

// EventName returns "ProductContentPublished".
func (ProductContentPublished) EventName() string { return "ProductContentPublished" }

// Entity returns "productcontent".
func (ProductContentPublished) Entity() string { return entity }

// SubjectID returns the catalogItemId.
func (e ProductContentPublished) SubjectID() string { return string(e.CatalogItemID) }

// ProductContentRetired is raised by Retire and by RetireOnDiscontinue for
// PUBLISHED content.
type ProductContentRetired struct {
	shared.Header
	CatalogItemID ID
	Revision      int
}

// EventName returns "ProductContentRetired".
func (ProductContentRetired) EventName() string { return "ProductContentRetired" }

// Entity returns "productcontent".
func (ProductContentRetired) Entity() string { return entity }

// SubjectID returns the catalogItemId.
func (e ProductContentRetired) SubjectID() string { return string(e.CatalogItemID) }

var (
	_ shared.Event = ProductContentPublished{}
	_ shared.Event = ProductContentRetired{}
)
