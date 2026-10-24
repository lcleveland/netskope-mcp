package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// Each event type must reach its own route; a wrong one is a bare 404 that
// reads like the tenant lacks the feature.
func TestSearchEventsPaths(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Write([]byte(`{"result":[]}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	c, err := netskope.New(netskope.Options{BaseURL: u, Token: "t", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for typ, want := range map[string]string{
		"clientstatus": "/api/v2/events/datasearch/clientstatus",
		"alert":        "/api/v2/events/datasearch/alert",
	} {
		if _, err := searchEvents(context.Background(), c, EventSearchInput{Type: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if got != want {
			t.Errorf("%s searched %s, want %s", typ, got, want)
		}
	}
	if _, err := searchEvents(context.Background(), c, EventSearchInput{Type: "infrastructure"}); err == nil {
		t.Error("infrastructure accepted, want refused")
	}
}
