package netskope

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RetryPolicy decides which failures are worth trying again.
type RetryPolicy struct {
	MaxRetries int
	Base       time.Duration
	Max        time.Duration
}

var DefaultRetryPolicy = RetryPolicy{MaxRetries: 3, Base: 500 * time.Millisecond, Max: 60 * time.Second}

// idempotent reports whether repeating the verb is harmless. This is the whole
// reason retry is not a one-liner: retrying a POST that did reach the tenant but
// whose response was lost creates a second publisher, and nothing downstream can
// tell that from a first one.
func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

// retryStatus decides on a response we did receive. 429 is always safe to retry
// regardless of verb: it means the tenant rejected the request before acting on
// it. 5xx is only safe when the verb is idempotent.
func (p RetryPolicy) retryStatus(method string, status, attempt int) bool {
	if attempt >= p.MaxRetries {
		return false
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	// 500 is included deliberately. It is usually a deterministic application
	// fault not worth repeating, but Netskope's Advanced Analytics routes answer
	// a transient backend hiccup with one, and the retry clears it: observed on
	// /reporting/aa/reports, which 500d, then timed out, then returned 61 reports
	// untouched. The idempotent guard is what keeps this safe.
	switch status {
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return idempotent(method)
	}
	return false
}

// retryTransport decides on an error where we never saw a response. We cannot
// tell "connection refused" (never arrived) from "connection reset after the
// request was written" (may have been acted on), so only idempotent verbs retry.
func (p RetryPolicy) retryTransport(method string, attempt int) bool {
	return attempt < p.MaxRetries && idempotent(method)
}

// backoff is exponential with full jitter, floored by any Retry-After the tenant
// gave us. Full jitter rather than fixed backoff because several tools firing at
// once would otherwise retry in lockstep and re-trip the same rate limit.
func (p RetryPolicy) backoff(attempt int, retryAfter time.Duration) time.Duration {
	d := p.Base << (attempt - 1)
	if d > p.Max {
		d = p.Max
	}
	if d <= 0 {
		// A caller-supplied policy may leave Base or Max at zero, and rand.Int64N
		// panics on a non-positive bound. Retry immediately rather than crash.
		return retryAfter
	}
	d = time.Duration(rand.Int64N(int64(d)) + int64(d)/2)
	if retryAfter > d {
		d = retryAfter
	}
	if d > p.Max {
		d = p.Max
	}
	return d
}

// lastRetryAfter pulls the tenant's own backoff instruction out of the previous
// failure, if it was one.
func lastRetryAfter(err error) time.Duration {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.RetryAfter
	}
	return 0
}

// parseRetryAfter reads RFC 9110 Retry-After (delay-seconds or HTTP-date) and
// falls back to the RateLimit-Reset hint Netskope sends alongside 429.
func parseRetryAfter(h http.Header, now time.Time) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := t.Sub(now); d > 0 {
				return d
			}
		}
	}
	if v := h.Get("RateLimit-Reset"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

// limiter paces requests. A fixed rate alone is a guess, so it also reacts to
// the tenant's RateLimit-Remaining: when the budget runs low it stops handing
// out tokens until the window resets, which keeps a chatty model from spending
// the whole allowance on one tools/call.
type limiter struct {
	rl *rate.Limiter

	mu    sync.Mutex
	until time.Time
}

func newLimiter(rps float64, burst int) *limiter {
	return &limiter{rl: rate.NewLimiter(rate.Limit(rps), burst)}
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	pause := time.Until(l.until)
	l.mu.Unlock()
	if pause > 0 {
		if err := sleepCtx(ctx, pause); err != nil {
			return err
		}
	}
	return l.rl.Wait(ctx)
}

// observe reads the RateLimit-* family off a response. Netskope reports the
// remaining budget for the current window; at zero we hold every caller until
// the window resets rather than spending the next N requests collecting 429s.
func (l *limiter) observe(h http.Header) {
	rem := h.Get("RateLimit-Remaining")
	if rem == "" {
		return
	}
	n, err := strconv.Atoi(rem)
	if err != nil || n > 0 {
		return
	}
	reset := parseRetryAfter(h, time.Now())
	if reset <= 0 {
		reset = time.Second
	}
	l.mu.Lock()
	if t := time.Now().Add(reset); t.After(l.until) {
		l.until = t
	}
	l.mu.Unlock()
}
