package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// reviewProductContentSOP is the operator procedure the review_product_content
// prompt hands to the model (Agentic Standard v2, AS-3). Per the charter,
// prompts encode how to interpret results, when to escalate and what "done"
// means; this one names tools but NEVER calls them, and it cannot write: every
// tool it names is read-only (ADR 0008) and every write of this context is a
// REST operation.
const reviewProductContentSOP = `You are reviewing the product content of one catalogue item (ITEM) of the retail network's storefront. Use only the MCP tools of product-content; never assume state - read it.

Procedure:
1. Call get_product_content(catalogItemId = ITEM). The answer is this service's own recorded content: state (DRAFT, PUBLISHED or RETIRED), description, bullets, attributes, media (with the role PRIMARY or GALLERY), primaryCategoryId, revision and publishedRevision.
2. When primaryCategoryId is present, call list_categories() and find that category: its path says where the product sits in the tree. A primaryCategoryId absent from the tree is a finding to report, not something to fix.

Interpretation:
- publishedRevision is the revision shoppers see (0 means never published). revision is the current shape. A PUBLISHED content whose revision is above publishedRevision has a revision waiting for a publication: shoppers still see the older one. Say so; do not report the waiting text as live.
- A DRAFT is never shown to shoppers. Content can be published only when it has a description, exactly one PRIMARY medium and a category; list which of the three is missing.
- A RETIRED content is terminal; retiredReason says why (MANUAL, DISCARDED or DISCONTINUED). Do not suggest reviving it.
- An unknown item is a not-found answer: the item has no content here, which is not evidence that the item does not exist elsewhere.

Escalate to the human content owner when the content is a DRAFT that misses the publish gate, when a revision is waiting for a publication, or when the primary category is not in the tree. Creating, revising, publishing, retiring or discarding content and changing categories are human/REST actions - nothing in this server can perform them, so do not promise them.

Done means: you have reported the state, the revision versus the published revision, what is present or missing (description, bullets, attributes, media with exactly one PRIMARY, primary category and its path) and named the escalation when one applies.`

// registerPrompts adds the workflow prompt (the operator procedure of this
// context). Read-only by construction: a prompt returns text.
func (d Deps) registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "review_product_content",
		Description: "Operator procedure for reviewing the product content of one catalogue item: read the content, compare its revision with the published revision, check the publish gate facts against the category tree and know when to escalate to the human content owner.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "catalogItemId",
			Description: "the catalogue item id whose content to review, e.g. item-123",
			Required:    true,
		}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		itemID := strings.TrimSpace(req.Params.Arguments["catalogItemId"])
		if itemID == "" {
			itemID = "<catalogItemId>"
		}
		return &mcp.GetPromptResult{
			Description: "How to review product content: read the content, compare revision with publishedRevision, check the primary category in the tree, escalate DRAFTs that miss the publish gate to the human content owner.",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: strings.ReplaceAll(reviewProductContentSOP, "ITEM", itemID)},
			}},
		}, nil
	})
}
