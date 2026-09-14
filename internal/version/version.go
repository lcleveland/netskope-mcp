// Package version carries the build stamp.
package version

// Version is overwritten at build time with -ldflags "-X .../internal/version.Version=...".
// The Nix package sets it from the derivation's `version` attribute.
var Version = "dev"
