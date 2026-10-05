package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/lcleveland/netskope-mcp/internal/config"
	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

func TestRoute(t *testing.T) {
	coll := Resource{Collection: "/api/v2/profiles/destinations", UpdateMethod: "PATCH"}
	single := Resource{Collection: "/api/v2/ips/allowlist", UpdateMethod: "PATCH", Singleton: true}
	body := map[string]any{"x": 1}

	for _, c := range []struct {
		r            Resource
		in           Input
		method, path string
	}{
		{coll, Input{Action: ActionDeploy}, "POST", "/api/v2/profiles/destinations/deploy"},
		{coll, Input{Action: ActionRevert, ID: "7"}, "POST", "/api/v2/profiles/destinations/7/revert"},
		{coll, Input{Action: ActionUpdate, ID: "7", Body: body}, "PATCH", "/api/v2/profiles/destinations/7"},
		{single, Input{Action: ActionGet}, "GET", "/api/v2/ips/allowlist"},
		{Resource{Collection: "/c", CreateArray: true}, Input{Action: ActionCreate, Body: body}, "POST", "/c"},
		{single, Input{Action: ActionUpdate, Body: body}, "PATCH", "/api/v2/ips/allowlist"},
	} {
		m, p, _, err := c.r.route(c.in)
		if err != nil || m != c.method || p != c.path {
			t.Errorf("%s: got %s %s, %v; want %s %s", c.in.Action, m, p, err, c.method, c.path)
		}
	}

	for _, in := range []Input{
		{Action: ActionRevert},          // no id
		{Action: ActionUpdate, ID: "7"}, // no body
		{Action: ActionGet},             // collection get needs an id
	} {
		if _, _, _, err := coll.route(in); err == nil {
			t.Errorf("%+v accepted, want refused", in)
		}
	}
	if _, _, b, _ := (Resource{Collection: "/c", CreateArray: true}).route(Input{Action: ActionCreate, Body: body}); len(b.([]any)) != 1 {
		t.Errorf("CreateArray body = %v, want a one-element array", b)
	}
	if !ActionDeploy.Write() || !ActionRevert.Write() || ActionDeploy.Destructive() {
		t.Error("deploy/revert must count as writes and not as destructive")
	}
}

// A tool in a group --tool-groups does not know is a tool nobody can turn on.
func TestResourceTable(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range All() {
		if seen[r.Name] {
			t.Errorf("duplicate tool name %s", r.Name)
		}
		seen[r.Name] = true
		if !slices.Contains(config.Groups, r.Group) {
			t.Errorf("%s is in unknown group %q", r.Name, r.Group)
		}
	}
}

// MCP rejects a non-object structuredContent, and urllist create answers with a
// bare array: every non-list result has to come back as an object.
func TestCallWrapsNonObjectResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"id":16}]`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	c, err := netskope.New(netskope.Options{BaseURL: u, Token: "t", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	r := Resource{Collection: "/c"}
	out, err := r.call(context.Background(), c, []Action{ActionCreate}, Input{Action: ActionCreate, Body: map[string]any{"name": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(map[string]any); !ok {
		t.Errorf("create result is %T, want an object", out)
	}
}
