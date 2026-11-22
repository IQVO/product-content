package http_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
)

// Every refusal a keyed handler writes goes through writeError, the writer that
// marks the capture with the class of its problem; a handler that wrote a
// problem by hand would bypass the class (and be released as unclassified).
// rejectNUL is outside the middleware (it answers before any key is recorded),
// so its writeProblem is not a bypass.
func TestHandlersAnswerErrorsOnlyThroughWriteError(t *testing.T) {
	for _, name := range []string{"handlers.go", "handlers_category.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"writeProblem(", "writeBody(", "WriteHeader(", "problem+json"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s contains %q: answer an error with writeError(w, r, err) so its replay class marks the capture", name, banned)
			}
		}
	}
}

var adrClassRow = regexp.MustCompile(`^\| ` + "`([a-z-]+)`" + ` \| (FINAL|OUTCOME|RETRYABLE) \|`)

// ADR 0009 carries the class of every problem type with its reason; the table
// and the catalogue must say the same, in both directions. Two catalogue
// entries can share one slug (not-found, unknown-category); a shared slug must
// declare one class, so collapse to a map first.
func TestTheADRDeclaresTheClassOfEveryProblem(t *testing.T) {
	paths, err := filepath.Glob("../../../../docs/adr/0009-*.md")
	if err != nil || len(paths) != 1 {
		t.Fatalf("ADR 0009 not found (%v, %v)", paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	inADR := map[string]string{}
	for _, line := range regexp.MustCompile(`\r?\n`).Split(string(raw), -1) {
		if m := adrClassRow.FindStringSubmatch(line); m != nil {
			inADR[m[1]] = m[2]
		}
	}
	inCatalogue := map[string]string{}
	for _, row := range httpadapter.Catalogue() {
		if was, ok := inCatalogue[row.Slug]; ok && was != row.Class {
			t.Errorf("problem type %q declares both %q and %q: one class per slug", row.Slug, was, row.Class)
		}
		inCatalogue[row.Slug] = row.Class
	}
	for slug, class := range inCatalogue {
		if got := inADR[slug]; got != class {
			t.Errorf("problem type %q: the ADR says %q, the catalogue %q", slug, got, class)
		}
		delete(inADR, slug)
	}
	for slug, class := range inADR {
		t.Errorf("the ADR classifies %q as %s but the catalogue has no such problem type", slug, class)
	}
}
