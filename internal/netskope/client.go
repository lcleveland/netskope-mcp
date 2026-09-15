// Package netskope is a typed client for the Netskope REST API v2.
//
// Endpoint paths and payload shapes vary by tenant and by whether beta routes
// are enabled, so the authority is the tenant's own Swagger at
// https://<tenant>/apidocs/?include_beta_routes=1 -- not the public docs.
package netskope

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lcleveland/netskope-mcp/internal/version"
)

// Client talks to one tenant. It is safe for concurrent use.
type Client struct {
	base   *url.URL
	token  string
	ua     string
	hc     *http.Client
	log    *slog.Logger
	retry  RetryPolicy
	limit  *limiter
	header string
}

// Options configures a Client. HTTPClient is the test seam: every test in this
// package injects an httptest server through it, which is what keeps `go test`
// hermetic enough to run inside the Nix build sandbox.
type Options struct {
	BaseURL    *url.URL
	Token      string
	Timeout    time.Duration
	Logger     *slog.Logger
	HTTPClient *http.Client
	AuthHeader string // defaults to "Netskope-API-Token"
	Retry      *RetryPolicy
	RateLimit  float64 // requests per second; 0 means the default
	RateBurst  int
}

// DefaultAuthHeader is the header Netskope REST API v2 documents. Some tooling
// also accepts "Authorization: Bearer"; AuthHeader exists so a tenant that
// insists on the latter is a config change rather than a patch.
const DefaultAuthHeader = "Netskope-API-Token"

func New(o Options) (*Client, error) {
	if o.BaseURL == nil {
		return nil, fmt.Errorf("netskope: BaseURL is required")
	}
	if o.Token == "" {
		return nil, fmt.Errorf("netskope: Token is required")
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: o.Timeout}
	}
	if o.AuthHeader == "" {
		o.AuthHeader = DefaultAuthHeader
	}
	retry := DefaultRetryPolicy
	if o.Retry != nil {
		retry = *o.Retry
	}
	if o.RateLimit <= 0 {
		o.RateLimit = 5
	}
	if o.RateBurst <= 0 {
		o.RateBurst = 10
	}
	return &Client{
		base:   o.BaseURL,
		token:  o.Token,
		ua:     "netskope-mcp/" + version.Version + " (+https://github.com/lcleveland/netskope-mcp)",
		hc:     o.HTTPClient,
		log:    o.Logger,
		retry:  retry,
		limit:  newLimiter(o.RateLimit, o.RateBurst),
		header: o.AuthHeader,
	}, nil
}

// BaseURL reports the tenant this client is bound to. Non-secret.
func (c *Client) BaseURL() string { return c.base.String() }

// String and LogValue exist so that a Client can never be printed with its token.
func (c *Client) String() string { return "netskope.Client{" + c.base.String() + "}" }
func (c *Client) LogValue() slog.Value {
	return slog.GroupValue(slog.String("base_url", c.base.String()), slog.String("token", "***"))
}

// Do issues one request, retrying per the client's policy, and decodes a 2xx
// body into out (which may be nil to discard it).
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		switch b := body.(type) {
		case json.RawMessage:
			payload = b
		case []byte:
			payload = b
		default:
			var err error
			if payload, err = json.Marshal(b); err != nil {
				return fmt.Errorf("encoding request body for %s %s: %w", method, path, err)
			}
		}
	}

	// JoinPath rather than u.Path = base.Path + path: Resource.itemPath has already
	// percent-escaped the id, and url.URL.Path holds the *decoded* path, so
	// assigning to it escapes the escapes (%2F becomes %252F) and the request goes
	// somewhere else entirely. JoinPath treats its argument as already escaped.
	u := c.base.JoinPath(path)
	if q != nil {
		u.RawQuery = q.Encode()
	}

	raw, err := c.send(ctx, method, u, path, payload)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding %s %s response: %w", method, path, err)
	}
	return nil
}

// send performs the request with retries and returns the successful response body.
func (c *Client) send(ctx context.Context, method string, u *url.URL, logPath string, payload []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, c.retry.backoff(attempt, lastRetryAfter(lastErr))); err != nil {
				return nil, err
			}
		}
		if err := c.limit.wait(ctx); err != nil {
			return nil, err
		}

		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
		if err != nil {
			return nil, fmt.Errorf("building %s %s: %w", method, logPath, err)
		}
		req.Header.Set(c.header, c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.ua)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		start := time.Now()
		resp, err := c.hc.Do(req)
		if err != nil {
			// A transport error may mean the request never reached the tenant, or
			// that it did and the reply was lost. Only the former is safe to retry
			// for a non-idempotent verb.
			lastErr = err
			if c.retry.retryTransport(method, attempt) {
				c.log.Debug("netskope request failed, retrying", "method", method, "path", logPath, "attempt", attempt, "err", err)
				continue
			}
			return nil, fmt.Errorf("netskope %s %s: %w", method, logPath, err)
		}

		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		c.limit.observe(resp.Header)
		c.log.Debug("netskope request",
			"method", method, "path", logPath, "status", resp.StatusCode,
			"duration", time.Since(start), "attempt", attempt)

		if readErr != nil {
			lastErr = readErr
			if c.retry.retryTransport(method, attempt) {
				continue
			}
			return nil, fmt.Errorf("netskope %s %s: reading response: %w", method, logPath, readErr)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Some v2 routes answer a missing object with 200 and an error
			// envelope instead of a 4xx. Without this the envelope reaches the
			// model as data, shaped just enough like a record to be believed.
			if isErrorEnvelope(raw) {
				return nil, newAPIError(method, logPath, resp, raw, c.token)
			}
			return raw, nil
		}

		apiErr := newAPIError(method, logPath, resp, raw, c.token)
		lastErr = apiErr
		if c.retry.retryStatus(method, resp.StatusCode, attempt) {
			c.log.Debug("netskope request retryable", "method", method, "path", logPath, "status", resp.StatusCode, "attempt", attempt)
			continue
		}
		return nil, apiErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("netskope %s %s: retries exhausted", method, logPath)
	}
	var ae *APIError
	if errors.As(lastErr, &ae) {
		return nil, ae
	}
	return nil, fmt.Errorf("netskope %s %s: %w", method, logPath, lastErr)
}

// newAPIError maps a failing response, pulling Netskope's error code and message
// out of the several envelope shapes v2 uses.
func newAPIError(method, path string, resp *http.Response, raw []byte, secret string) *APIError {
	e := &APIError{
		Status: resp.StatusCode, Method: method, Path: path,
		Snippet:    truncate(raw, secret),
		RetryAfter: parseRetryAfter(resp.Header, time.Now()),
	}
	var env struct {
		Status  string `json:"status"`
		Code    any    `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Errors  []struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &env) == nil {
		e.Code = stringify(env.Code)
		e.Message = scrub(firstNonEmpty(env.Message, env.Detail), secret)
		if len(env.Errors) > 0 {
			if e.Code == "" {
				e.Code = stringify(env.Errors[0].Code)
			}
			if e.Message == "" {
				e.Message = scrub(env.Errors[0].Message, secret)
			}
		}
		if e.Message != "" {
			e.Snippet = "" // the structured message is strictly better than the raw body
		}
	}
	return e
}

// scrub removes the token from a string destined for an error or a log line.
func scrub(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprint(x)
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if s := strings.TrimSpace(x); s != "" {
			return s
		}
	}
	return ""
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
