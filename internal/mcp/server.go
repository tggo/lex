package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the reported server version.
const Version = "0.1.0"

// NewServer builds an MCP server exposing the lex tools backed by svc.
func NewServer(svc *Service) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "lex", Version: Version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_laws",
		Description: "Full-text search over legislation titles and article text. Returns ranked hits with an act_uri to pass to get_act.",
	}, handle(svc.SearchLaws))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_act",
		Description: "Get an act's metadata (title, version/as-of date, in-force status, source URL) and its articles, by resource URI.",
	}, handle(svc.GetAct))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_article",
		Description: "Get a single article of an act by act URI and article number.",
	}, handle(svc.GetArticle))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_amendments",
		Description: "List the acts this act amends, is amended by, or repeals.",
	}, handle(svc.ListAmendments))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_related",
		Description: "List the acts this act cites or consolidates.",
	}, handle(svc.FindRelated))

	return s
}

// handle adapts a plain (input -> output, error) function to an MCP tool
// handler. A nil result lets the SDK synthesize content from the structured
// output value.
func handle[In, Out any](fn func(context.Context, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := fn(ctx, in)
		return nil, out, err
	}
}
