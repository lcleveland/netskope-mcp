// Package tools registers the MCP tool surface over the Netskope client.
//
// Most Netskope resources are the same CRUD shape over a different path, so they
// are described as data (a []Resource table) and share one handler. That keeps
// the tool count at roughly one per resource instead of one per endpoint+verb:
// a model picks well from ~20 well-described tools and poorly from ~90.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// Action is the operation a resource tool performs.
type Action string

const (
	ActionList   Action = "list"
	ActionGet    Action = "get"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// Destructive reports whether an action can irreversibly remove tenant
// configuration. Only these are gated behind --allow-destructive.
func (a Action) Destructive() bool { return a == ActionDelete }

// Write reports whether an action changes tenant state at all. It drives the
// ReadOnlyHint annotation and the audit log line.
func (a Action) Write() bool {
	return a == ActionCreate || a == ActionUpdate || a == ActionDelete
}

// Input is the argument shape shared by every resource tool. The JSON schema the
// model sees is derived from this struct, then narrowed per tool so that the
// action enum lists only the actions that tool actually permits.
type Input struct {
	Action Action            `json:"action" jsonschema:"the operation to perform; see the tool description for which are available"`
	ID     string            `json:"id,omitempty" jsonschema:"the object id; required for get, update and delete"`
	Body   json.RawMessage   `json:"body,omitempty" jsonschema:"a JSON object holding the fields to set; required for create and update"`
	Query  map[string]string `json:"query,omitempty" jsonschema:"extra query parameters such as fields, filter, limit or offset"`
}

// Resource describes one Netskope collection as a tool.
type Resource struct {
	// Name is the MCP tool name, e.g. "netskope_publishers".
	Name string
	// Group gates registration via --tool-groups.
	Group string
	// Title is the human-facing label.
	Title string
	// Description is the only specification the model gets for this resource:
	// say what it is, name the endpoint, and list the body fields that matter.
	Description string
	// Collection is the path of the collection, e.g. "/api/v2/infrastructure/publishers".
	Collection string
	// ItemPath builds the path for a single object. Nil means Collection/<id>.
	ItemPath func(id string) string
	// Actions are the actions this resource supports, before destructive filtering.
	Actions []Action
	// MaxItems caps how many records a list may return. Zero uses the default.
	MaxItems int
	// LimitParam is the query parameter that bounds a list at the tenant. Zero
	// value means "limit"; the SCIM collections page with "count" instead, and
	// sending them "limit" bounds nothing at all.
	LimitParam string
}

func (r Resource) itemPath(id string) string {
	if r.ItemPath != nil {
		return r.ItemPath(id)
	}
	return strings.TrimRight(r.Collection, "/") + "/" + url.PathEscape(id)
}

// route maps an action onto an HTTP verb and path, and validates that the input
// carries what that action needs. Doing the validation here rather than trusting
// the schema matters: schema validation is advisory, and a client may skip it.
func (r Resource) route(in Input) (method, path string, body any, err error) {
	switch in.Action {
	case ActionList:
		return http.MethodGet, r.Collection, nil, nil
	case ActionGet:
		if in.ID == "" {
			return "", "", nil, fmt.Errorf("action %q requires `id`", in.Action)
		}
		return http.MethodGet, r.itemPath(in.ID), nil, nil
	case ActionCreate:
		if len(in.Body) == 0 {
			return "", "", nil, fmt.Errorf("action %q requires `body`", in.Action)
		}
		return http.MethodPost, r.Collection, in.Body, nil
	case ActionUpdate:
		if in.ID == "" {
			return "", "", nil, fmt.Errorf("action %q requires `id`", in.Action)
		}
		if len(in.Body) == 0 {
			return "", "", nil, fmt.Errorf("action %q requires `body`", in.Action)
		}
		return http.MethodPut, r.itemPath(in.ID), in.Body, nil
	case ActionDelete:
		if in.ID == "" {
			return "", "", nil, fmt.Errorf("action %q requires `id`", in.Action)
		}
		return http.MethodDelete, r.itemPath(in.ID), nil, nil
	}
	return "", "", nil, fmt.Errorf("unknown action %q", in.Action)
}

// call runs one resource action. allowed is the post-filter action list, closed
// over at registration time: this is the check that actually enforces
// --allow-destructive, because it cannot be talked around the way a prompt can.
func (r Resource) call(ctx context.Context, c *netskope.Client, allowed []Action, in Input) (any, error) {
	if !slices.Contains(allowed, in.Action) {
		return nil, fmt.Errorf("action %q is not enabled for %s; available actions are %s",
			in.Action, r.Name, joinActions(allowed))
	}
	method, path, body, err := r.route(in)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	for k, v := range in.Query {
		q.Set(k, v)
	}
	if in.Action == ActionList {
		applyListDefaults(q, r.limitParam(), r.maxItems())
	}

	var out any
	if err := c.Do(ctx, method, path, q, body, &out); err != nil {
		return nil, err
	}
	if in.Action == ActionList {
		return capResult(out, r.maxItems()), nil
	}
	return out, nil
}

func (r Resource) limitParam() string {
	if r.LimitParam != "" {
		return r.LimitParam
	}
	return "limit"
}

func (r Resource) maxItems() int {
	if r.MaxItems > 0 {
		return r.MaxItems
	}
	return defaultMaxItems
}

func joinActions(as []Action) string {
	s := make([]string, len(as))
	for i, a := range as {
		s[i] = string(a)
	}
	return strings.Join(s, ", ")
}
