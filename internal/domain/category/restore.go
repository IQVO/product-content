package category

import (
	"errors"
	"fmt"
	"slices"

	"github.com/IQVO/product-content/internal/domain/shared"
)

// Restore rebuilds the persisted tree, validating every invariant a stored row
// must keep: unique ids, existing parents, no cycle, depth, unique sibling names
// and paths that match the names. A violation is shared.ErrInvalidSnapshot.
func Restore(s Snapshot) (*Tree, error) {
	t := &Tree{cats: slices.Clone(s.Categories), version: s.Version, loaded: s.Version, updatedAt: shared.UTC(s.UpdatedAt)}
	if err := checkSnapshot(s, t); err != nil {
		return nil, shared.Corrupt("category tree: %v", err)
	}
	return t, nil
}

func checkSnapshot(s Snapshot, t *Tree) error {
	if s.Version < 1 || s.UpdatedAt.IsZero() {
		return errors.New("version and updatedAt are required")
	}
	if len(s.Categories) > MaxCategories {
		return errors.New("the tree exceeds its bound")
	}
	seen := map[string]bool{}
	for _, c := range s.Categories {
		if !shared.ValidID(c.ID) || seen[c.ID] || !validName(c.Name) {
			return fmt.Errorf("category %q needs a unique valid id and a valid name", c.ID)
		}
		seen[c.ID] = true
	}
	for _, c := range s.Categories {
		if err := checkAncestry(t, c); err != nil {
			return err
		}
	}
	for _, c := range s.Categories {
		if err := checkNode(t, c); err != nil {
			return err
		}
	}
	return nil
}

// checkAncestry walks up from c; meeting a node twice means the parents form a
// cycle. It runs for every node before any check that follows parent links, so
// those can not loop.
func checkAncestry(t *Tree, c Category) error {
	seen := map[string]bool{c.ID: true}
	for id := c.ParentID; id != ""; id = t.cats[t.index(id)].ParentID {
		if t.index(id) < 0 {
			return fmt.Errorf("category %q has an unknown parent", c.ID)
		}
		if seen[id] {
			return fmt.Errorf("category %q is its own ancestor", c.ID)
		}
		seen[id] = true
	}
	return nil
}

func checkNode(t *Tree, c Category) error {
	if t.depth(c.ID) > MaxDepth {
		return fmt.Errorf("category %q is deeper than %d levels", c.ID, MaxDepth)
	}
	if t.siblingNamed(c.ParentID, c.Name, c.ID) {
		return fmt.Errorf("category %q shares its name with a sibling", c.ID)
	}
	if c.Path != t.pathOf(c.ID) {
		return fmt.Errorf("category %q has the path %q, want %q", c.ID, c.Path, t.pathOf(c.ID))
	}
	return nil
}
