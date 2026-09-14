package tools

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/netskope"
)

// Options controls which tools get registered.
type Options struct {
	// AllowDestructive registers delete actions. Off by default.
	AllowDestructive bool
	// Groups limits registration to these tool groups. Empty means all.
	Groups []string
	Logger *slog.Logger
}

func (o Options) enabled(group string) bool {
	return len(o.Groups) == 0 || slices.Contains(o.Groups, group)
}

// All returns the full resource table.
func All() []Resource {
	var rs []Resource
	rs = append(rs, npaResources...)
	rs = append(rs, policyResources...)
	rs = append(rs, scimResources...)
	rs = append(rs, reportingResources...)
	return rs
}

// Register adds every enabled tool to the server and reports how many it added.
func Register(s *mcp.Server, c *netskope.Client, o Options) (int, error) {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	n := 0
	if o.enabled("core") {
		registerTenantInfo(s, c)
		n++
	}
	for _, r := range All() {
		if !o.enabled(r.Group) {
			continue
		}
		added, err := registerResource(s, c, r, o)
		if err != nil {
			return n, fmt.Errorf("registering %s: %w", r.Name, err)
		}
		if added {
			n++
		}
	}
	if o.enabled("events") {
		n += registerEvents(s, c)
	}
	return n, nil
}

// registerResource is where --allow-destructive is enforced, and it is enforced
// three times over on purpose:
//
//  1. The action enum in the JSON schema omits the forbidden actions, so a
//     compliant client rejects the call before it is ever sent and the model's
//     own sampling is steered away from it.
//  2. The generated description lists only the permitted actions, so the model is
//     never told the capability exists. There is no instruction to argue with.
//  3. The handler closes over the filtered list and refuses anything outside it.
//
// Only (3) is load-bearing -- schema validation is advisory and a client may
// skip it -- but (1) and (2) are what stop the model from trying in the first
// place, which is the difference between a refusal the user sees and a refusal
// that never needed to happen.
func registerResource(s *mcp.Server, c *netskope.Client, r Resource, o Options) (bool, error) {
	actions := slices.Clone(r.Actions)
	if !o.AllowDestructive {
		actions = slices.DeleteFunc(actions, Action.Destructive)
	}
	if len(actions) == 0 {
		// Nothing left to do: registering a tool that can only fail would waste
		// context in every tools/list for no benefit.
		o.Logger.Debug("skipping tool with no enabled actions", "tool", r.Name)
		return false, nil
	}

	schema, err := jsonschema.For[Input](nil)
	if err != nil {
		return false, fmt.Errorf("deriving input schema: %w", err)
	}
	prop, ok := schema.Properties["action"]
	if !ok || prop == nil {
		return false, fmt.Errorf("input schema has no `action` property")
	}
	prop.Enum = make([]any, len(actions))
	for i, a := range actions {
		prop.Enum[i] = string(a)
	}

	readOnly := !slices.ContainsFunc(actions, Action.Write)
	destructive := slices.ContainsFunc(actions, Action.Destructive)

	mcp.AddTool(s, &mcp.Tool{
		Name:        r.Name,
		Title:       r.Title,
		Description: r.Description + actionHelp(actions),
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    readOnly,
			DestructiveHint: &destructive,
			OpenWorldHint:   ptr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in Input) (*mcp.CallToolResult, any, error) {
		out, err := r.call(ctx, c, actions, in)
		if err != nil {
			// A tenant-side failure is a result the model should see and reason
			// about, not a protocol error: return IsError rather than err, so the
			// 403-means-missing-grant hint actually reaches it.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		if in.Action.Write() {
			// Audit the fact of the write, never its body: bodies carry user
			// identities and policy internals.
			o.Logger.Info("netskope write",
				"tool", r.Name, "action", string(in.Action), "id", in.ID)
		}
		return nil, out, nil
	})
	return true, nil
}

func actionHelp(actions []Action) string {
	var b strings.Builder
	b.WriteString("\n\nAvailable actions: ")
	b.WriteString(joinActions(actions))
	b.WriteString(".")
	if !slices.ContainsFunc(actions, Action.Destructive) {
		b.WriteString(" Deleting through this server is disabled by the operator.")
	}
	return b.String()
}

func ptr[T any](v T) *T { return &v }
