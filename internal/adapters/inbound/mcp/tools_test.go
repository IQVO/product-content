package mcp_test

import (
	"strings"
	"testing"
)

// get_product_content answers what GET /product-contents/{catalogItemId}
// answers: a PUBLISHED content with a revision waiting for publication.
func TestGetProductContentReturnsPublishedContent(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "get_product_content", map[string]any{"catalogItemId": w.publishedID})
	for k, v := range map[string]any{
		"catalogItemId": w.publishedID, "state": "PUBLISHED", "revision": 2.0, "publishedRevision": 1.0,
		"primaryCategoryId": w.categoryID, "description": "A 2-litre insulated steel bottle, now in matte.",
	} {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, k := range []string{"createdAt", "updatedAt", "publishedAt"} {
		if _, ok := got[k].(string); !ok {
			t.Errorf("%s = %v, want an RFC 3339 instant", k, got[k])
		}
	}
	for _, k := range []string{"retiredAt", "retiredReason"} {
		if _, has := got[k]; has {
			t.Errorf("%s must be omitted on PUBLISHED content: %v", k, got)
		}
	}
	if bullets, _ := got["bullets"].([]any); len(bullets) != 1 || bullets[0] != "Double-wall" {
		t.Errorf("bullets = %v", got["bullets"])
	}
	media, _ := got["media"].([]any)
	if len(media) != 1 || media[0].(map[string]any)["role"] != "PRIMARY" || media[0].(map[string]any)["mediaId"] != "med-1" {
		t.Errorf("media = %v", got["media"])
	}
}

// A DRAFT carries its attributes; a unit is present only when there is one.
func TestGetProductContentReturnsDraftWithAttributes(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "get_product_content", map[string]any{"catalogItemId": w.draftID})
	if got["state"] != "DRAFT" || got["revision"] != 1.0 || got["publishedRevision"] != 0.0 {
		t.Fatalf("draft = %v", got)
	}
	if _, has := got["publishedAt"]; has {
		t.Errorf("publishedAt must be omitted on a draft: %v", got)
	}
	attrs, _ := got["attributes"].([]any)
	if len(attrs) != 2 {
		t.Fatalf("attributes = %v, want two", got["attributes"])
	}
	withUnit, _ := attrs[0].(map[string]any)
	noUnit, _ := attrs[1].(map[string]any)
	if withUnit["name"] != "capacity" || withUnit["unit"] != "l" {
		t.Errorf("first attribute = %v", withUnit)
	}
	if _, has := noUnit["unit"]; has {
		t.Errorf("an attribute without a unit must omit it: %v", noUnit)
	}
}

// Like the REST GET, RETIRED content is readable and says why it is retired.
func TestGetProductContentReturnsRetiredContent(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "get_product_content", map[string]any{"catalogItemId": w.retiredID})
	if got["state"] != "RETIRED" || got["retiredReason"] != "DISCARDED" {
		t.Fatalf("retired = %v", got)
	}
	if _, ok := got["retiredAt"].(string); !ok {
		t.Errorf("retiredAt = %v, want an RFC 3339 instant", got["retiredAt"])
	}
}

// Content with no body fields still answers empty arrays, never null.
func TestGetProductContentArraysAreNeverNull(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "get_product_content", map[string]any{"catalogItemId": w.draftID})
	for _, k := range []string{"bullets", "attributes", "media"} {
		if _, ok := got[k].([]any); !ok {
			t.Errorf("%s = %#v, want an array", k, got[k])
		}
	}
}

func TestGetProductContentErrors(t *testing.T) {
	h, _ := withWorld(t)
	failWith(t, h.session, "get_product_content", map[string]any{"catalogItemId": "item-nobody"}, "not-found")
	// An id that can never name a resource never reaches the repository.
	failWith(t, h.session, "get_product_content", map[string]any{"catalogItemId": strings.Repeat("x", 129)}, "not-found")
	failWith(t, h.session, "get_product_content", map[string]any{"catalogItemId": "bad\x00id"}, "not-found")
	failWith(t, h.session, "get_product_content", map[string]any{"catalogItemId": "   "}, "invalid-argument")
}

func TestListCategoriesReturnsTheTree(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "list_categories", map[string]any{})
	if got["version"] != 2.0 {
		t.Errorf("version = %v, want 2 (two category writes)", got["version"])
	}
	cats, _ := got["categories"].([]any)
	if len(cats) != 2 {
		t.Fatalf("categories = %v, want two", got["categories"])
	}
	root, _ := cats[0].(map[string]any)
	child, _ := cats[1].(map[string]any)
	if root["categoryId"] != w.categoryID || root["name"] != "Drinkware" || root["path"] != "Drinkware" {
		t.Errorf("root = %v", root)
	}
	if _, has := root["parentId"]; has {
		t.Errorf("a root category omits parentId: %v", root)
	}
	if child["parentId"] != w.categoryID || child["path"] != "Drinkware/Bottles" {
		t.Errorf("child = %v", child)
	}
}

// Before the first category the tree is version 0 and empty, never null.
func TestListCategoriesEmptyTree(t *testing.T) {
	h := newHarness(t)
	got := h.ok(t, "list_categories", map[string]any{})
	cats, ok := got["categories"].([]any)
	if got["version"] != 0.0 || !ok || len(cats) != 0 {
		t.Fatalf("empty tree = %v, want version 0 and an empty array", got)
	}
}

// Reading through every tool and the resource emits nothing and stores
// nothing: the MCP surface cannot append an outbox event or change a row.
func TestReadsChangeNothing(t *testing.T) {
	h, w := withWorld(t)
	before := h.store.Counts()
	h.ok(t, "get_product_content", map[string]any{"catalogItemId": w.publishedID})
	h.ok(t, "get_product_content", map[string]any{"catalogItemId": w.draftID})
	h.ok(t, "list_categories", map[string]any{})
	failWith(t, h.session, "get_product_content", map[string]any{"catalogItemId": "item-nobody"}, "not-found")
	if after := h.store.Counts(); after != before {
		t.Fatalf("store %+v -> %+v: a read tool changed state", before, after)
	}
}
