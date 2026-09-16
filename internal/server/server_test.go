package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/config"
	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

func testServer(t *testing.T, allowDestructive bool) *mcp.Server {
	t.Helper()
	return testServerWithGroups(t, config.Groups, allowDestructive)
}

func testServerWithGroups(t *testing.T, groups []string, allowDestructive bool) *mcp.Server {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	t.Cleanup(fake.Close)

	u, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := netskope.New(netskope.Options{BaseURL: u, Token: "t", HTTPClient: fake.Client()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{BaseURL: u, Groups: groups, AllowDestructive: allowDestructive, Path: "/mcp"}
	s, n, err := New(cfg, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no tools registered")
	}
	return s
}

func connect(t *testing.T, s *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestToolsListed(t *testing.T) {
	cs := connect(t, testServer(t, false))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) < 10 {
		t.Fatalf("expected the full tool surface, got %d", len(res.Tools))
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"netskope_tenant_info", "netskope_publishers", "netskope_event_search"} {
		if !strings.Contains(strings.Join(names, ","), want) {
			t.Errorf("missing tool %q; got %v", want, names)
		}
	}
}

// The security-critical test: with destructive actions off, "delete" must not
// appear in any advertised schema, and calling it must fail.
func TestDeleteNotAdvertisedWhenDisallowed(t *testing.T) {
	s := testServer(t, false)
	cs := connect(t, s)
	ctx := context.Background()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		schema := mustJSON(t, tool.InputSchema)
		if strings.Contains(schema, `"delete"`) {
			t.Errorf("tool %q advertises a delete action with allowDestructive=false: %s", tool.Name, schema)
		}
	}

	out, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "netskope_publishers",
		Arguments: map[string]any{"action": "delete", "id": "1"},
	})
	if err != nil {
		return // rejected at the protocol layer by schema validation: also fine
	}
	if !out.IsError {
		t.Fatal("delete succeeded with allowDestructive=false")
	}
}

func TestDeleteAdvertisedWhenAllowed(t *testing.T) {
	cs := connect(t, testServer(t, true))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range res.Tools {
		if tool.Name != "netskope_publishers" {
			continue
		}
		found = strings.Contains(mustJSON(t, tool.InputSchema), `"delete"`)
	}
	if !found {
		t.Fatal("delete was not advertised with allowDestructive=true")
	}
}

// mustJSON re-encodes whatever shape the schema arrived in over the wire.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The internetaccess API is still in development and has to be enabled per
// tenant by Netskope, so on most tenants those tools can only 403 -- the default
// configuration must not register them, and naming the group must.
func TestInternetAccessIsOptIn(t *testing.T) {
	const gated = "netskope_realtime_policy_rules"

	if names := toolNames(t, testServerWithGroups(t, config.DefaultGroups, false)); slices.Contains(names, gated) {
		t.Fatalf("%s was registered by the default tool groups %v", gated, config.DefaultGroups)
	}
	optedIn := append(slices.Clone(config.DefaultGroups), "internetaccess")
	if names := toolNames(t, testServerWithGroups(t, optedIn, false)); !slices.Contains(names, gated) {
		t.Fatalf("%s was not registered after opting in; got %v", gated, names)
	}
}

func toolNames(t *testing.T, s *mcp.Server) []string {
	t.Helper()
	res, err := connect(t, s).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(res.Tools))
	for i, tool := range res.Tools {
		names[i] = tool.Name
	}
	return names
}
