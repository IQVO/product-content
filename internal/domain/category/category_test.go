package category

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/domain/shared"
)

var (
	t0 = time.Date(2026, 10, 20, 9, 0, 0, 123456000, time.UTC)
	t1 = t0.Add(time.Hour)
	t2 = t1.Add(time.Hour)
)

func wantErr(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

// build returns a tree with: home > kitchen > bottles, home > garden, outdoor.
func build(t *testing.T) *Tree {
	t.Helper()
	tr := NewTree()
	for _, c := range [][3]string{
		{"home", "", "Home"}, {"kitchen", "home", "Kitchen"}, {"bottles", "kitchen", "Bottles"},
		{"garden", "home", "Garden"}, {"outdoor", "", "Outdoor"},
	} {
		if _, _, err := tr.CreateCategory(c[0], c[1], c[2], t0); err != nil {
			t.Fatalf("CreateCategory(%v): %v", c, err)
		}
	}
	return tr
}

func paths(tr *Tree) map[string]string {
	out := map[string]string{}
	for _, c := range tr.Categories() {
		out[c.ID] = c.Path
	}
	return out
}

func TestEmptyTree(t *testing.T) {
	tr := NewTree()
	if tr.ID() != "main" || tr.Version() != 0 || tr.LoadedVersion() != 0 || len(tr.Categories()) != 0 || tr.Has("x") || !tr.UpdatedAt().IsZero() {
		t.Fatalf("empty tree = %+v", tr)
	}
	if _, ok := tr.Get("x"); ok {
		t.Fatal("Get on an empty tree finds nothing")
	}
}

func TestCreateCategoryPublishesANewVersion(t *testing.T) {
	tr := NewTree()
	c, events, err := tr.CreateCategory("home", "", "Home", t0.Add(789*time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "category", c, Category{ID: "home", Name: "Home", Path: "Home"})
	eq(t, "version", tr.Version(), int64(1))
	eq(t, "updatedAt", tr.UpdatedAt(), t0)
	c2, events2, err := tr.CreateCategory("kitchen", "home", "Kitchen", t1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "path", c2.Path, "Home/Kitchen")
	eq(t, "parent", c2.ParentID, "home")
	eq(t, "version", tr.Version(), int64(2))
	if len(events) != 1 || len(events2) != 1 {
		t.Fatalf("events = %v %v, want one each", events, events2)
	}
	e := events2[0].(CategoryTreePublished)
	want := []Category{{ID: "home", Name: "Home", Path: "Home"}, {ID: "kitchen", ParentID: "home", Name: "Kitchen", Path: "Home/Kitchen"}}
	eq(t, "event version", e.AggregateVersion(), int64(2))
	eq(t, "event time", e.OccurredAt(), t1)
	eq(t, "event tree", e.TreeID, "main")
	eq(t, "event subject", e.SubjectID(), "main")
	eq(t, "event name", e.EventName(), "CategoryTreePublished")
	eq(t, "event entity", e.Entity(), "categorytree")
	if !reflect.DeepEqual(e.Categories, want) {
		t.Fatalf("event categories = %+v", e.Categories)
	}
	if first := events[0].(CategoryTreePublished); len(first.Categories) != 1 {
		t.Fatalf("an earlier event changed: %+v", first)
	}
}

func TestCreateCategoryRefusals(t *testing.T) {
	deep := build(t)
	if _, _, err := deep.CreateCategory("pets", "bottles", "Pets", t1); err != nil {
		t.Fatalf("level 4 is allowed: %v", err)
	}
	tests := []struct {
		name         string
		id, parent   string
		catName      string
		want         error
		tree         *Tree
		wantVersion  int64
		wantCategory int
	}{
		{"invalid id", "-x", "", "A", shared.ErrValidation, build(t), 5, 5},
		{"existing id", "home", "", "A", shared.ErrValidation, build(t), 5, 5},
		{"blank name", "a", "", "", shared.ErrValidation, build(t), 5, 5},
		{"padded name", "a", "", " A", shared.ErrValidation, build(t), 5, 5},
		{"slash in name", "a", "", "A/B", shared.ErrValidation, build(t), 5, 5},
		{"name too long", "a", "", strings.Repeat("x", MaxName+1), shared.ErrValidation, build(t), 5, 5},
		{"unknown parent", "a", "nope", "A", ErrUnknownParent, build(t), 5, 5},
		{"fifth level", "a", "pets", "A", ErrMaxDepth, deep, 6, 6},
		{"duplicate sibling", "a", "home", "kitchen", ErrDuplicateSiblingName, build(t), 5, 5},
		{"duplicate root", "a", "", "HOME", ErrDuplicateSiblingName, build(t), 5, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, events, err := tt.tree.CreateCategory(tt.id, tt.parent, tt.catName, t2)
			wantErr(t, err, tt.want)
			if c != (Category{}) || len(events) != 0 || tt.tree.Version() != tt.wantVersion || len(tt.tree.Categories()) != tt.wantCategory {
				t.Fatalf("a refused create changed the tree: %+v %v version %d", c, events, tt.tree.Version())
			}
		})
	}
}

func TestSameNameUnderDifferentParentsIsFine(t *testing.T) {
	tr := build(t)
	if _, _, err := tr.CreateCategory("gk", "garden", "Kitchen", t1); err != nil {
		t.Fatalf("a sibling name is only unique per parent: %v", err)
	}
	if got := paths(tr)["gk"]; got != "Home/Garden/Kitchen" {
		t.Fatalf("path = %q", got)
	}
}

func TestNameLimitIsInclusive(t *testing.T) {
	tr := NewTree()
	if _, _, err := tr.CreateCategory("a", "", strings.Repeat("x", MaxName), t0); err != nil {
		t.Fatalf("a name of %d characters is valid: %v", MaxName, err)
	}
}

func TestTreeIsBounded(t *testing.T) {
	cats := make([]Category, 0, MaxCategories)
	for i := 0; i < MaxCategories; i++ {
		id := fmt.Sprintf("c%d", i)
		cats = append(cats, Category{ID: id, Name: id, Path: id})
	}
	tr, err := Restore(Snapshot{Categories: cats, Version: 1, UpdatedAt: t0})
	if err != nil {
		t.Fatalf("a full tree restores: %v", err)
	}
	_, _, err = tr.CreateCategory("one-more", "", "One more", t1)
	wantErr(t, err, shared.ErrValidation)
	if !errors.Is(err, ErrTooManyCategories) || tr.Version() != 1 {
		t.Fatalf("err = %v version %d", err, tr.Version())
	}
}

func TestMoveCategoryRecomputesTheSubtree(t *testing.T) {
	tr := build(t)
	c, events, err := tr.MoveCategory("kitchen", "outdoor", t1)
	if err != nil {
		t.Fatal(err)
	}
	if c.ParentID != "outdoor" || c.Path != "Outdoor/Kitchen" || tr.Version() != 6 || len(events) != 1 {
		t.Fatalf("category %+v version %d events %v", c, tr.Version(), events)
	}
	if got := paths(tr)["bottles"]; got != "Outdoor/Kitchen/Bottles" {
		t.Fatalf("a descendant keeps its old path: %q", got)
	}
	if e := events[0].(CategoryTreePublished); e.AggregateVersion() != 6 || len(e.Categories) != 5 || !e.OccurredAt().Equal(t1) {
		t.Fatalf("event = %+v", e)
	}
}

func TestMoveToTheRoot(t *testing.T) {
	tr := build(t)
	c, _, err := tr.MoveCategory("kitchen", "", t1)
	if err != nil || c.ParentID != "" || c.Path != "Kitchen" || paths(tr)["bottles"] != "Kitchen/Bottles" {
		t.Fatalf("move to root: %+v %v", c, err)
	}
}

func TestMoveToTheSameParentStillPublishes(t *testing.T) {
	tr := build(t)
	c, events, err := tr.MoveCategory("kitchen", "home", t1)
	if err != nil || c.Path != "Home/Kitchen" || len(events) != 1 || tr.Version() != 6 {
		t.Fatalf("move to the same parent: %+v %v %v", c, events, err)
	}
}

func TestMoveCategoryDepthBoundary(t *testing.T) {
	tr := build(t) // home(1) > kitchen(2) > bottles(3); outdoor(1)
	// kitchen with bottles has height 2: under garden (level 2) it spans 3..4, allowed.
	if _, _, err := tr.MoveCategory("kitchen", "garden", t1); err != nil {
		t.Fatalf("a subtree ending at level 4 is allowed: %v", err)
	}
	// outdoor (height 1) under bottles (level 4 now) would be level 5.
	_, events, err := tr.MoveCategory("outdoor", "bottles", t2)
	wantErr(t, err, ErrMaxDepth)
	if len(events) != 0 {
		t.Fatal("a refused move raises no event")
	}
	// home (height 4 now: home>garden>kitchen>bottles) under outdoor (level 1) spans 2..5.
	_, _, err = tr.MoveCategory("home", "outdoor", t2)
	wantErr(t, err, ErrMaxDepth)
}

func TestMoveCategoryRefusals(t *testing.T) {
	tests := []struct {
		name       string
		id, parent string
		want       error
	}{
		{"unknown category", "nope", "home", ErrCategoryNotFound},
		{"unknown parent", "kitchen", "nope", ErrUnknownParent},
		{"under itself", "home", "home", ErrCategoryCycle},
		{"under a child", "home", "kitchen", ErrCategoryCycle},
		{"under a grandchild", "home", "bottles", ErrCategoryCycle},
		{"duplicate sibling", "kitchen", "", ErrDuplicateSiblingName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := build(t)
			if tt.name == "duplicate sibling" {
				if _, _, err := tr.CreateCategory("k2", "", "kitchen", t1); err != nil {
					t.Fatal(err)
				}
			}
			ver, before := tr.Version(), paths(tr)
			c, events, err := tr.MoveCategory(tt.id, tt.parent, t2)
			wantErr(t, err, tt.want)
			if c != (Category{}) || len(events) != 0 || tr.Version() != ver || !reflect.DeepEqual(paths(tr), before) {
				t.Fatal("a refused move changed the tree")
			}
		})
	}
}

func TestRenameCategoryRecomputesTheSubtree(t *testing.T) {
	tr := build(t)
	c, events, err := tr.RenameCategory("kitchen", "Cooking", t1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Cooking" || c.Path != "Home/Cooking" || tr.Version() != 6 || len(events) != 1 {
		t.Fatalf("category %+v version %d", c, tr.Version())
	}
	if paths(tr)["bottles"] != "Home/Cooking/Bottles" {
		t.Fatalf("descendant path = %q", paths(tr)["bottles"])
	}
}

func TestRenameToItsOwnNameIsAllowed(t *testing.T) {
	tr := build(t)
	if _, _, err := tr.RenameCategory("kitchen", "KITCHEN", t1); err != nil {
		t.Fatalf("a category may change the case of its own name: %v", err)
	}
}

func TestRenameCategoryRefusals(t *testing.T) {
	tests := []struct {
		name, id, newName string
		want              error
	}{
		{"unknown", "nope", "X", ErrCategoryNotFound},
		{"blank", "kitchen", "", shared.ErrValidation},
		{"slash", "kitchen", "A/B", shared.ErrValidation},
		{"duplicate sibling", "kitchen", "garden", ErrDuplicateSiblingName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := build(t)
			c, events, err := tr.RenameCategory(tt.id, tt.newName, t2)
			wantErr(t, err, tt.want)
			if c != (Category{}) || len(events) != 0 || tr.Version() != 5 || paths(tr)["kitchen"] != "Home/Kitchen" {
				t.Fatal("a refused rename changed the tree")
			}
		})
	}
}

func TestPublishCategoryTreePublishesAgain(t *testing.T) {
	tr := build(t)
	events := tr.PublishCategoryTree(t2)
	if len(events) != 1 || tr.Version() != 6 || !tr.UpdatedAt().Equal(t2) {
		t.Fatalf("events %v version %d", events, tr.Version())
	}
	e := events[0].(CategoryTreePublished)
	if e.AggregateVersion() != 6 || len(e.Categories) != 5 {
		t.Fatalf("event = %+v", e)
	}
	if empty := NewTree().PublishCategoryTree(t0); len(empty) != 1 || empty[0].(CategoryTreePublished).AggregateVersion() != 1 {
		t.Fatalf("an empty tree publishes version 1 with no categories: %v", empty)
	}
}

func TestCategoriesAreOrderedByPathThenID(t *testing.T) {
	tr := NewTree()
	for _, c := range [][3]string{{"b", "", "Same"}, {"zz", "", "Alpha"}, {"a", "zz", "Child"}} {
		_, _, _ = tr.CreateCategory(c[0], c[1], c[2], t0)
	}
	var got []string
	for _, c := range tr.Categories() {
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, []string{"zz", "a", "b"}) {
		t.Fatalf("order = %v, want parents before children, by path", got)
	}
	// Same path (case-insensitive duplicates cannot exist), so id breaks no tie here; check Compare on ids directly.
	cs := []Category{{ID: "b", Path: "P"}, {ID: "a", Path: "P"}}
	sortCategories(cs)
	if cs[0].ID != "a" {
		t.Fatalf("ids break path ties: %v", cs)
	}
}

func TestGetAndHas(t *testing.T) {
	tr := build(t)
	c, ok := tr.Get("bottles")
	if !ok || c.Path != "Home/Kitchen/Bottles" || !tr.Has("bottles") || tr.Has("nope") {
		t.Fatalf("Get = %+v %v", c, ok)
	}
	tr.MarkPersisted()
	if tr.LoadedVersion() != 5 {
		t.Fatalf("loaded = %d", tr.LoadedVersion())
	}
}

func TestSnapshotRoundTrips(t *testing.T) {
	tr := build(t)
	snap := tr.Snapshot()
	got, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Snapshot(), snap) || got.LoadedVersion() != 5 || got.Version() != 5 {
		t.Fatalf("round trip differs: %+v vs %+v", got.Snapshot(), snap)
	}
	snap.Categories[0].Name = "changed"
	if got.Snapshot().Categories[0].Name == "changed" {
		t.Fatal("Restore aliases the snapshot")
	}
}

func TestRestoreRejectsCorruptRows(t *testing.T) {
	good := func() Snapshot { return build(t).Snapshot() }
	idx := func(s Snapshot, id string) int {
		for i, c := range s.Categories {
			if c.ID == id {
				return i
			}
		}
		t.Fatalf("no %s", id)
		return -1
	}
	mut := func(f func(*Snapshot)) Snapshot { s := good(); f(&s); return s }
	tests := []struct {
		name string
		snap Snapshot
	}{
		{"no version", mut(func(s *Snapshot) { s.Version = 0 })},
		{"no updatedAt", mut(func(s *Snapshot) { s.UpdatedAt = time.Time{} })},
		{"bad id", mut(func(s *Snapshot) { s.Categories[0].ID = "-x" })},
		{"duplicate id", mut(func(s *Snapshot) { s.Categories[1].ID = s.Categories[0].ID })},
		{"bad name", mut(func(s *Snapshot) { s.Categories[0].Name = "" })},
		{"unknown parent", mut(func(s *Snapshot) { s.Categories[idx(*s, "kitchen")].ParentID = "nope" })},
		{"self parent", mut(func(s *Snapshot) { s.Categories[idx(*s, "home")].ParentID = "home" })},
		{"cycle", mut(func(s *Snapshot) {
			s.Categories[idx(*s, "home")].ParentID = "bottles"
			s.Categories[idx(*s, "home")].Path = "x"
		})},
		{"too deep", mut(func(s *Snapshot) {
			s.Categories = append(s.Categories, Category{ID: "l4", ParentID: "bottles", Name: "L4", Path: "Home/Kitchen/Bottles/L4"},
				Category{ID: "l5", ParentID: "l4", Name: "L5", Path: "Home/Kitchen/Bottles/L4/L5"})
		})},
		{"duplicate siblings", mut(func(s *Snapshot) {
			s.Categories[idx(*s, "garden")].Name = "KITCHEN"
			s.Categories[idx(*s, "garden")].Path = "Home/KITCHEN"
		})},
		{"wrong path", mut(func(s *Snapshot) { s.Categories[idx(*s, "bottles")].Path = "Bottles" })},
		{"too many", mut(func(s *Snapshot) { s.Categories = make([]Category, MaxCategories+1) })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := Restore(tt.snap)
			wantErr(t, err, shared.ErrInvalidSnapshot)
			if tr != nil {
				t.Fatal("a corrupt row returns no aggregate")
			}
		})
	}
}

func TestDepthAndHeightHelpers(t *testing.T) {
	tr := build(t)
	if tr.depth("") != 0 || tr.depth("home") != 1 || tr.depth("bottles") != 3 || tr.depth("nope") != 0 {
		t.Fatal("depth")
	}
	if tr.height("bottles") != 1 || tr.height("kitchen") != 2 || tr.height("home") != 3 || tr.height("outdoor") != 1 {
		t.Fatal("height")
	}
	if !tr.inSubtree("home", "bottles") || !tr.inSubtree("home", "home") || tr.inSubtree("kitchen", "garden") || tr.inSubtree("home", "") || tr.inSubtree("home", "nope") {
		t.Fatal("inSubtree")
	}
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}
