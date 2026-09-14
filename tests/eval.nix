# Module eval checks: cheap, no VM, run on every `nix flake check`.
#
# These exist mostly to keep the secrets handling honest. The token must reach
# the unit only through LoadCredential=, and never through ExecStart or
# Environment= -- that is a property of the generated unit, so it can be asserted
# statically here rather than needing a booted machine. tests/module.nix proves
# the runtime half.
{
  pkgs,
  self,
  lib,
}:

let
  stubPackage = pkgs.writeShellScriptBin "netskope-mcp" ''exec echo "$@"'';

  evalModule =
    module:
    (lib.nixosSystem {
      inherit (pkgs.stdenv.hostPlatform) system;
      modules = [
        self.nixosModules.netskope-mcp
        {
          services.netskope-mcp.package = lib.mkForce stubPackage;
          # Just enough system to evaluate. Nothing here is built.
          boot.loader.grub.enable = false;
          fileSystems."/" = {
            device = "none";
            fsType = "tmpfs";
          };
          system.stateVersion = lib.trivial.release;
        }
        module
      ];
    }).config;

  failedAssertions = config: map (a: a.message) (builtins.filter (a: !a.assertion) config.assertions);

  # A configuration that should evaluate cleanly, reused by several checks.
  base = {
    services.netskope-mcp = {
      enable = true;
      tenant = "acme";
      apiTokenFile = "/run/secrets/netskope-api-token";
    };
  };

  ok =
    name: config:
    let
      broken = failedAssertions config;
    in
    assert
      broken == [ ]
      || throw "${name}: unexpected assertion failures: ${lib.concatStringsSep "; " broken}";
    config;
in
{
  # The load-bearing one. If a future change starts passing the token through
  # the command line or the environment, this fails the build.
  module-eval =
    let
      config = ok "module-eval" (evalModule base);
      service = config.systemd.services.netskope-mcp;
      sc = service.serviceConfig;
    in
    pkgs.runCommand "netskope-mcp-module-eval" { } ''
      cat > cmd <<'EOF'
      ${sc.ExecStart}
      EOF
      cat > env <<'EOF'
      ${lib.concatStringsSep "\n" (lib.mapAttrsToList (k: v: "${k}=${v}") service.environment)}
      EOF
      cat > creds <<'EOF'
      ${lib.concatStringsSep "\n" sc.LoadCredential}
      EOF

      check() { grep -qF -- "$1" "$2" || { echo "missing from $2: $1"; cat "$2"; exit 1; }; }
      refute() { grep -qF -- "$1" "$2" && { echo "unexpected in $2: $1"; cat "$2"; exit 1; }; true; }

      # The configuration that should be on the command line, because none of it
      # is secret.
      check "--http" cmd
      check "--addr 127.0.0.1:8231" cmd
      check "--path /mcp" cmd
      check "--tenant acme" cmd
      check "--log-level info" cmd

      # The token reaches the unit exactly one way.
      check "api-token:/run/secrets/netskope-api-token" creds

      # ...and no other way. A path in argv would be fine, but the module must
      # never pass --api-token-file either: the binary is meant to find the
      # credential itself, so that changing the secrets engine changes nothing here.
      refute "--api-token" cmd
      refute "/run/secrets/netskope-api-token" cmd
      refute "NETSKOPE_API_TOKEN" env

      # Destructive actions are off unless asked for.
      refute "--allow-destructive" cmd

      touch $out
    '';

  # allowDestructive must actually reach the binary, or the option is a lie.
  module-allow-destructive =
    let
      config = ok "module-allow-destructive" (
        evalModule (lib.recursiveUpdate base { services.netskope-mcp.allowDestructive = true; })
      );
      warnings = config.warnings;
    in
    assert
      lib.any (lib.hasInfix "allowDestructive is true") warnings
      || throw "enabling allowDestructive produced no warning";
    pkgs.runCommand "netskope-mcp-allow-destructive" { } ''
      cat > cmd <<'EOF'
      ${config.systemd.services.netskope-mcp.serviceConfig.ExecStart}
      EOF
      grep -qF -- "--allow-destructive" cmd || { echo "allowDestructive did not reach the binary"; cat cmd; exit 1; }
      touch $out
    '';

  # Binding off-loopback without a bearer token exposes a tenant admin
  # credential to the network. That has to warn.
  module-warns-unauthenticated =
    let
      config = evalModule (
        lib.recursiveUpdate base {
          services.netskope-mcp = {
            listenAddress = "0.0.0.0";
            openFirewall = true;
          };
        }
      );
    in
    assert
      lib.any (lib.hasInfix "unauthenticated") config.warnings
      || throw "no warning about an unauthenticated non-loopback listener";
    assert
      config.networking.firewall.allowedTCPPorts == [ 8231 ]
      || throw "openFirewall did not open the configured port";
    pkgs.runCommand "netskope-mcp-warns-unauthenticated" { } "touch $out";

  # A bearer token is a second credential and takes the same route as the first.
  module-bearer-credential =
    let
      config = ok "module-bearer-credential" (
        evalModule (
          lib.recursiveUpdate base {
            services.netskope-mcp = {
              listenAddress = "0.0.0.0";
              bearerTokenFile = "/run/secrets/netskope-mcp-bearer";
            };
          }
        )
      );
      sc = config.systemd.services.netskope-mcp.serviceConfig;
    in
    assert
      lib.any (lib.hasInfix "http-token:/run/secrets/netskope-mcp-bearer") sc.LoadCredential
      || throw "bearerTokenFile did not become a systemd credential";
    assert
      !lib.hasInfix "netskope-mcp-bearer" sc.ExecStart || throw "bearerTokenFile leaked into ExecStart";
    assert
      !lib.any (lib.hasInfix "unauthenticated") config.warnings
      || throw "warned about an unauthenticated listener despite a bearer token";
    pkgs.runCommand "netskope-mcp-bearer-credential" { } "touch $out";

  # The misconfigurations that should be caught at eval time rather than at 3am.
  module-rejects-bad-config =
    let
      wants =
        name: module: infix:
        let
          broken = failedAssertions (evalModule module);
        in
        assert
          lib.any (lib.hasInfix infix) broken
          || throw "${name}: expected an assertion mentioning ${infix}, got: ${lib.concatStringsSep "; " broken}";
        true;
    in
    assert wants "no token" {
      services.netskope-mcp = {
        enable = true;
        tenant = "acme";
      };
    } "apiTokenFile must be set";
    assert wants "both tenant and baseUrl" (lib.recursiveUpdate base {
      services.netskope-mcp.baseUrl = "https://acme.eu.goskope.com";
    }) "exactly one";
    assert wants "neither tenant nor baseUrl" {
      services.netskope-mcp = {
        enable = true;
        apiTokenFile = "/run/secrets/t";
      };
    } "exactly one";
    assert wants "relative token path" (lib.recursiveUpdate base {
      services.netskope-mcp.apiTokenFile = "secrets/token";
    }) "must be an absolute path";
    assert wants "openFirewall on loopback" (lib.recursiveUpdate base {
      services.netskope-mcp.openFirewall = true;
    }) "has no effect";
    pkgs.runCommand "netskope-mcp-rejects-bad-config" { } "touch $out";

  # A token path inside the store is world-readable; warn rather than fail,
  # because it is legal and someone may have done it knowingly.
  module-warns-store-path =
    let
      config = evalModule (
        lib.recursiveUpdate base {
          services.netskope-mcp.apiTokenFile = "${builtins.storeDir}/deadbeef-token";
        }
      );
    in
    assert
      lib.any (lib.hasInfix "world-readable") config.warnings
      || throw "no warning about a token path in the Nix store";
    pkgs.runCommand "netskope-mcp-warns-store-path" { } "touch $out";

  # installCli must work with the daemon off: a workstation that only spawns the
  # server over stdio wants the binary and nothing else.
  module-cli-without-service =
    let
      config = evalModule {
        services.netskope-mcp = {
          enable = false;
          installCli = true;
        };
      };
    in
    assert
      lib.any (p: lib.hasInfix "netskope-mcp" (p.name or "")) config.environment.systemPackages
      || throw "installCli did not put the binary on PATH";
    assert
      !(config.systemd.services ? netskope-mcp)
      || throw "the service unit was defined with enable = false";
    pkgs.runCommand "netskope-mcp-cli-without-service" { } "touch $out";
}
