package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// readSecretFile reads a credential from disk.
//
// The trailing newline matters: sops-nix and agenix both write secrets with one,
// as does anyone who created the file with an editor, and a token with "\n" glued
// to the end produces a 401 whose cause is invisible in the logs. Trim it.
func readSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading credential %s: %w", path, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("credential %s is empty", path)
	}
	return s, nil
}

// credentialPath locates a systemd credential by name.
//
// $CREDENTIALS_DIRECTORY is set only by systemd, and only for units that declare
// LoadCredential=. It points at a 0700 root-owned tmpfs directory holding 0400
// files owned by the unit's (possibly dynamic) uid, torn down when the unit stops.
// That is the whole reason the NixOS module needs no knowledge of sops-nix or
// agenix: both produce a root-readable file, which is all LoadCredential= wants.
func credentialPath(env func(string) string, name string) (string, bool) {
	dir := env("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return "", false
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}
