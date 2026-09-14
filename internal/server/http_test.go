package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/config"
	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

func httpFixture(t *testing.T, bearer string) *httptest.Server {
	t.Helper()
	tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	t.Cleanup(tenant.Close)

	u, _ := url.Parse(tenant.URL)
	c, err := netskope.New(netskope.Options{BaseURL: u, Token: "t", HTTPClient: tenant.Client()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{BaseURL: u, Groups: config.Groups, Path: "/mcp", HTTPAuthToken: bearer}
	s, _, err := New(cfg, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(Handler(cfg, s, nil))
	t.Cleanup(front.Close)
	return front
}

// Health must answer without a token: systemd and monitoring probe liveness, and
// handing them the bearer secret to do it would defeat the point of having one.
func TestHealthzNeedsNoToken(t *testing.T) {
	front := httpFixture(t, "bearer-abc")
	resp, err := front.Client().Get(front.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: got %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("healthz body: %v", body)
	}
}

func TestBearerGate(t *testing.T) {
	front := httpFixture(t, "bearer-abc")
	for _, tc := range []struct {
		name, header string
		want         int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, front.URL+"/mcp", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			resp, err := front.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("got %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// The full handshake over Streamable HTTP, through the bearer gate.
func TestStreamableHTTPHandshake(t *testing.T) {
	front := httpFixture(t, "bearer-abc")

	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:   front.URL + "/mcp",
			HTTPClient: &http.Client{Transport: bearerRoundTripper{front.Client().Transport, "bearer-abc"}},
		}, nil)
	if err != nil {
		t.Fatalf("connecting over streamable http: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) < 10 {
		t.Fatalf("got %d tools, want the full surface", len(res.Tools))
	}
}

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
