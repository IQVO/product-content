// The MCP surface beyond tools (Agentic Standard v2, AS-3): ONE resource -
// the scoped content of one catalogue item - and ONE prompt - this context's
// operator procedure. Pinned through the same in-memory client the tool tests
// use.
package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const contentURIPrefix = "warehouse://product-content/contents/"

// The resource template is advertised with the fleet URI shape and reads the
// same read model as the get_product_content tool.
func TestSurface_ResourceTemplateListedAndRead(t *testing.T) {
	h, w := withWorld(t)
	ctx := t.Context()

	listed, err := h.session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}
	found := false
	for _, rt := range listed.ResourceTemplates {
		if rt.URITemplate == contentURIPrefix+"{catalogItemId}" {
			found = true
			if rt.MIMEType != "application/json" {
				t.Errorf("template MIMEType = %q, want application/json", rt.MIMEType)
			}
		}
	}
	if !found {
		t.Fatalf("content resource template not advertised: %+v", listed.ResourceTemplates)
	}

	res, err := h.session.ReadResource(ctx, &sdk.ReadResourceParams{URI: contentURIPrefix + w.publishedID})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %d entries, want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != contentURIPrefix+w.publishedID {
		t.Errorf("content URI = %q", c.URI)
	}
	var got struct {
		CatalogItemID     string `json:"catalogItemId"`
		State             string `json:"state"`
		Revision          int    `json:"revision"`
		PublishedRevision int    `json:"publishedRevision"`
		PrimaryCategoryID string `json:"primaryCategoryId"`
	}
	if err := json.Unmarshal([]byte(c.Text), &got); err != nil {
		t.Fatalf("resource text is not the content JSON: %v (%s)", err, c.Text)
	}
	if got.CatalogItemID != w.publishedID || got.State != "PUBLISHED" || got.Revision != 2 || got.PublishedRevision != 1 || got.PrimaryCategoryID != w.categoryID {
		t.Errorf("content = %+v, want %s PUBLISHED at revision 2 with revision 1 published in category %s", got, w.publishedID, w.categoryID)
	}
}

// A malformed URI and an unknown item are errors, never silent defaults.
func TestSurface_ResourceRejectsMalformedAndUnknown(t *testing.T) {
	h, w := withWorld(t)
	for _, uri := range []string{
		"staffing://product-content/contents/" + w.publishedID,    // wrong scheme
		"warehouse://product-content/categories/" + w.publishedID, // wrong model
		contentURIPrefix, // empty catalogItemId
		contentURIPrefix + w.publishedID + "/extra",                     // trailing segment
		contentURIPrefix + "item-nobody",                                // unknown item
		contentURIPrefix + strings.Repeat("x", 129),                     // an id that can never name a resource
		"warehouse://product-content/contents/" + w.publishedID + "%00", // NUL in the id
	} {
		if _, err := h.session.ReadResource(t.Context(), &sdk.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("read resource %q: expected an error", uri)
		}
	}
}

// The prompt is advertised with its typed argument and returns the procedure.
func TestSurface_PromptListedAndGet(t *testing.T) {
	h, w := withWorld(t)
	ctx := t.Context()

	listed, err := h.session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(listed.Prompts) != 1 || listed.Prompts[0].Name != "review_product_content" {
		t.Fatalf("prompts = %+v, want exactly review_product_content", listed.Prompts)
	}
	if len(listed.Prompts[0].Arguments) != 1 || listed.Prompts[0].Arguments[0].Name != "catalogItemId" || !listed.Prompts[0].Arguments[0].Required {
		t.Fatalf("prompt arguments = %+v, want one required catalogItemId", listed.Prompts[0].Arguments)
	}

	res, err := h.session.GetPrompt(ctx, &sdk.GetPromptParams{
		Name:      "review_product_content",
		Arguments: map[string]string{"catalogItemId": w.publishedID},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(res.Messages))
	}
	text := res.Messages[0].Content.(*sdk.TextContent).Text
	for _, needle := range []string{"get_product_content", "list_categories", w.publishedID, "publishedRevision"} {
		if !strings.Contains(text, needle) {
			t.Errorf("procedure text does not mention %q", needle)
		}
	}

	// A missing argument degrades to the placeholder, never to a hidden call.
	noArg, err := h.session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "review_product_content"})
	if err != nil {
		t.Fatalf("get prompt without arguments: %v", err)
	}
	if plain := noArg.Messages[0].Content.(*sdk.TextContent).Text; !strings.Contains(plain, "<catalogItemId>") {
		t.Errorf("procedure without an argument should carry the <catalogItemId> placeholder")
	}
}
