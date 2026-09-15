package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

func items(n int) []any {
	xs := make([]any, n)
	for i := range xs {
		xs[i] = map[string]any{"id": i, "name": strings.Repeat("x", 32)}
	}
	return xs
}

// An unqualified list against a large tenant must not hand the model tens of
// thousands of records: it blows the context window and costs real money.
func TestCapResultTruncatesEnvelope(t *testing.T) {
	in := map[string]any{"status": "success", "total": float64(5000), "data": items(1000)}
	out, ok := capResult(in, 10).(map[string]any)
	if !ok {
		t.Fatalf("want a map, got %T", out)
	}
	if got := len(out["data"].([]any)); got != 10 {
		t.Fatalf("returned %d items, want 10", got)
	}
	tr, ok := out["_truncation"].(truncation)
	if !ok {
		t.Fatal("no _truncation marker: the model would take a partial result for a complete one")
	}
	if !tr.Truncated || tr.Returned != 10 || tr.Total != 5000 {
		t.Fatalf("truncation marker = %+v", tr)
	}
}

func TestCapResultTruncatesBareArray(t *testing.T) {
	out, ok := capResult(items(50), 5).(map[string]any)
	if !ok {
		t.Fatalf("want a wrapped map, got %T", out)
	}
	if got := len(out["data"].([]any)); got != 5 {
		t.Fatalf("returned %d items, want 5", got)
	}
}

// MCP rejects a non-object structuredContent, so even an untruncated bare array
// has to come back wrapped: /api/v2/policy/urllist failed schema validation
// whenever the tenant had few enough lists to stay under the cap.
func TestCapResultWrapsShortBareArray(t *testing.T) {
	out, ok := capResult(items(3), 10).(map[string]any)
	if !ok {
		t.Fatalf("want a wrapped map, got %T", out)
	}
	if got := len(out["data"].([]any)); got != 3 {
		t.Fatalf("returned %d items, want 3", got)
	}
	if _, marked := out["_truncation"]; marked {
		t.Fatal("a result within budget was marked truncated")
	}
}

func TestCapResultLeavesSmallResultsAlone(t *testing.T) {
	in := map[string]any{"status": "success", "data": items(3)}
	out := capResult(in, 10)
	m := out.(map[string]any)
	if _, marked := m["_truncation"]; marked {
		t.Fatal("a result within budget was marked truncated")
	}
	if len(m["data"].([]any)) != 3 {
		t.Fatal("a result within budget was trimmed")
	}
}

// Item count alone is not enough: a handful of policy objects with large embedded
// rule sets can still be enormous.
func TestCapResultEnforcesByteBudget(t *testing.T) {
	big := make([]any, 40)
	for i := range big {
		big[i] = map[string]any{"id": i, "blob": strings.Repeat("y", 8<<10)}
	}
	for _, max := range []int{100, 10} { // 100 leaves the item cap unused; 10 trips it
		out := capResult(map[string]any{"data": big}, max)
		b, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > maxBytes {
			t.Errorf("max=%d: encoded result is %d bytes, past the %d budget", max, len(b), maxBytes)
		}
	}
}

// A bare array gets no envelope to hide behind, and used to skip the byte budget
// entirely.
func TestBareArrayEnforcesByteBudget(t *testing.T) {
	big := make([]any, 40)
	for i := range big {
		big[i] = map[string]any{"id": i, "blob": strings.Repeat("y", 8<<10)}
	}
	b, err := json.Marshal(capResult(big, 100))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxBytes {
		t.Fatalf("encoded result is %d bytes, past the %d budget", len(b), maxBytes)
	}
}

// SCIM returns its collection under "Resources", not "data". While capResult
// only knew "data", /api/v2/scim/Users skipped both caps and a whole tenant's
// user list went out at once.
func TestSCIMEnvelopeIsCapped(t *testing.T) {
	in := map[string]any{
		"Resources":    items(50),
		"totalResults": float64(500),
	}
	out, ok := capResult(in, 5).(map[string]any)
	if !ok {
		t.Fatalf("want a map, got %T", capResult(in, 5))
	}
	if got := len(out["Resources"].([]any)); got != 5 {
		t.Fatalf("Resources has %d items, want 5", got)
	}
	if _, dup := out["data"]; dup {
		t.Fatal("capped items were written to `data`, leaving a stale `Resources`")
	}
	if tr := out["_truncation"].(truncation); tr.Total != 500 {
		t.Fatalf("total = %d, want the SCIM totalResults 500", tr.Total)
	}
}

func TestSingleObjectPassesThrough(t *testing.T) {
	in := map[string]any{"id": "1", "name": "pub"}
	out := capResult(in, 10).(map[string]any)
	if out["name"] != "pub" {
		t.Fatalf("a single object was mangled: %v", out)
	}
}

// Actions that need an id or a body must be rejected here, not sent as a
// malformed request: schema validation is advisory and a client may skip it.
func TestRouteValidatesArguments(t *testing.T) {
	r := Resource{Name: "t", Collection: "/api/v2/x", Actions: crud}
	for _, tc := range []struct {
		name string
		in   Input
		want string
	}{
		{"get without id", Input{Action: ActionGet}, "requires `id`"},
		{"update without id", Input{Action: ActionUpdate, Body: map[string]any{"a": 1}}, "requires `id`"},
		{"update without body", Input{Action: ActionUpdate, ID: "1"}, "requires `body`"},
		{"create without body", Input{Action: ActionCreate}, "requires `body`"},
		{"delete without id", Input{Action: ActionDelete}, "requires `id`"},
		{"unknown action", Input{Action: "frobnicate"}, "unknown action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := r.route(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestItemPathEscapesID(t *testing.T) {
	r := Resource{Collection: "/api/v2/x"}
	if got := r.itemPath("a b/c"); got != "/api/v2/x/a%20b%2Fc" {
		t.Fatalf("itemPath = %q; an unescaped id could reach a different endpoint", got)
	}
}

// itemPath escaping only matters if it survives URL assembly. It did not: the
// client used to hand the escaped path to url.URL.Path, which holds the decoded
// path, so %2F went out as %252F and the request landed somewhere else.
func TestEscapedIDSurvivesTheClient(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := netskope.New(netskope.Options{BaseURL: base, Token: "t", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}

	r := Resource{Name: "x", Collection: "/api/v2/x", Actions: []Action{ActionGet}}
	if _, err := r.call(context.Background(), c, r.Actions, Input{Action: ActionGet, ID: "a b/c"}); err != nil {
		t.Fatal(err)
	}
	if want := "/api/v2/x/a%20b%2Fc"; got != want {
		t.Fatalf("the tenant saw %q, want %q", got, want)
	}
}

// SCIM pages with count/startIndex. Sending it "limit" bounds nothing at the
// tenant, which is the whole point of applying a default.
func TestListDefaultUsesTheCollectionsOwnLimitParam(t *testing.T) {
	for _, r := range All() {
		q := url.Values{}
		applyListDefaults(q, r.limitParam(), r.maxItems())
		if q.Get(r.limitParam()) == "" {
			t.Errorf("%s: list is unbounded at the tenant", r.Name)
		}
		if strings.HasPrefix(r.Collection, "/api/v2/scim/") && r.limitParam() != "count" {
			t.Errorf("%s: SCIM collection bounded with %q, want \"count\"", r.Name, r.limitParam())
		}
		// A live tenant ignores `count` sent without `startIndex` and returns
		// every record, so the bound has to travel with its page origin.
		if r.limitParam() == "count" && q.Get("startIndex") == "" {
			t.Errorf("%s: count sent without startIndex; the tenant ignores it", r.Name)
		}
	}
}

// The NPA policy endpoints route only PATCH at item level, so update must not
// fall back to the PUT default that every other resource uses.
func TestUpdateMethodOverride(t *testing.T) {
	for _, tc := range []struct {
		r    Resource
		want string
	}{
		{Resource{Collection: "/api/v2/x", Actions: crud}, "PUT"},
		{Resource{Collection: "/api/v2/policy/npa/rules", Actions: crud, UpdateMethod: "PATCH"}, "PATCH"},
	} {
		got, _, _, err := tc.r.route(Input{Action: ActionUpdate, ID: "1", Body: map[string]any{"a": 1}})
		if err != nil {
			t.Fatalf("%s: %v", tc.r.Collection, err)
		}
		if got != tc.want {
			t.Errorf("%s: update uses %s, want %s", tc.r.Collection, got, tc.want)
		}
	}
}
