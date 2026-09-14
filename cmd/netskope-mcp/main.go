// Command netskope-mcp serves the Netskope REST API v2 as MCP tools.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lcleveland/netskope-mcp/internal/config"
	"github.com/lcleveland/netskope-mcp/internal/netskope"
	"github.com/lcleveland/netskope-mcp/internal/server"
	"github.com/lcleveland/netskope-mcp/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		if errors.Is(err, config.ErrVersion) {
			fmt.Println(version.Version)
			return
		}
		// Never os.Stdout: in stdio mode it carries the JSON-RPC framing.
		fmt.Fprintln(os.Stderr, "netskope-mcp: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string, env func(string) string) error {
	cfg, err := config.Load(args, env, os.Stderr)
	if err != nil {
		return err
	}

	// stderr unconditionally, and set before anything else can log. stdout is
	// the stdio transport's wire format; writing a single line to it there
	// corrupts the session.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	if cfg.TokenSource == "NETSKOPE_API_TOKEN" {
		log.Warn("the API token was read from the environment, where anything that can read " +
			"/proc/self/environ can see it; prefer --api-token-file or systemd's LoadCredential=")
	}

	client, err := netskope.New(netskope.Options{
		BaseURL: cfg.BaseURL,
		Token:   cfg.Token,
		Timeout: cfg.RequestTimeout,
		Logger:  log,
	})
	if err != nil {
		return err
	}

	srv, n, err := server.New(cfg, client, log)
	if err != nil {
		return err
	}
	log.Info("starting",
		"version", version.Version, "mode", string(cfg.Mode), "tools", n,
		"tenant", cfg.BaseURL.String(), "groups", cfg.Groups,
		"allow_destructive", cfg.AllowDestructive, "token_source", cfg.TokenSource)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Mode == config.ModeHTTP {
		return server.ServeHTTP(ctx, cfg, srv, log)
	}
	return server.ServeStdio(ctx, srv)
}
