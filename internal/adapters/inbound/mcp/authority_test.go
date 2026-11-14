package mcp

import "testing"

// The declaration helper is the fleet's pinned shape: one Meta entry under
// com.warehouse/authority carrying the level. Every rung of the ladder is
// exercised here, though only L1 is used by a tool of this read-only surface.
func TestAuthorityMetaDeclaresEveryLevelUnderTheFleetKey(t *testing.T) {
	for _, level := range []string{authorityL1, authorityL2, authorityL3, authorityL4} {
		meta := authorityMeta(level)
		if len(meta) != 1 || meta[authorityMetaKey] != level {
			t.Errorf("authorityMeta(%q) = %v, want exactly {%s: %q}", level, meta, authorityMetaKey, level)
		}
	}
	if authorityMetaKey != "com.warehouse/authority" {
		t.Errorf("authorityMetaKey = %q, want the fleet wire key com.warehouse/authority", authorityMetaKey)
	}
}
