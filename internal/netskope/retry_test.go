package netskope

import (
	"testing"
	"time"
)

// Options.Retry is exported, so a caller can hand over a policy with Base or Max
// left at zero. rand.Int64N panics on a non-positive bound, so that used to take
// the process down on the first retry.
func TestBackoffSurvivesZeroValuedPolicy(t *testing.T) {
	for _, p := range []RetryPolicy{
		{MaxRetries: 2},
		{MaxRetries: 2, Base: 500 * time.Millisecond},
		{MaxRetries: 2, Max: time.Minute},
	} {
		for attempt := 1; attempt <= p.MaxRetries; attempt++ {
			if d := p.backoff(attempt, 0); d < 0 {
				t.Errorf("%+v: backoff(%d) = %s", p, attempt, d)
			}
		}
		if d := p.backoff(1, 3*time.Second); d < 3*time.Second && p.Base == 0 {
			t.Errorf("%+v: a zero policy dropped the tenant's Retry-After: %s", p, d)
		}
	}
}
