package tools

import (
	"encoding/json"
	"net/url"
	"strconv"
)

const (
	// defaultMaxItems bounds a list response. A tenant can hold tens of
	// thousands of private apps; handing all of them to a model costs real money
	// and usually answers nothing that a filtered query would not.
	defaultMaxItems = 200

	// maxBytes bounds the encoded result regardless of item count, because item
	// size varies by two orders of magnitude across Netskope resources.
	maxBytes = 60 << 10
)

// applyListDefaults supplies a limit when the caller did not, so that an
// unqualified "list" is bounded at the tenant rather than in our own memory.
func applyListDefaults(q url.Values, max int) {
	if q.Get("limit") == "" {
		q.Set("limit", strconv.Itoa(max))
	}
}

// truncation is appended to a capped result so the model is told that what it
// received is partial, and what to do about it.
type truncation struct {
	Truncated bool   `json:"truncated"`
	Returned  int    `json:"returned"`
	Total     int    `json:"total,omitempty"`
	Note      string `json:"note"`
}

const truncNote = "the result was truncated; narrow it with query.filter, query.limit or query.offset"

// capResult trims a list response to the item and byte budget.
//
// Netskope v2 is not consistent about its envelope: some collections return a
// bare array, some {"data": [...]}, some {"data": [...], "total": N}. Handle the
// shapes we have seen and pass anything else through untouched rather than
// guessing and silently dropping fields.
func capResult(v any, max int) any {
	switch x := v.(type) {
	case []any:
		items, cut := capSlice(x, max)
		if !cut {
			return x
		}
		return map[string]any{"data": items, "_truncation": truncation{true, len(items), len(x), truncNote}}

	case map[string]any:
		raw, ok := x["data"]
		if !ok {
			return capBytes(x, max)
		}
		arr, ok := raw.([]any)
		if !ok {
			return capBytes(x, max)
		}
		items, cut := capSlice(arr, max)
		if !cut {
			return capBytes(x, max)
		}
		out := make(map[string]any, len(x)+1)
		for k, val := range x {
			out[k] = val
		}
		out["data"] = items
		out["_truncation"] = truncation{true, len(items), totalOf(x, len(arr)), truncNote}
		return out
	}
	return v
}

func capSlice(xs []any, max int) ([]any, bool) {
	if len(xs) <= max {
		return xs, false
	}
	return xs[:max], true
}

func totalOf(m map[string]any, fallback int) int {
	if t, ok := m["total"].(float64); ok {
		return int(t)
	}
	return fallback
}

// capBytes is the backstop for a response that is within the item budget but
// still enormous -- a handful of policy objects with large embedded rule sets,
// say. It halves the item count until the encoding fits.
func capBytes(v any, max int) any {
	if size(v) <= maxBytes {
		return v
	}
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	arr, ok := m["data"].([]any)
	if !ok || len(arr) == 0 {
		return v
	}
	n := len(arr)
	for n > 1 && size(arr[:n]) > maxBytes {
		n /= 2
	}
	out := make(map[string]any, len(m)+1)
	for k, val := range m {
		out[k] = val
	}
	out["data"] = arr[:n]
	out["_truncation"] = truncation{true, n, totalOf(m, len(arr)), truncNote}
	return out
}

func size(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// limitOne is the smallest possible read, for probes.
func limitOne() url.Values {
	q := url.Values{}
	q.Set("limit", "1")
	return q
}
