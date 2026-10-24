package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// URLLookupInput drives POST /api/v2/nsiq/urllookup.
type URLLookupInput struct {
	URLs     []string `json:"urls" jsonschema:"the URLs or domains to look up, 1 to 100"`
	Category string   `json:"category,omitempty" jsonschema:"limit predefined categories to casb or swg; omit for both"`
}

func registerURLLookup(s *mcp.Server, c *netskope.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "netskope_url_lookup",
		Title: "URL category lookup",
		Description: "Look up how Netskope categorizes URLs: the web categories and this tenant's " +
			"URL lists each one matches. Endpoint: POST /api/v2/nsiq/urllookup (a read; nothing " +
			"changes).\n\n" +
			"The first step when a user reports a site blocked or allowed unexpectedly: compare the " +
			"categories here with the policy that fired (netskope_event_search type=page). If " +
			"Netskope has the category wrong, file netskope_url_recategorizations `create`.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in URLLookupInput) (*mcp.CallToolResult, any, error) {
		out, err := lookupURLs(ctx, c, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})
}

func lookupURLs(ctx context.Context, c *netskope.Client, in URLLookupInput) (any, error) {
	if len(in.URLs) == 0 || len(in.URLs) > 100 {
		return nil, fmt.Errorf("`urls` takes 1 to 100 URLs, got %d", len(in.URLs))
	}
	if in.Category != "" && in.Category != "casb" && in.Category != "swg" {
		return nil, fmt.Errorf("`category` is casb or swg, got %q", in.Category)
	}
	query := map[string]any{"urls": in.URLs}
	if in.Category != "" {
		query["category"] = in.Category
	}
	var out any
	if err := c.Do(ctx, http.MethodPost, "/api/v2/nsiq/urllookup", nil, map[string]any{"query": query}, &out); err != nil {
		return nil, err
	}
	return capResult(out, defaultMaxItems), nil
}
