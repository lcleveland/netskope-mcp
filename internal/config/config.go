// Package config resolves the server's runtime configuration from flags, the
// environment and systemd credentials.
//
// The one rule that shapes everything here: the API token must never reach argv
// or the process environment when running as a service. `ps` shows argv to every
// user on the box, and /proc/<pid>/environ outlives the process in a core dump.
// So there is deliberately no --api-token flag -- only --api-token-file -- and
// the NixOS module passes the token through systemd's credential directory,
// which is a 0400 file on a private tmpfs.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Mode selects the MCP transport.
type Mode string

const (
	ModeStdio Mode = "stdio"
	ModeHTTP  Mode = "http"
)

// DefaultGroups are the tool families registered when --tool-groups is not
// given. Narrowing them is not only about safety: every registered tool costs
// context in the client's tools/list, so a tenant that only cares about NPA
// should say so.
var DefaultGroups = []string{"core", "npa", "policy", "events", "scim", "reporting"}

// Groups are every family --tool-groups accepts. Anything here but not in
// DefaultGroups is opt-in and has to be named explicitly, because it only works
// on a tenant that has been set up for it, and a tool that 403s on every call is
// worse than no tool at all:
//
//   - "internetaccess": the real-time protection policy tools. That API is
//     still in development at Netskope and has to be enabled per tenant by
//     Netskope, so until it is, every call 403s with what reads like a role
//     problem and no role can fix it. Turn it on once the routes answer.
var Groups = append(slices.Clone(DefaultGroups), "internetaccess")

// Config is the fully resolved configuration. Its String and LogValue methods
// redact the secrets, so logging a Config by accident cannot leak one.
type Config struct {
	BaseURL          *url.URL
	Token            string
	HTTPAuthToken    string
	Mode             Mode
	Addr             string
	Path             string
	AllowDestructive bool
	Groups           []string
	RequestTimeout   time.Duration
	LogLevel         slog.Level
	TokenSource      string // where Token came from, for the startup log line
}

// LogValue implements slog.LogValuer so that slog.Any("cfg", cfg) cannot print a
// token. Keep this in sync with the struct.
func (c *Config) LogValue() slog.Value {
	base := ""
	if c.BaseURL != nil {
		base = c.BaseURL.String()
	}
	return slog.GroupValue(
		slog.String("base_url", base),
		slog.String("mode", string(c.Mode)),
		slog.String("addr", c.Addr),
		slog.String("path", c.Path),
		slog.Bool("allow_destructive", c.AllowDestructive),
		slog.String("groups", strings.Join(c.Groups, ",")),
		slog.Duration("request_timeout", c.RequestTimeout),
		slog.String("token_source", c.TokenSource),
		slog.String("token", "***"),
		slog.String("http_auth_token", redactPresence(c.HTTPAuthToken)),
	)
}

func (c *Config) String() string { return c.LogValue().String() }

func redactPresence(s string) string {
	if s == "" {
		return "<unset>"
	}
	return "***"
}

// ErrVersion is returned by Load when --version was passed; main prints the
// version and exits 0 rather than treating it as a failure.
var ErrVersion = errors.New("version requested")

type flags struct {
	stdio            bool
	http             bool
	tenant           string
	baseURL          string
	apiTokenFile     string
	httpTokenFile    string
	addr             string
	path             string
	allowDestructive bool
	toolGroups       string
	requestTimeout   time.Duration
	logLevel         string
	version          bool
}

// Load resolves configuration from args and env. Both are injected rather than
// read from globals so the tests can drive the whole precedence table without
// touching the real process.
func Load(args []string, env func(string) string, stderr io.Writer) (*Config, error) {
	var f flags
	fs := flag.NewFlagSet("netskope-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.BoolVar(&f.stdio, "stdio", false, "serve MCP over stdio (default when neither --stdio nor --http is given)")
	fs.BoolVar(&f.http, "http", false, "serve MCP over Streamable HTTP")
	fs.StringVar(&f.tenant, "tenant", "", "Netskope tenant short name, e.g. \"acme\" for https://acme.goskope.com")
	fs.StringVar(&f.baseURL, "base-url", "", "full tenant base URL; use instead of --tenant for regional tenants")
	fs.StringVar(&f.apiTokenFile, "api-token-file", "", "path to a file holding the Netskope REST API v2 token")
	fs.StringVar(&f.httpTokenFile, "http-auth-token-file", "", "path to a file holding the bearer token required on the HTTP endpoint")
	fs.StringVar(&f.addr, "addr", "127.0.0.1:8231", "listen address for --http")
	fs.StringVar(&f.path, "path", "/mcp", "URL path the MCP endpoint is mounted at")
	fs.BoolVar(&f.allowDestructive, "allow-destructive", false, "register delete actions; off by default")
	fs.StringVar(&f.toolGroups, "tool-groups", strings.Join(DefaultGroups, ","),
		"comma-separated tool groups to register; known groups are "+strings.Join(Groups, ", ")+
			", of which any not in the default must be named explicitly")
	fs.DurationVar(&f.requestTimeout, "request-timeout", 30*time.Second, "timeout for a single Netskope API request")
	fs.StringVar(&f.logLevel, "log-level", "info", "debug, info, warn or error")
	fs.BoolVar(&f.version, "version", false, "print the version and exit")

	// There is no --api-token. Say why, so nobody adds one back.
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage of netskope-mcp:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nThe API token is never taken on the command line: argv is world-readable\n"+
			"through ps. Supply it with --api-token-file, NETSKOPE_API_TOKEN_FILE, or\n"+
			"systemd's LoadCredential= (which the NixOS module uses).\n")
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if f.version {
		return nil, ErrVersion
	}

	cfg := &Config{
		Addr:             f.addr,
		Path:             f.path,
		AllowDestructive: f.allowDestructive,
		RequestTimeout:   f.requestTimeout,
	}

	if f.stdio && f.http {
		return nil, errors.New("--stdio and --http are mutually exclusive")
	}
	cfg.Mode = ModeStdio
	if f.http {
		cfg.Mode = ModeHTTP
	}

	var err error
	if cfg.BaseURL, err = resolveBaseURL(f, env); err != nil {
		return nil, err
	}
	if cfg.Groups, err = resolveGroups(f.toolGroups); err != nil {
		return nil, err
	}
	if cfg.LogLevel, err = parseLevel(f.logLevel, flagSet(fs, "log-level"), env); err != nil {
		return nil, err
	}
	if cfg.Token, cfg.TokenSource, err = resolveToken(f.apiTokenFile, env); err != nil {
		return nil, err
	}
	if cfg.HTTPAuthToken, err = resolveHTTPToken(f.httpTokenFile, env); err != nil {
		return nil, err
	}
	if cfg.Path == "" || cfg.Path[0] != '/' {
		return nil, fmt.Errorf("--path must begin with %q, got %q", "/", cfg.Path)
	}
	return cfg, nil
}

func resolveBaseURL(f flags, env func(string) string) (*url.URL, error) {
	tenant, base := f.tenant, f.baseURL
	if tenant == "" {
		tenant = env("NETSKOPE_TENANT")
	}
	if base == "" {
		base = env("NETSKOPE_BASE_URL")
	}
	switch {
	case tenant != "" && base != "":
		return nil, errors.New("set exactly one of --tenant and --base-url, not both")
	case tenant == "" && base == "":
		return nil, errors.New("no tenant configured: pass --tenant <name> (or --base-url for a regional tenant)")
	case base == "":
		base = "https://" + tenant + ".goskope.com"
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parsing tenant URL %q: %w", base, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("tenant URL %q must be absolute, e.g. https://acme.goskope.com", base)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func resolveGroups(s string) ([]string, error) {
	var out []string
	for _, g := range strings.Split(s, ",") {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if !slices.Contains(Groups, g) {
			return nil, fmt.Errorf("unknown tool group %q; known groups are %s", g, strings.Join(Groups, ", "))
		}
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("--tool-groups is empty: the server would expose no tools")
	}
	return out, nil
}

// flagSet reports whether a flag was actually passed, as opposed to sitting at
// its default. Comparing against the default value cannot tell the two apart,
// and an explicit --log-level info must beat the environment.
func flagSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

func parseLevel(s string, explicit bool, env func(string) string) (slog.Level, error) {
	if v := env("NETSKOPE_MCP_LOG_LEVEL"); v != "" && !explicit {
		s = v
	}
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q; want debug, info, warn or error", s)
}

// resolveToken walks the credential ladder. The order is deliberate: an explicit
// flag beats the environment, and systemd's credential directory is the fallback
// precisely because the unit passes nothing else.
func resolveToken(flagPath string, env func(string) string) (token, source string, err error) {
	if flagPath != "" {
		t, err := readSecretFile(flagPath)
		return t, "--api-token-file", err
	}
	if p := env("NETSKOPE_API_TOKEN_FILE"); p != "" {
		t, err := readSecretFile(p)
		return t, "NETSKOPE_API_TOKEN_FILE", err
	}
	if t := strings.TrimSpace(env("NETSKOPE_API_TOKEN")); t != "" {
		// Supported because a hand-written .mcp.json will reach for it, but the
		// value is now in this process's environ, readable by anything that can
		// read /proc/self/environ. main logs a warning about this.
		return t, "NETSKOPE_API_TOKEN", nil
	}
	if p, ok := credentialPath(env, "api-token"); ok {
		t, err := readSecretFile(p)
		return t, "systemd credential", err
	}
	return "", "", errors.New("no Netskope API token found: pass --api-token-file, set NETSKOPE_API_TOKEN_FILE " +
		"or NETSKOPE_API_TOKEN, or run under a systemd unit with LoadCredential=api-token:<path>")
}

func resolveHTTPToken(flagPath string, env func(string) string) (string, error) {
	if flagPath != "" {
		return readSecretFile(flagPath)
	}
	if p := env("NETSKOPE_HTTP_AUTH_TOKEN_FILE"); p != "" {
		return readSecretFile(p)
	}
	if p, ok := credentialPath(env, "http-token"); ok {
		return readSecretFile(p)
	}
	return "", nil // optional: an unauthenticated loopback listener is the default
}
