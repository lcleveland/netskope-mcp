package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netskope-mcp/internal/config"
	"github.com/lcleveland/netskope-mcp/internal/version"
)

// shutdownGrace bounds how long in-flight requests get to finish on SIGTERM.
const shutdownGrace = 5 * time.Second

// Handler builds the HTTP mux: the MCP endpoint, optionally behind a bearer
// gate, plus a health endpoint.
func Handler(cfg *config.Config, s *mcp.Server, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Outside the auth gate on purpose. It exposes no tenant data, and systemd
	// and any monitoring must be able to probe liveness without being handed the
	// bearer token to do it.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok","version":"`+version.Version+`"}`)
	})

	var h http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{
			Logger:         log,
			SessionTimeout: 10 * time.Minute,
			// DisableLocalhostProtection stays false: the SDK's DNS-rebinding and
			// cross-origin checks are exactly what a loopback listener wants, since
			// a browser on this host could otherwise be made to drive the tenant.
		})

	if cfg.HTTPAuthToken != "" {
		h = auth.RequireBearerToken(staticVerifier(cfg.HTTPAuthToken), &auth.RequireBearerTokenOptions{
			AllowMissingExpiration: true,
		})(h)
	}
	mux.Handle(cfg.Path, h)
	mux.Handle(cfg.Path+"/", h)
	return mux
}

// staticVerifier checks a shared secret in constant time.
//
// Not OAuth, and not pretending to be: this is a bearer token an operator puts
// in a file so that a non-loopback listener is not simply open. If real OAuth is
// ever wanted, auth.ProtectedResourceMetadataHandler in the same package is the
// upgrade path.
func staticVerifier(want string) auth.TokenVerifier {
	w := []byte(want)
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(token), w) != 1 {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
	}
}

// ServeHTTP listens until the context is cancelled, then drains.
func ServeHTTP(ctx context.Context, cfg *config.Config, s *mcp.Server, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           Handler(cfg, s, log),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "path", cfg.Path, "authenticated", cfg.HTTPAuthToken != "")
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errc
	}
}
