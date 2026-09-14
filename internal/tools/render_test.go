package tools

import (
	"encoding/json"
	"strings"
	"testing"
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
	out := capResult(map[string]any{"data": big}, 100)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxBytes*2 {
		t.Fatalf("encoded result is %d bytes, well past the %d budget", len(b), maxBytes)
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
		{"update without id", Input{Action: ActionUpdate, Body: json.RawMessage(`{}`)}, "requires `id`"},
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
