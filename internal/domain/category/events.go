package category

import "github.com/IQVO/product-content/internal/domain/shared"

// CategoryTreePublished is raised by every command of the tree. Its fields are
// the model's data list: the version and the whole tree.
type CategoryTreePublished struct {
	shared.Header
	TreeID     string
	Categories []Category
}

// EventName returns "CategoryTreePublished".
func (CategoryTreePublished) EventName() string { return "CategoryTreePublished" }

// Entity returns "categorytree".
func (CategoryTreePublished) Entity() string { return "categorytree" }

// SubjectID returns the categoryTreeId.
func (e CategoryTreePublished) SubjectID() string { return e.TreeID }

var _ shared.Event = CategoryTreePublished{}
