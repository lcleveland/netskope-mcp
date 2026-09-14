package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeStdio runs the server over stdio until the context is cancelled.
//
// The invariant that makes this work: nothing in the process may write to stdout
// but the transport. stdout IS the JSON-RPC framing, and a single stray Println
// corrupts it into a client-side parse error that points nowhere near the cause.
// main enforces this by pinning slog to stderr before anything else runs.
func ServeStdio(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}
