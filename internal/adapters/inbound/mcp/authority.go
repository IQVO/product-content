package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Authority levels (Agentic Standard v2, AS-1). Every registered tool
// declares exactly one of these in its Meta under authorityMetaKey; the fleet
// rule is in warehouse-docs agents/fleet/agent-authority.md. This context's
// MCP surface is read-only (ADR 0008), so every tool here is L1: a pure read
// of this context's own state that changes nothing. L2..L4 are declared but
// unused by any tool on purpose: the fleet's pinned declaration shape (the
// mcp_authority_fitness sensor) is the SAME const ladder in every repo, and
// authority_test.go exercises the whole ladder.
const (
	// authorityMetaKey is the tools/list _meta key carrying a tool's level.
	authorityMetaKey = "com.warehouse/authority"
	// authorityL1 reads facts and changes nothing.
	authorityL1 = "L1"
	// authorityL2 is a pure recommendation: computes a proposal, changes nothing.
	authorityL2 = "L2"
	// authorityL3 mutates state; the caller is accountable.
	authorityL3 = "L3"
	// authorityL4 mutates state inside an envelope an ADR of this context
	// grants; no such ADR exists here.
	authorityL4 = "L4"
)

// authorityMeta is the Meta a tool registers to declare its authority level.
func authorityMeta(level string) mcp.Meta {
	return mcp.Meta{authorityMetaKey: level}
}
