package server_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioEndToEnd builds the real binary and drives it the way a client does:
// spawn it, speak MCP over its stdin/stdout, list the tools.
//
// This is the only test that proves the stdio path works at all, and it is worth
// its cost: a stray write to stdout anywhere in the process corrupts the framing,
// and nothing else in the suite would catch that.
func TestStdioEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	// buildGoModule's checkPhase has the toolchain on PATH, but a bare
	// `go test` from an environment without it should skip rather than fail.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "netskope-mcp")

	build := exec.Command("go", "build", "-o", bin, "github.com/lcleveland/netskope-mcp/cmd/netskope-mcp")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, out)
	}

	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.Command(bin, "--stdio", "--tenant", "acme", "--api-token-file", tokenFile, "--log-level", "error")
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting over stdio: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list over stdio: %v", err)
	}
	if len(res.Tools) < 10 {
		t.Fatalf("got %d tools over stdio, want the full surface", len(res.Tools))
	}
	t.Logf("stdio handshake succeeded, %d tools", len(res.Tools))
}
