package netskope

import (
	"fmt"
	"strings"
	"time"
)

// maxSnippet bounds how much of a failing response body rides along in an error.
// Enough to diagnose, small enough not to blow a context window when the error
// is handed back to a model as tool output.
const maxSnippet = 2048

// APIError is a non-2xx response from the tenant.
//
// Path deliberately carries no query string: event-search filters and SCIM
// filters routinely contain usernames and email addresses, and errors end up in
// logs and in model context.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Code    string
	Message string
	Snippet string
	// RetryAfter is the tenant's own backoff instruction, when it sent one.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "netskope %s %s: %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	if hint := e.hint(); hint != "" {
		fmt.Fprintf(&b, ": %s", hint)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	} else if e.Snippet != "" {
		fmt.Fprintf(&b, ": %s", e.Snippet)
	}
	return b.String()
}

// hint turns a status code into something a model can act on rather than
// surface verbatim to a user. The 403 case is the one that earns its keep:
// Netskope v2 tokens carry per-endpoint grants, so a 403 almost always means
// "the token is fine, this endpoint just was not ticked" -- which looks
// identical to a bug unless the message says otherwise.
func (e *APIError) hint() string {
	switch e.Status {
	case 401:
		return "the API token was rejected; check that apiTokenFile holds a current REST API v2 token"
	case 403:
		return "the token has no grant for this endpoint; add it under Settings > Tools > REST API v2"
	case 404:
		return "no such object"
	case 429:
		return "rate limited by the tenant after exhausting retries; narrow the query or lower --rate-limit"
	}
	if e.Status >= 500 {
		return "the tenant returned a server error"
	}
	return ""
}

// truncate bounds a response body for inclusion in an error, and scrubs the
// token out of it first.
//
// The scrub is not paranoia: a reverse proxy, a WAF, or the tenant's own error
// page can echo request headers back in a failure body, and that body ends up in
// the logs and in a model's context. Covered by TestErrorDoesNotLeakToken.
func truncate(b []byte, secret string) string {
	s := strings.TrimSpace(string(b))
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "***")
	}
	if len(s) > maxSnippet {
		return s[:maxSnippet] + "... (truncated)"
	}
	return s
}
