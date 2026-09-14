package tools

import (
	"context"
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// TenantInfoOutput is deliberately small: this tool exists to answer "is the
// token working and what can it see", not to dump tenant configuration.
type TenantInfoOutput struct {
	BaseURL   string `json:"base_url"`
	Reachable bool   `json:"reachable"`
	Detail    string `json:"detail"`
}

// registerTenantInfo adds the connectivity probe.
//
// This is the first tool a session should call and the cheapest way to tell the
// three failure modes apart: a wrong tenant URL (connection error), a bad token
// (401), and a token whose grants do not cover the endpoint in question (403).
// Without it, all three surface as an opaque failure on whatever tool the model
// happened to try first.
func registerTenantInfo(s *mcp.Server, c *netskope.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "netskope_tenant_info",
		Title: "Tenant connectivity check",
		Description: "Check that the configured Netskope tenant is reachable and that the API " +
			"token is accepted. Returns the tenant base URL and whether a probe request " +
			"succeeded.\n\n" +
			"Call this first when anything else fails: it separates a wrong tenant URL from a " +
			"rejected token from a token whose per-endpoint grants are too narrow, which " +
			"otherwise all look the same.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, TenantInfoOutput, error) {
		out := TenantInfoOutput{BaseURL: c.BaseURL()}
		// A cheap, universally granted read. Any 2xx proves URL plus token; a
		// 403 proves URL plus token but a narrow grant, which is still useful.
		var sink any
		err := c.Do(ctx, http.MethodGet, "/api/v2/infrastructure/publishers", limitOne(), nil, &sink)
		switch {
		case err == nil:
			out.Reachable, out.Detail = true, "the tenant accepted the API token"
		default:
			var ae *netskope.APIError
			if errors.As(err, &ae) && ae.Status == http.StatusForbidden {
				out.Reachable = true
				out.Detail = "the token is valid but has no grant for the probe endpoint; " +
					"tenant connectivity is fine and per-endpoint grants need widening"
				return nil, out, nil
			}
			out.Detail = err.Error()
		}
		return nil, out, nil
	})
}
