// Package ids is the production ports.IDGenerator: a category id is "cat-" and
// 12 random hexadecimal characters.
package ids

import (
	"strings"

	"github.com/google/uuid"

	"github.com/IQVO/product-content/internal/application/ports"
)

// Generator mints ids.
type Generator struct{}

var _ ports.IDGenerator = Generator{}

// NewCategoryID returns "cat-" followed by 12 random hexadecimal characters.
func (Generator) NewCategoryID() string {
	return "cat-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}
