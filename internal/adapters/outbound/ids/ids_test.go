package ids

import (
	"regexp"
	"testing"

	"github.com/IQVO/product-content/internal/domain/shared"
)

func TestNewCategoryIDIsAValidUniqueID(t *testing.T) {
	rx := regexp.MustCompile(`^cat-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := Generator{}.NewCategoryID()
		if !rx.MatchString(id) || !shared.ValidID(id) || seen[id] {
			t.Fatalf("id %q is malformed or repeated", id)
		}
		seen[id] = true
	}
}
