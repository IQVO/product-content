// Package category is the CategoryTree aggregate of the product-content bounded
// context: the single, versioned tree of categories a shopper browses.
//
// The tree is ONE live aggregate with no draft state (model-gap decision 8):
// every command applies to the live tree and publishes a new version, so the
// aggregate version and the published tree version are the same number.
// Categories are never deleted, only created, moved and renamed
// (ErrCategoryInUse guards a future removal, model-gap decision 4).
//
// The package imports only the shared domain package and the standard library.
package category

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/IQVO/product-content/internal/domain/shared"
)

// Domain errors, one per Err of the model plus the two lookups the commands need.
var (
	// ErrCategoryCycle: a node never moves under its own subtree.
	ErrCategoryCycle = errors.New("category cycle")
	// ErrMaxDepth: the tree is at most MaxDepth levels deep.
	ErrMaxDepth = errors.New("max depth")
	// ErrDuplicateSiblingName: siblings never share a name.
	ErrDuplicateSiblingName = errors.New("duplicate sibling name")
	// ErrCategoryInUse: a category in use by PUBLISHED content is never
	// removed. Categories are never deleted today (no remove command by
	// design), so nothing raises it yet; it names the guard of a future removal.
	ErrCategoryInUse = errors.New("category in use")
	// ErrCategoryNotFound: the category named by the command is not in the tree.
	ErrCategoryNotFound = errors.New("category not found")
	// ErrUnknownParent: the parent named by the command is not in the tree.
	ErrUnknownParent = errors.New("unknown parent category")
)

// Limits.
const (
	// MaxDepth is the deepest level of the tree; a root category is level 1.
	MaxDepth = 4
	// MaxName is the longest category name in characters.
	MaxName = 100
	// MaxCategories bounds the tree (the model sizes it at ~10^3 categories).
	MaxCategories = 5000
	// TreeID is the id of the one tree.
	TreeID  = "main"
	pathSep = "/"
)

// ErrTooManyCategories: the tree reached MaxCategories.
var ErrTooManyCategories = shared.Invalid("the tree holds at most %d categories", MaxCategories)

// Category is one node of the tree. ParentID is "" for a root category. Path is
// derived: the names from the root joined with "/".
type Category struct {
	ID       string
	ParentID string
	Name     string
	Path     string
}

// Tree is the aggregate root.
type Tree struct {
	cats      []Category
	version   int64
	loaded    int64
	updatedAt time.Time
}

// Snapshot is the persisted form of the tree. Restore validates it.
type Snapshot struct {
	Categories []Category
	Version    int64
	UpdatedAt  time.Time
}

// NewTree returns the empty tree that exists before the first category: version
// 0, never stored.
func NewTree() *Tree { return &Tree{} }

// ID returns TreeID.
func (t *Tree) ID() string { return TreeID }

// Version returns the tree version: it grows by one for every command, and every
// command publishes it.
func (t *Tree) Version() int64 { return t.version }

// LoadedVersion returns the version the tree had when it was loaded, 0 for the
// tree that was never stored.
func (t *Tree) LoadedVersion() int64 { return t.loaded }

// MarkPersisted records that the current version is stored.
func (t *Tree) MarkPersisted() { t.loaded = t.version }

// UpdatedAt returns when the tree last changed (zero for the empty tree).
func (t *Tree) UpdatedAt() time.Time { return t.updatedAt }

// Has reports whether the category is part of the tree (content.Categories).
func (t *Tree) Has(categoryID string) bool { return t.index(categoryID) >= 0 }

// Get returns the category.
func (t *Tree) Get(categoryID string) (Category, bool) {
	i := t.index(categoryID)
	if i < 0 {
		return Category{}, false
	}
	return t.cats[i], true
}

// Categories returns the categories ordered by path, then id: parents before
// their children.
func (t *Tree) Categories() []Category {
	out := slices.Clone(t.cats)
	sortCategories(out)
	return out
}

func sortCategories(cs []Category) {
	slices.SortFunc(cs, func(a, b Category) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// Snapshot returns the persisted form.
func (t *Tree) Snapshot() Snapshot {
	return Snapshot{Categories: t.Categories(), Version: t.version, UpdatedAt: t.updatedAt}
}

func (t *Tree) index(id string) int {
	return slices.IndexFunc(t.cats, func(c Category) bool { return c.ID == id })
}

// depth returns the level of the category: 1 for a root. 0 for a root's
// non-existent parent "".
func (t *Tree) depth(id string) int {
	d := 0
	for id != "" {
		i := t.index(id)
		if i < 0 {
			return d
		}
		d++
		id = t.cats[i].ParentID
	}
	return d
}

// height returns the levels of the subtree rooted at id: 1 for a leaf.
func (t *Tree) height(id string) int {
	h := 0
	for _, c := range t.cats {
		if c.ParentID == id {
			h = max(h, t.height(c.ID))
		}
	}
	return h + 1
}

// inSubtree reports whether id is root or below it.
func (t *Tree) inSubtree(root, id string) bool {
	for id != "" {
		if id == root {
			return true
		}
		i := t.index(id)
		if i < 0 {
			return false
		}
		id = t.cats[i].ParentID
	}
	return false
}

func (t *Tree) siblingNamed(parentID, name, except string) bool {
	return slices.ContainsFunc(t.cats, func(c Category) bool {
		return c.ParentID == parentID && c.ID != except && strings.EqualFold(c.Name, name)
	})
}

func validName(name string) bool {
	return shared.ValidTextN(name, MaxName) && !strings.Contains(name, pathSep)
}

// recompute rebuilds every path from the names and parents.
func (t *Tree) recompute() {
	for i := range t.cats {
		t.cats[i].Path = t.pathOf(t.cats[i].ID)
	}
}

func (t *Tree) pathOf(id string) string {
	var names []string
	for id != "" {
		i := t.index(id)
		names = append(names, t.cats[i].Name)
		id = t.cats[i].ParentID
	}
	slices.Reverse(names)
	return strings.Join(names, pathSep)
}

// publish stamps the change and raises CategoryTreePublished for the new version.
func (t *Tree) publish(now time.Time) []shared.Event {
	t.version++
	t.updatedAt = shared.UTC(now)
	return []shared.Event{CategoryTreePublished{
		Header:     shared.Header{Version: t.version, At: t.updatedAt},
		TreeID:     TreeID,
		Categories: t.Categories(),
	}}
}

// CreateCategory adds the category under parentID ("" = a root category). The id
// is minted by the caller.
func (t *Tree) CreateCategory(id, parentID, name string, now time.Time) (Category, []shared.Event, error) {
	if !shared.ValidID(id) || t.Has(id) {
		return Category{}, nil, shared.Invalid("categoryId must be a new valid id")
	}
	if !validName(name) {
		return Category{}, nil, shared.Invalid("name must be trimmed text of at most %d characters without %q", MaxName, pathSep)
	}
	if parentID != "" && !t.Has(parentID) {
		return Category{}, nil, ErrUnknownParent
	}
	if len(t.cats) >= MaxCategories {
		return Category{}, nil, ErrTooManyCategories
	}
	if t.depth(parentID)+1 > MaxDepth {
		return Category{}, nil, ErrMaxDepth
	}
	if t.siblingNamed(parentID, name, "") {
		return Category{}, nil, ErrDuplicateSiblingName
	}
	t.cats = append(t.cats, Category{ID: id, ParentID: parentID, Name: name})
	t.recompute()
	events := t.publish(now)
	c, _ := t.Get(id)
	return c, events, nil
}

// MoveCategory moves the category, with its subtree, under parentID ("" = to
// the root).
func (t *Tree) MoveCategory(id, parentID string, now time.Time) (Category, []shared.Event, error) {
	i := t.index(id)
	if i < 0 {
		return Category{}, nil, ErrCategoryNotFound
	}
	if parentID != "" && !t.Has(parentID) {
		return Category{}, nil, ErrUnknownParent
	}
	if t.inSubtree(id, parentID) {
		return Category{}, nil, ErrCategoryCycle
	}
	if t.depth(parentID)+t.height(id) > MaxDepth {
		return Category{}, nil, ErrMaxDepth
	}
	if t.siblingNamed(parentID, t.cats[i].Name, id) {
		return Category{}, nil, ErrDuplicateSiblingName
	}
	t.cats[i].ParentID = parentID
	t.recompute()
	events := t.publish(now)
	c, _ := t.Get(id)
	return c, events, nil
}

// RenameCategory renames the category; the paths of its subtree follow.
func (t *Tree) RenameCategory(id, name string, now time.Time) (Category, []shared.Event, error) {
	i := t.index(id)
	if i < 0 {
		return Category{}, nil, ErrCategoryNotFound
	}
	if !validName(name) {
		return Category{}, nil, shared.Invalid("name must be trimmed text of at most %d characters without %q", MaxName, pathSep)
	}
	if t.siblingNamed(t.cats[i].ParentID, name, id) {
		return Category{}, nil, ErrDuplicateSiblingName
	}
	t.cats[i].Name = name
	t.recompute()
	events := t.publish(now)
	c, _ := t.Get(id)
	return c, events, nil
}

// PublishCategoryTree publishes the live tree again under a new version
// (PUBLISHED -> PUBLISHED): the way to re-send the tree to a consumer.
func (t *Tree) PublishCategoryTree(now time.Time) []shared.Event {
	return t.publish(now)
}
