package tools

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// APIInput drives netskope_api: any v2 read the curated tools do not cover.
type APIInput struct {
	Method string            `json:"method,omitempty" jsonschema:"GET (default) or POST; POST only for lookup routes whose last segment starts with get"`
	Path   string            `json:"path" jsonschema:"the REST API v2 path, e.g. /api/v2/services/dlp/profiles"`
	Query  map[string]string `json:"query,omitempty" jsonschema:"query parameters such as fields, filter, limit or offset"`
	Body   map[string]any    `json:"body,omitempty" jsonschema:"the JSON request body for a POST lookup"`
}

// The generic tool only reads. Writes stay on the curated tools, whose
// descriptions carry the deploy/ordering caveats a raw route cannot.
//
// Netskope sends many lookups as POST (getusers, getanomalies, all of ADEM),
// and names them get<thing>; that naming is the only POST this tool sends.
// getclientlogs is the exception: it makes enrolled clients upload their logs.
func apiRequest(method, p string) (string, string, error) {
	method = strings.ToUpper(cmp.Or(method, http.MethodGet))
	clean, err := apiPath(p)
	if err != nil {
		return "", "", err
	}
	if method == http.MethodGet {
		return method, clean, nil
	}
	last := path.Base(clean)
	if method != http.MethodPost || !strings.HasPrefix(last, "get") || last == "getclientlogs" {
		return "", "", fmt.Errorf("%s %s is refused: netskope_api only reads (GET, or POST to a get* lookup); use a dedicated tool to change configuration", method, clean)
	}
	return method, clean, nil
}

// A GET is still not always harmless here, so some are refused outright:
//   - /events/dataexport/ advances a cursor shared with the tenant's SIEM
//     (see EventSearchInput); reading it silently drops the SIEM's logs.
//   - token/otp segments hand back live credentials: VPE and broker
//     registration tokens, streaming client JWTs, device OTPs, repo tokens.
//
// ponytail: substring denylist on segments, swap for an allowlist if Netskope
// ships a secret-bearing GET under a name this misses.
func apiPath(p string) (string, error) {
	clean := path.Clean("/" + strings.TrimSpace(p))
	if !strings.HasPrefix(clean, "/api/v2/") {
		return "", fmt.Errorf("path must start with /api/v2/, got %q", p)
	}
	if strings.HasPrefix(clean, "/api/v2/events/dataexport/") {
		return "", fmt.Errorf("%s is refused: reading the dataexport iterator consumes the cursor the tenant's SIEM reads from; use netskope_event_search", clean)
	}
	for _, seg := range strings.Split(clean, "/") {
		s := strings.ToLower(seg)
		if strings.Contains(s, "token") || s == "otp" {
			return "", fmt.Errorf("%s is refused: it returns live credentials, which this server never reads", clean)
		}
	}
	return clean, nil
}

func registerAPI(s *mcp.Server, c *netskope.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "netskope_api",
		Title: "Netskope API (generic read)",
		Description: "Read any Netskope REST API v2 route the dedicated tools do not cover: DLP " +
			"profiles and rules, destination/network/DNS profiles, IPsec and GRE tunnels, IPS, " +
			"RBI templates, DEM/ADEM, devices, RBAC roles, CCI app data, incidents, watchlists, " +
			"Enterprise Browser, AI gateway and so on. Path must start with /api/v2/.\n\n" +
			"Reads only: GET, or POST to a lookup route whose last segment starts with `get` " +
			"(e.g. /api/v2/users/getusers, /api/v2/incidents/anomalies/getanomalies, the ADEM " +
			"routes), with the query in `body`.\n\n" +
			"Prefer a dedicated tool when one exists (publishers, private apps, NPA policy, URL " +
			"lists, custom categories, SCIM, events): their descriptions carry caveats a raw " +
			"route does not. Routes vary by tenant and release; a 404 usually means the route is " +
			"not exposed here, a 403 that the token's role lacks it. Large collections are capped; " +
			"bound them with `query.limit`. Binary downloads (PDF, CSV, ZIP, images) are not " +
			"supported. The dataexport iterator and credential-returning routes are refused.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in APIInput) (*mcp.CallToolResult, any, error) {
		method, p, err := apiRequest(in.Method, in.Path)
		if err != nil {
			return errorResult(err), nil, nil
		}
		var body any
		if method == http.MethodPost {
			body = in.Body
			if in.Body == nil {
				body = map[string]any{}
			}
		}
		q := url.Values{}
		for k, v := range in.Query {
			q.Set(k, v)
		}
		var out any
		if err := c.Do(ctx, method, p, q, body, &out); err != nil {
			return errorResult(err), nil, nil
		}
		return nil, capResult(out, defaultMaxItems), nil
	})
}
