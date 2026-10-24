package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

func TestLookupURLs(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"result":[]}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	c, err := netskope.New(netskope.Options{BaseURL: u, Token: "t", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := lookupURLs(ctx, c, URLLookupInput{URLs: []string{"example.com"}, Category: "swg"}); err != nil {
		t.Fatal(err)
	}
	q, _ := body["query"].(map[string]any)
	if method != http.MethodPost || path != "/api/v2/nsiq/urllookup" || q["category"] != "swg" || len(q["urls"].([]any)) != 1 {
		t.Errorf("sent %s %s %v; want POST /api/v2/nsiq/urllookup {query:{urls,category}}", method, path, body)
	}

	for _, in := range []URLLookupInput{{}, {URLs: make([]string, 101)}, {URLs: []string{"a"}, Category: "web"}} {
		if _, err := lookupURLs(ctx, c, in); err == nil {
			t.Errorf("%d urls, category %q accepted; want refused", len(in.URLs), in.Category)
		}
	}
}
