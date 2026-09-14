// Package server wires the Netskope client into an MCP server and serves it over
// stdio or Streamable HTTP.
package server

import (
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/config"
	"github.com/lcleveland/netskope-mcp/internal/netskope"
	"github.com/lcleveland/netskope-mcp/internal/tools"
	"github.com/lcleveland/netskope-mcp/internal/version"
)

// New builds the MCP server and registers the enabled tools.
func New(cfg *config.Config, c *netskope.Client, log *slog.Logger) (*mcp.Server, int, error) {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "netskope-mcp",
		Title:   "Netskope",
		Version: version.Version,
	}, &mcp.ServerOptions{
		Logger:       log,
		Instructions: instructions(cfg),
	})

	n, err := tools.Register(s, c, tools.Options{
		AllowDestructive: cfg.AllowDestructive,
		Groups:           cfg.Groups,
		Logger:           log,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("registering tools: %w", err)
	}
	return s, n, nil
}

// instructions tell the client what this server is bound to and what it may do.
// The destructive line is stated either way: a model that knows deletion is off
// stops proposing it, and one that knows it is on has been told the stakes.
func instructions(cfg *config.Config) string {
	s := "Tools for a Netskope tenant at " + cfg.BaseURL.String() + ", over REST API v2.\n\n" +
		"Call netskope_tenant_info first if anything fails: it distinguishes a wrong tenant " +
		"URL from a rejected token from a token with too narrow a set of per-endpoint grants.\n\n" +
		"This is production security infrastructure. NPA policy rule order is significant and " +
		"the first match wins, so read the current ordering before inserting a rule. URL list " +
		"and custom category edits do not affect live traffic until they are deployed.\n\n"
	if cfg.AllowDestructive {
		return s + "Delete actions ARE enabled. Netskope has no undo: confirm with the user " +
			"before deleting a publisher, private app, policy rule or SCIM object."
	}
	return s + "Delete actions are disabled by the operator and are not registered. Do not " +
		"suggest workarounds; ask the user to change the server configuration if a deletion " +
		"is genuinely needed."
}
