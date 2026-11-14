package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/IQVO/product-content/internal/domain/content"
)

// mimeTypeJSON is the MIME type of every resource this server serves.
const mimeTypeJSON = "application/json"

// contentURIPrefix is the scheme and path of the ONE resource this server
// offers (Agentic Standard v2, AS-3).
const contentURIPrefix = "warehouse://product-content/contents/"

// contentURITemplate is the ONE resource this server offers: a scoped read
// model this context owns, not a dump of a table. It answers "what does the
// product page of this item say?" for ONE catalogue item, built from the same
// GetProductContent read use case the get_product_content tool and GET
// /product-contents/{catalogItemId} use. Bounded by construction: one item,
// the content's fixed fields (at most 10 bullets, 50 attributes and 12 media),
// JSON text.
const contentURITemplate = contentURIPrefix + "{catalogItemId}"

// registerResources adds the resource surface (read-only by nature, L1; the
// charter keeps resources context data an agent can load without a tool
// call). No resource ever writes: Deps holds read use cases only (ADR 0008).
func (d Deps) registerResources(server *mcp.Server) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: contentURITemplate,
		Name:        "product content of one catalogue item",
		Description: "The product content of one catalogue item: state (DRAFT, PUBLISHED or RETIRED), description, bullets, attributes, media, primary category, revision and published revision. An unknown item is an error.",
		MIMEType:    mimeTypeJSON,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		itemID, err := parseContentURI(req.Params.URI)
		if err != nil {
			return nil, err
		}
		v, err := d.GetProductContent.Execute(ctx, content.ID(itemID))
		if err != nil {
			return nil, mapError(ctx, err)
		}
		return resourceResult(req.Params.URI, toContent(v))
	})
}

// resourceResult marshals one read model into the resource's text contents.
func resourceResult(uri string, view any) (*mcp.ReadResourceResult, error) {
	body, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: mimeTypeJSON,
			Text:     string(body),
		}},
	}, nil
}

// parseContentURI extracts the catalogItemId from a
// warehouse://product-content/contents/{catalogItemId} URI. The id is
// model-supplied and therefore untrusted: a malformed URI or an id that can
// never name a resource is rejected rather than silently defaulted.
func parseContentURI(uri string) (string, error) {
	rest, ok := strings.CutPrefix(uri, contentURIPrefix)
	if !ok {
		return "", fmt.Errorf("resource uri %q must start with %q", uri, contentURIPrefix)
	}
	if rest == "" || strings.Contains(rest, "/") {
		return "", fmt.Errorf("resource uri %q must carry exactly one non-empty catalogItemId after %q", uri, contentURIPrefix)
	}
	if err := checkItemID(rest); err != nil {
		return "", err
	}
	return rest, nil
}
