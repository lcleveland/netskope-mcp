package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeToken(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTokenPrecedence(t *testing.T) {
	flagFile := writeToken(t, "from-flag\n")
	envFile := writeToken(t, "from-env-file")

	// A systemd credential directory, as LoadCredential= would materialise it.
	credDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(credDir, "api-token"), []byte("from-credential\n"), 0o400); err != nil {
		t.Fatal(err)
	}

	full := map[string]string{
		"NETSKOPE_API_TOKEN_FILE": envFile,
		"NETSKOPE_API_TOKEN":      "from-env",
		"CREDENTIALS_DIRECTORY":   credDir,
	}
	drop := func(keys ...string) map[string]string {
		m := map[string]string{}
		for k, v := range full {
			m[k] = v
		}
		for _, k := range keys {
			delete(m, k)
		}
		return m
	}

	for _, tc := range []struct {
		name       string
		args       []string
		env        map[string]string
		wantToken  string
		wantSource string
	}{
		{"flag wins", []string{"--api-token-file", flagFile}, full, "from-flag", "--api-token-file"},
		{"env file next", nil, full, "from-env-file", "NETSKOPE_API_TOKEN_FILE"},
		{"env value next", nil, drop("NETSKOPE_API_TOKEN_FILE"), "from-env", "NETSKOPE_API_TOKEN"},
		{"credential last", nil, drop("NETSKOPE_API_TOKEN_FILE", "NETSKOPE_API_TOKEN"), "from-credential", "systemd credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.env["NETSKOPE_TENANT"] = "acme"
			cfg, err := Load(tc.args, envOf(tc.env), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Token != tc.wantToken {
				t.Errorf("token = %q, want %q", cfg.Token, tc.wantToken)
			}
			if cfg.TokenSource != tc.wantSource {
				t.Errorf("source = %q, want %q", cfg.TokenSource, tc.wantSource)
			}
		})
	}
}

// A token file that ends in a newline is the normal case, not the exception:
// sops-nix, agenix and every text editor produce one. Not trimming it yields a
// 401 whose cause is invisible.
func TestTokenTrimsWhitespace(t *testing.T) {
	cfg, err := Load([]string{"--api-token-file", writeToken(t, "  tok\n\n")},
		envOf(map[string]string{"NETSKOPE_TENANT": "acme"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "tok" {
		t.Fatalf("token = %q, want %q", cfg.Token, "tok")
	}
}

func TestEmptyTokenFileRejected(t *testing.T) {
	_, err := Load([]string{"--api-token-file", writeToken(t, "\n  \n")},
		envOf(map[string]string{"NETSKOPE_TENANT": "acme"}), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want an empty-credential error, got %v", err)
	}
}

func TestNoTokenIsAnError(t *testing.T) {
	_, err := Load(nil, envOf(map[string]string{"NETSKOPE_TENANT": "acme"}), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no Netskope API token") {
		t.Fatalf("want a missing-token error, got %v", err)
	}
}

// There must never be a way to put the token in argv, where ps exposes it to
// every user on the box. If someone adds an --api-token flag, this fails.
func TestNoApiTokenFlag(t *testing.T) {
	_, err := Load([]string{"--api-token", "sekrit", "--tenant", "acme"}, envOf(nil), io.Discard)
	if err == nil {
		t.Fatal("--api-token was accepted; the token must never be passable in argv")
	}
}

func TestTenantAndBaseURL(t *testing.T) {
	env := map[string]string{"NETSKOPE_API_TOKEN": "t"}
	for _, tc := range []struct {
		name    string
		args    []string
		want    string
		wantErr string
	}{
		{name: "tenant expands", args: []string{"--tenant", "acme"}, want: "https://acme.goskope.com"},
		{name: "base url verbatim", args: []string{"--base-url", "https://acme.eu.goskope.com"}, want: "https://acme.eu.goskope.com"},
		{name: "trailing slash trimmed", args: []string{"--base-url", "https://acme.goskope.com/"}, want: "https://acme.goskope.com"},
		{name: "both rejected", args: []string{"--tenant", "a", "--base-url", "https://b"}, wantErr: "exactly one"},
		{name: "neither rejected", args: nil, wantErr: "no tenant configured"},
		{name: "relative rejected", args: []string{"--base-url", "acme.goskope.com"}, wantErr: "must be absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.args, envOf(env), io.Discard)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.BaseURL.String(); got != tc.want {
				t.Fatalf("base URL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToolGroups(t *testing.T) {
	env := map[string]string{"NETSKOPE_API_TOKEN": "t", "NETSKOPE_TENANT": "acme"}
	cfg, err := Load([]string{"--tool-groups", "npa,events,npa"}, envOf(env), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups) != 2 || cfg.Groups[0] != "npa" || cfg.Groups[1] != "events" {
		t.Fatalf("groups = %v, want [npa events] deduplicated", cfg.Groups)
	}
	if _, err := Load([]string{"--tool-groups", "nope"}, envOf(env), io.Discard); err == nil {
		t.Fatal("an unknown tool group was accepted")
	}
	if _, err := Load([]string{"--tool-groups", ","}, envOf(env), io.Discard); err == nil {
		t.Fatal("an empty tool group list was accepted")
	}
}

func TestModesAreExclusive(t *testing.T) {
	env := map[string]string{"NETSKOPE_API_TOKEN": "t", "NETSKOPE_TENANT": "acme"}
	if _, err := Load([]string{"--stdio", "--http"}, envOf(env), io.Discard); err == nil {
		t.Fatal("--stdio and --http were accepted together")
	}
	cfg, err := Load(nil, envOf(env), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeStdio {
		t.Fatalf("default mode = %q, want stdio", cfg.Mode)
	}
}

// Logging a Config by accident must not leak the token.
func TestConfigRedactsSecrets(t *testing.T) {
	env := map[string]string{"NETSKOPE_API_TOKEN": "sup3rs3cret", "NETSKOPE_TENANT": "acme"}
	cfg, err := Load(nil, envOf(env), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTPAuthToken = "bearer-s3cret"
	s := cfg.String()
	for _, secret := range []string{"sup3rs3cret", "bearer-s3cret"} {
		if strings.Contains(s, secret) {
			t.Fatalf("Config.String() leaked %q: %s", secret, s)
		}
	}
	if !strings.Contains(s, "***") {
		t.Fatalf("Config.String() did not redact: %s", s)
	}
}

// The environment supplies a default; an explicit flag overrides it. Comparing
// the flag against its default value cannot tell "unset" from "set to the
// default", so --log-level info used to be silently overridden by the env.
func TestLogLevelFlagBeatsEnvironment(t *testing.T) {
	tok := writeToken(t, "t")
	for _, tc := range []struct {
		name string
		args []string
		env  string
		want slog.Level
	}{
		{"env supplies the default", nil, "debug", slog.LevelDebug},
		{"explicit flag wins", []string{"--log-level", "info"}, "debug", slog.LevelInfo},
		{"explicit flag wins either way", []string{"--log-level", "error"}, "debug", slog.LevelError},
		{"neither set", nil, "", slog.LevelInfo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--tenant", "acme", "--api-token-file", tok}, tc.args...)
			cfg, err := Load(args, envOf(map[string]string{"NETSKOPE_MCP_LOG_LEVEL": tc.env}), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.LogLevel != tc.want {
				t.Fatalf("LogLevel = %v, want %v", cfg.LogLevel, tc.want)
			}
		})
	}
}
