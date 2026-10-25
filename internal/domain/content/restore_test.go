package content

import (
	"reflect"
	"testing"

	"github.com/IQVO/product-content/internal/domain/shared"
)

func TestSnapshotRoundTrips(t *testing.T) {
	published := mustPublished(t)
	revised := mustPublished(t)
	nb := completeBody()
	nb.Description = "Edited"
	if err := revised.ReviseContent(nb, tree, t2); err != nil {
		t.Fatal(err)
	}
	discarded := mustCreate(t, emptyDoc)
	_ = discarded.DiscardDraft(t1)
	retired := mustPublished(t)
	_, _ = retired.Retire(t2)
	discontinuedDraft := mustCreate(t, completeBody())
	discontinuedDraft.RetireOnDiscontinue(t1)
	for name, c := range map[string]*ProductContent{
		"draft": mustCreate(t, completeBody()), "published": published, "revised": revised,
		"discarded": discarded, "retired": retired, "discontinued draft": discontinuedDraft,
	} {
		t.Run(name, func(t *testing.T) {
			snap := c.Snapshot()
			got, err := Restore(snap)
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if !reflect.DeepEqual(got.Snapshot(), snap) {
				t.Fatalf("round trip differs:\n got %+v\nwant %+v", got.Snapshot(), snap)
			}
			if got.LoadedVersion() != snap.Version || got.Version() != snap.Version {
				t.Fatalf("loaded %d version %d, want %d", got.LoadedVersion(), got.Version(), snap.Version)
			}
		})
	}
}

func TestRestoreCopiesTheSnapshot(t *testing.T) {
	snap := mustCreate(t, completeBody()).Snapshot()
	c, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	snap.Body.Bullets[0] = "changed"
	if c.Body().Bullets[0] != "Double-wall" {
		t.Fatal("Restore aliases the snapshot")
	}
}

func TestRestoreRejectsCorruptRows(t *testing.T) {
	pub := func() Snapshot { return mustPublished(t).Snapshot() }
	draft := func() Snapshot { return mustCreate(t, completeBody()).Snapshot() }
	retiredPub := func() Snapshot {
		c := mustPublished(t)
		_, _ = c.Retire(t2)
		return c.Snapshot()
	}
	tests := []struct {
		name string
		snap Snapshot
	}{
		{"bad id", with(draft(), func(s *Snapshot) { s.ID = "-x" })},
		{"bad state", with(draft(), func(s *Snapshot) { s.State = "NEW" })},
		{"no version", with(draft(), func(s *Snapshot) { s.Version = 0 })},
		{"no revision", with(draft(), func(s *Snapshot) { s.Revision = 0 })},
		{"no createdAt", with(draft(), func(s *Snapshot) { s.CreatedAt = zeroTime })},
		{"no updatedAt", with(draft(), func(s *Snapshot) { s.UpdatedAt = zeroTime })},
		{"too many bullets", with(draft(), func(s *Snapshot) { s.Body.Bullets = make([]string, MaxBullets+1) })},
		{"too many attributes", with(draft(), func(s *Snapshot) { s.Body.Attributes = make([]Attribute, MaxAttributes+1) })},
		{"too many media", with(draft(), func(s *Snapshot) { s.Body.Media = make([]Media, MaxMedia+1) })},
		{"bad field", with(draft(), func(s *Snapshot) { s.Body.Description = " x " })},
		{"published revision above revision", with(pub(), func(s *Snapshot) { s.PublishedRevision = s.Revision + 1 })},
		{"negative published revision", with(draft(), func(s *Snapshot) { s.PublishedRevision = -1 })},
		{"draft that was published", with(draft(), func(s *Snapshot) { s.PublishedRevision = 1; s.PublishedAt = t1 })},
		{"publishedAt without revision", with(draft(), func(s *Snapshot) { s.PublishedAt = t1 })},
		{"published revision without publishedAt", with(pub(), func(s *Snapshot) { s.PublishedAt = zeroTime })},
		{"published never published", with(pub(), func(s *Snapshot) { s.PublishedRevision = 0; s.PublishedAt = zeroTime })},
		{"published incomplete", with(pub(), func(s *Snapshot) { s.Body.Description = "" })},
		{"retired without reason", with(retiredPub(), func(s *Snapshot) { s.RetiredReason = "" })},
		{"retired without retiredAt", with(retiredPub(), func(s *Snapshot) { s.RetiredAt = zeroTime })},
		{"retired unknown reason", with(retiredPub(), func(s *Snapshot) { s.RetiredReason = "BORED" })},
		{"discarded after publication", with(retiredPub(), func(s *Snapshot) { s.RetiredReason = ReasonDiscarded })},
		{"reason on live content", with(pub(), func(s *Snapshot) { s.RetiredReason = ReasonManual })},
		{"retiredAt on live content", with(pub(), func(s *Snapshot) { s.RetiredAt = t2 })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Restore(tt.snap)
			wantErr(t, err, shared.ErrInvalidSnapshot)
			if c != nil {
				t.Fatal("a corrupt row returns no aggregate")
			}
		})
	}
}

func with(s Snapshot, f func(*Snapshot)) Snapshot { f(&s); return s }
