package netskope

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires a Client to a fake tenant. Every test in this package goes
// through here, which is what keeps `go test` hermetic enough to run inside the
// Nix build sandbox: nothing ever dials a real host.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{
		BaseURL:    u,
		Token:      "s3cr3t",
		HTTPClient: srv.Client(),
		RateLimit:  1000, // do not pace the tests
		RateBurst:  1000,
		Retry:      &RetryPolicy{MaxRetries: 3, Base: time.Millisecond, Max: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestAuthHeaderIsSent(t *testing.T) {
	var gotToken, gotAuth, gotAccept, gotUA string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(DefaultAuthHeader)
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(`{"data":[]}`))
	})
	if err := c.Do(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotToken != "s3cr3t" {
		t.Errorf("%s = %q, want the token", DefaultAuthHeader, gotToken)
	}
	// Sending both auth headers makes some tenants reject the request outright.
	if gotAuth != "" {
		t.Errorf("Authorization was also sent (%q); only %s should be", gotAuth, DefaultAuthHeader)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if !strings.HasPrefix(gotUA, "netskope-mcp/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestRetriesRateLimitAndHonoursRetryAfter(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"data":[1]}`))
	})
	var out any
	if err := c.Do(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, &out); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("made %d requests, want 2 (one 429, one success)", n)
	}
}

// A POST that reached the tenant and failed with a 5xx must NOT be retried:
// the tenant may have acted on it, and a second attempt creates a duplicate
// object that nothing downstream can distinguish from the first.
func TestNonIdempotentVerbNotRetriedOnServerError(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusBadGateway)
	})
	err := c.Do(context.Background(), http.MethodPost, "/api/v2/x", nil, map[string]string{"a": "b"}, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if n != 1 {
		t.Fatalf("POST was retried %d times on 502; it must not be", n-1)
	}
}

func TestIdempotentVerbRetriedOnServerError(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{}`))
	})
	if err := c.Do(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("made %d requests, want 3", n)
	}
}

// A plain 500 on a GET is retried too, not just the 502/503/504 family. The
// Advanced Analytics routes answer a transient backend hiccup with one, and a
// tenant that 500s once then succeeds must not surface as a failed tool call.
func TestIdempotentVerbRetriedOnInternalServerError(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{}`))
	})
	if err := c.Do(context.Background(), http.MethodGet, "/api/v2/reporting/aa/reports", nil, nil, nil); err != nil {
		t.Fatalf("a 500 that clears on retry still failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("made %d requests, want 2", n)
	}
}

// ...and the idempotent guard still holds for 500: a POST the tenant may have
// acted on is not repeated just because 500 joined the retryable set.
func TestNonIdempotentVerbNotRetriedOnInternalServerError(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	if err := c.Do(context.Background(), http.MethodPost, "/api/v2/x", nil, map[string]string{"a": "b"}, nil); err == nil {
		t.Fatal("want an error")
	}
	if n != 1 {
		t.Fatalf("POST was retried %d times on 500; it must not be", n-1)
	}
}

// A 429 IS safe to retry even for a POST: it means the tenant rejected the
// request before acting on it.
func TestNonIdempotentVerbRetriedOnRateLimit(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{}`))
	})
	if err := c.Do(context.Background(), http.MethodPost, "/api/v2/x", nil, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("made %d requests, want 2", n)
	}
}

func TestErrorHints(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, `{}`, "token was rejected"},
		{http.StatusForbidden, `{}`, "no grant for this endpoint"},
		{http.StatusNotFound, `{}`, "no such object"},
		// Kong answers an absent route with a 404 that has nothing to do with
		// the object asked for, so it must not read as "no such object".
		{http.StatusNotFound, `{"message":"no Route matched with those values"}`, "no such API route"},
		// The tenant answers a missing publisher with 200 and an error
		// envelope. Do must reject it rather than hand it back as data.
		{http.StatusOK, `{"status":"error","message":"No publisher with id '999999' is found."}`, "error envelope"},
	} {
		c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		})
		err := c.Do(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, nil)
		if err == nil {
			t.Fatalf("status %d body %s: want an error", tc.status, tc.body)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d: error %q does not explain the cause (want %q)", tc.status, err, tc.want)
		}
	}
}

// A 2xx payload that merely carries a status field, or is a JSON array, is real
// data and must survive the error-envelope check.
func TestErrorEnvelopeDoesNotEatRealPayloads(t *testing.T) {
	for _, body := range []string{
		`{"status":"success","data":{"publishers":[]}}`,
		`[{"id":1,"name":"Allowed URLs"}]`,
		`{"data":{"status":"error"}}`,
	} {
		c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(body))
		})
		var out any
		if err := c.Do(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, &out); err != nil {
			t.Errorf("body %s: rejected a real payload: %v", body, err)
		}
	}
}

func TestErrorUsesStructuredMessage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error","code":"E1234","message":"host is required"}`))
	})
	err := c.Do(context.Background(), http.MethodGet, "/api/v2/x", nil, nil, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "E1234") || !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("error lost the tenant's own message: %v", err)
	}
}

// The token must never ride along in an error, even when the tenant echoes it
// back in a failing response body.
func TestErrorDoesNotLeakToken(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"echo":"` + r.Header.Get(DefaultAuthHeader) + `"}`))
	})
	err := c.Do(context.Background(), http.MethodPost, "/api/v2/x", nil, map[string]string{}, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("the error leaked the token: %v", err)
	}
}

func TestClientStringRedacts(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {})
	if strings.Contains(c.String(), "s3cr3t") {
		t.Fatalf("Client.String() leaked the token: %s", c.String())
	}
	if strings.Contains(c.LogValue().String(), "s3cr3t") {
		t.Fatalf("Client.LogValue() leaked the token")
	}
}

// The query string carries user identities in event and SCIM filters, so it must
// not appear in an error that will be logged and handed to a model.
func TestErrorOmitsQueryString(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	q := url.Values{"filter": {`userName eq "alice@example.com"`}}
	err := c.Do(context.Background(), http.MethodGet, "/api/v2/scim/Users", q, nil, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "alice@example.com") {
		t.Fatalf("the error leaked a query parameter: %v", err)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.Do(ctx, http.MethodGet, "/api/v2/x", nil, nil, nil); err == nil {
		t.Fatal("want an error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("retries ignored the context deadline: took %s", d)
	}
}
