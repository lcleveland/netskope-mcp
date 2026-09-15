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

// applyListDefaults supplies a bound when the caller did not, so that an
// unqualified "list" is bounded at the tenant rather than in our own memory.
// param is the collection's own name for it -- SCIM says "count", not "limit".
func applyListDefaults(q url.Values, param string, max int) {
	if q.Get(param) == "" {
		q.Set(param, strconv.Itoa(max))
	}
	// SCIM honours `count` only when `startIndex` comes with it. Sent alone it is
	// ignored outright and the tenant returns every record, which is how a bound
	// we thought we were setting turned into a 355-user response.
	if param == "count" && q.Get("startIndex") == "" {
		q.Set("startIndex", "1")
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

// listKeys are the keys a collection arrives under. Netskope's own APIs use
// "data"; the SCIM surface uses "Resources", and missing that meant neither cap
// applied to /api/v2/scim/Users -- the tenant's entire user list reached the
// model, over the client's own response limit.
var listKeys = []string{"data", "Resources"}

// capResult trims a list response to the item and byte budget.
//
// Netskope v2 is not consistent about its envelope: some collections return a
// bare array, some {"data": [...]}, some {"data": [...], "total": N}, and SCIM
// returns {"Resources": [...], "totalResults": N}. Handle the shapes we have
// seen and pass anything else through untouched rather than guessing and
// silently dropping fields.
func capResult(v any, max int) any {
	switch x := v.(type) {
	case []any:
		items, cut := capSlice(x, max)
		return fit(nil, "data", items, len(x), cut)

	case map[string]any:
		for _, key := range listKeys {
			arr, ok := x[key].([]any)
			if !ok {
				continue
			}
			items, cut := capSlice(arr, max)
			return fit(x, key, items, totalOf(x, len(arr)), cut)
		}
		// A single object, or an envelope we have not seen. Nothing to trim.
		return x
	}
	return v
}

// fit applies the byte budget on top of the item budget and, if either bit, wraps
// the result so the model is told what it got is partial.
//
// The byte pass is the one that earns its keep: the item cap alone lets a handful
// of policy objects with large embedded rule sets through, and item size varies by
// two orders of magnitude across Netskope resources. envelope is the original map
// for a {"data": ...} response, or nil for a bare array; key is the envelope key
// the items came from, so a capped SCIM result replaces "Resources" instead of
// growing a second, shorter list beside it.
func fit(envelope map[string]any, key string, items []any, total int, cut bool) any {
	for len(items) > 1 && size(items) > maxBytes {
		items = items[:len(items)/2]
		cut = true
	}
	if !cut {
		if envelope != nil {
			return envelope
		}
		// MCP requires structuredContent to be an object, so a top-level array
		// response (/api/v2/policy/urllist) has to be wrapped or the client
		// rejects the whole result. The cut path below already returns a map.
		return map[string]any{"data": items}
	}
	out := make(map[string]any, len(envelope)+2)
	for k, v := range envelope {
		out[k] = v
	}
	out[key] = items
	out["_truncation"] = truncation{true, len(items), total, truncNote}
	return out
}

func capSlice(xs []any, max int) ([]any, bool) {
	if len(xs) <= max {
		return xs, false
	}
	return xs[:max], true
}

func totalOf(m map[string]any, fallback int) int {
	for _, k := range []string{"total", "totalResults"} {
		if t, ok := m[k].(float64); ok {
			return int(t)
		}
	}
	return fallback
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
