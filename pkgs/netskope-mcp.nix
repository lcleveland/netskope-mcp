# The MCP server itself: a single static Go binary with no runtime dependencies.
#
# This is the first from-source build in this set of flakes -- netskope-client,
# falcon-sensor and ninjarmm-ncplayer all repackage prebuilt vendor binaries --
# so it is also the first to carry a `vendorHash`. That hash pins the dependency
# tree and must be refreshed whenever go.mod or go.sum changes:
#
#   nix build .#netskope-mcp 2>&1 | grep -A1 'got:'
#
# Set `vendorHash = lib.fakeHash` first if you would rather be told the value
# than have the old one silently satisfy a stale lock.
{
  lib,
  buildGoModule,
  versionCheckHook,
}:

buildGoModule (finalAttrs: {
  pname = "netskope-mcp";
  version = "0.1.0";

  # Only the Go tree. Keeping flake.nix, the module and the README out of the
  # source hash means editing prose does not rebuild the binary, which matters
  # more than it sounds given how much prose these repos carry.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
    ];
  };

  vendorHash = "sha256-xWVLVyxy6+Howd+mfNvxKbE9K39x7t5mU8iAGHprciI=";

  subPackages = [ "cmd/netskope-mcp" ];

  # No cgo anywhere in this tree. Disabling it gives a fully static binary and
  # forces Go's pure resolver, which is why the module's unit can get away with
  # such a narrow RestrictAddressFamilies.
  env.CGO_ENABLED = 0;

  ldflags = [
    "-s"
    "-w"
    "-X"
    "github.com/lcleveland/netskope-mcp/internal/version.Version=${finalAttrs.version}"
  ];

  # The Go tests are hermetic: every one of them stands up an httptest server on
  # loopback, which the Nix sandbox allows, and none reaches a real tenant. That
  # is a property worth keeping -- `netskope.New` takes an injected *http.Client
  # precisely so it stays true.
  doCheck = true;

  # subPackages narrows what gets built AND, by default, what gets tested -- so
  # with it set, checkPhase ran only ./cmd/netskope-mcp, which has almost no
  # tests, and the entire ./internal suite was silently skipped in CI. Unset it
  # for the check phase only: build one binary, test everything.
  preCheck = ''
    unset subPackages
  '';

  nativeInstallCheckInputs = [ versionCheckHook ];
  versionCheckProgramArg = "--version";
  doInstallCheck = true;

  meta = {
    description = "MCP server for the Netskope REST API v2 (NPA, policy, events, SCIM)";
    longDescription = ''
      Bridges an MCP client such as Claude Code or Claude Desktop to a Netskope
      tenant's REST API v2. Serves either over stdio, for a client that spawns it
      directly, or over Streamable HTTP as a systemd service.

      Delete actions are not registered unless explicitly enabled, and the API
      token is read from a file or a systemd credential -- never from argv or the
      process environment.
    '';
    homepage = "https://github.com/lcleveland/netskope-mcp";
    license = lib.licenses.mit;
    platforms = lib.platforms.linux;
    mainProgram = "netskope-mcp";
  };
})
