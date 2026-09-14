package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// eventTypes are the datasearch indices. Anything outside this set is rejected
// locally rather than sent, because the tenant's error for a bad type is a bare
// 404 that reads like the endpoint is missing.
var eventTypes = []string{"application", "audit", "page", "infrastructure", "network", "alert", "incident"}

// EventSearchInput drives a datasearch query.
//
// Deliberately NOT the dataexport iterator (/api/v2/events/dataexport/*): that
// endpoint keeps a server-side cursor keyed by a consumer-supplied `index` and
// advances it on every `next`. A model calling it would consume and skip pages
// belonging to whatever SIEM shares that index -- silent, permanent log loss
// that looks like nothing at all from here. datasearch is a stateless GET and
// is the right primitive for ad-hoc investigation.
type EventSearchInput struct {
	Type      string `json:"type" jsonschema:"the event index to search: application, audit, page, infrastructure, network, alert or incident"`
	Query     string `json:"query,omitempty" jsonschema:"a Skope IT Query Language expression, e.g. user eq 'a@b.com' and app eq 'Dropbox'"`
	StartTime string `json:"starttime,omitempty" jsonschema:"start of the window as an RFC3339 timestamp or a Unix epoch in seconds; defaults to 24 hours ago"`
	EndTime   string `json:"endtime,omitempty" jsonschema:"end of the window as an RFC3339 timestamp or a Unix epoch in seconds; defaults to now"`
	Fields    string `json:"fields,omitempty" jsonschema:"comma-separated field names to return; returning fewer fields is markedly faster"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum records to return; defaults to 100 and is capped at 1000"`
	Offset    int    `json:"offset,omitempty" jsonschema:"records to skip, for paging through a result set"`
}

// registerEvents adds the event tools and reports how many it added.
func registerEvents(s *mcp.Server, c *netskope.Client) int {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "netskope_event_search",
		Title: "Search events",
		Description: "Search Netskope event data with the Skope IT Query Language. " +
			"Endpoint: /api/v2/events/datasearch/{type}.\n\n" +
			"Indices: `application` (cloud app activity), `page` (web requests), `network` " +
			"(NPA and firewall flows), `audit` (administrator actions in the tenant), " +
			"`infrastructure` (publisher and appliance health), `alert` and `incident`.\n\n" +
			"Query syntax is field/operator/value joined with `and`/`or`, e.g. " +
			"`user eq 'a@b.com' and activity eq 'Upload'`. Always bound the window: an " +
			"unqualified search over a busy tenant is slow and returns little of use. " +
			"Naming `fields` makes the query substantially faster and the result far smaller.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in EventSearchInput) (*mcp.CallToolResult, any, error) {
		out, err := searchEvents(ctx, c, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "netskope_alert_search",
		Title: "Search alerts",
		Description: "Search the alert index: DLP matches, malware detections, anomalies, " +
			"compromised credentials, policy violations and watchlist hits. A convenience " +
			"wrapper over netskope_event_search with type=alert.\n\n" +
			"Useful query fields: `alert_type`, `severity`, `user`, `app`, `policy`, `action`. " +
			"Start broad over a short window, then narrow -- alert volume varies enormously " +
			"between tenants.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in EventSearchInput) (*mcp.CallToolResult, any, error) {
		in.Type = "alert"
		out, err := searchEvents(ctx, c, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})

	return 2
}

func searchEvents(ctx context.Context, c *netskope.Client, in EventSearchInput) (any, error) {
	if in.Type == "" {
		return nil, fmt.Errorf("`type` is required; one of %v", eventTypes)
	}
	if !slices.Contains(eventTypes, in.Type) {
		return nil, fmt.Errorf("unknown event type %q; one of %v", in.Type, eventTypes)
	}

	// Default to the last 24 hours rather than letting the tenant pick: an
	// unbounded datasearch can run to its 180s server-side timeout and return
	// nothing, which reads as a broken tool rather than a too-broad question.
	now := time.Now()
	start, err := epoch(in.StartTime, now.Add(-24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("starttime: %w", err)
	}
	end, err := epoch(in.EndTime, now)
	if err != nil {
		return nil, fmt.Errorf("endtime: %w", err)
	}
	if end <= start {
		return nil, fmt.Errorf("endtime (%d) must be after starttime (%d)", end, start)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q := url.Values{}
	q.Set("starttime", strconv.FormatInt(start, 10))
	q.Set("endtime", strconv.FormatInt(end, 10))
	q.Set("limit", strconv.Itoa(limit))
	if in.Offset > 0 {
		q.Set("offset", strconv.Itoa(in.Offset))
	}
	if in.Query != "" {
		q.Set("query", in.Query)
	}
	if in.Fields != "" {
		q.Set("fields", in.Fields)
	}

	var out any
	path := "/api/v2/events/datasearch/" + url.PathEscape(in.Type)
	if err := c.Do(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return capResult(out, limit), nil
}

// epoch accepts either an RFC3339 timestamp or a bare Unix epoch, because a
// model will reach for both and the tenant only takes the latter.
func epoch(s string, fallback time.Time) (int64, error) {
	if s == "" {
		return fallback.Unix(), nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, fmt.Errorf("want an RFC3339 timestamp or a Unix epoch in seconds, got %q", s)
	}
	return t.Unix(), nil
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
