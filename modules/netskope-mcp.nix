# NixOS module for netskope-mcp, an MCP server over the Netskope REST API v2.
#
# Two things shape this module.
#
# The first is the secret. The server holds a Netskope REST API v2 token, which
# is a tenant administration credential: anything that can read it can read (and
# depending on its grants, rewrite) the tenant's security configuration. So the
# token is never a Nix value. `apiTokenFile` is an absolute path resolved at
# runtime and handed to the unit through systemd's LoadCredential=, which
# materialises it 0400, owned by the service's uid, on a private tmpfs that is
# torn down when the unit stops. It never reaches the Nix store, the unit's
# Environment=, argv, or a nixos-rebuild log. That is also why this module needs
# no knowledge of sops-nix or agenix: both produce a root-readable file, which is
# all LoadCredential= wants.
#
# The second is that the thing on the other end of this server is a language
# model. `allowDestructive` defaults to false, and when false the delete actions
# are not registered at all -- they are absent from the JSON schema the model is
# shown, so there is no instruction to argue with and nothing to jailbreak. The
# outer safety net is the token's own per-endpoint grants, which live in the
# tenant rather than here; the README says to prefer a read-only one.
{
  config,
  lib,
  pkgs,
  ...
}:

let
  inherit (lib)
    mkIf
    mkOption
    mkEnableOption
    types
    optional
    optionals
    literalExpression
    ;

  cfg = config.services.netskope-mcp;

  # Loopback is the default and the only address that is safe unauthenticated.
  isLoopback = a: a == "127.0.0.1" || a == "::1" || a == "localhost" || lib.hasPrefix "127." a;

  # Every one of these is non-secret by construction. If you add an argument
  # here, check that it stays true: argv is world-readable through ps.
  serveArgs = [
    "--http"
    "--addr"
    "${cfg.listenAddress}:${toString cfg.port}"
    "--path"
    cfg.path
  ]
  ++ optionals (cfg.tenant != null) [
    "--tenant"
    cfg.tenant
  ]
  ++ optionals (cfg.baseUrl != null) [
    "--base-url"
    cfg.baseUrl
  ]
  ++ [
    "--tool-groups"
    (lib.concatStringsSep "," (cfg.toolGroups ++ optional cfg.enableInternetAccess "internetaccess"))
    "--request-timeout"
    cfg.requestTimeout
    "--log-level"
    cfg.logLevel
  ]
  ++ optional cfg.allowDestructive "--allow-destructive"
  ++ cfg.extraArgs;

  staticUser = cfg.user != null;
in
{
  options.services.netskope-mcp = {
    enable = mkEnableOption "the Netskope REST API v2 MCP server";

    package = mkOption {
      type = types.package;
      default = pkgs.callPackage ../pkgs/netskope-mcp.nix { };
      defaultText = literalExpression "pkgs.netskope-mcp";
      description = "The netskope-mcp package to use.";
    };

    tenant = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "acme";
      description = ''
        Netskope tenant short name, expanded to `https://<tenant>.goskope.com`.
        Set this or {option}`baseUrl`, not both.
      '';
    };

    baseUrl = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "https://acme.eu.goskope.com";
      description = ''
        Full tenant base URL. Use instead of {option}`tenant` for a regional
        tenant whose hostname is not `<name>.goskope.com`.
      '';
    };

    apiTokenFile = mkOption {
      # NB: string, not path -- a `path` would be copied into the world-readable
      # /nix/store when interpolated. This is an absolute path resolved at runtime.
      type = types.nullOr types.str;
      default = null;
      example = "/run/secrets/netskope-api-token";
      description = ''
        Absolute path to a runtime file holding a Netskope REST API v2 token
        (under RBAC v3: create a role under Settings > Administration > Roles,
        then a Service Account under Settings > Administration > Administrators
        & Roles > Administrators that carries it).

        Read through systemd credentials at runtime, so the value never enters
        the unit definition, the Nix store, the process environment, argv, or a
        nixos-rebuild log. Provide it with sops-nix, agenix, or a plain
        root-owned 0400 file; this module does not care which.

        The role attached to the token bounds everything this server can do, and
        it is the outer safety net that {option}`allowDestructive` sits inside.
        Prefer a role with no Manage permissions unless you specifically intend
        to let a model change the tenant.
      '';
    };

    allowDestructive = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Register delete actions on publishers, private apps, policy rules and
        SCIM objects.

        When false (the default) those actions are not registered at all: they
        are absent from the tool schemas the model is shown, so it is never told
        the capability exists. Enabling this is a deliberate act -- Netskope has
        no undo.
      '';
    };

    toolGroups = mkOption {
      type = types.listOf (
        types.enum [
          "core"
          "npa"
          "policy"
          "events"
          "scim"
          "reporting"
        ]
      );
      default = [
        "core"
        "npa"
        "policy"
        "events"
        "scim"
        "reporting"
      ];
      example = [
        "core"
        "npa"
        "events"
      ];
      description = ''
        Which tool families to register. Narrowing this is not only about safety:
        every registered tool costs context in the client's tool list, so a
        deployment that only cares about Private Access should say so.

        The real-time protection tools are not in this list; they have their own
        {option}`enableInternetAccess` switch.
      '';
    };

    enableInternetAccess = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Register the real-time protection policy tools
        (`netskope_realtime_policy_rules` and `netskope_realtime_policy_groups`,
        which read `/api/v2/policy/internetaccess/*`).

        Off by default because that API is still in development at Netskope and
        has to be enabled per tenant by Netskope, the same way
        `npa_api_policy_enabled` does for the NPA policy routes. Until it is,
        every call returns 403 with *"the token has no grant for this endpoint"*
        -- which reads like a role that needs widening, but no role you can build
        in the UI will clear it. A tool that always fails is worse than one the
        model was never shown, so turn this on once the routes answer.
      '';
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address the MCP endpoint binds to. Keep this on loopback unless you also
        set {option}`bearerTokenFile`; see the warnings this module emits.
      '';
    };

    port = mkOption {
      type = types.port;
      default = 8231;
      description = "TCP port for the MCP endpoint.";
    };

    path = mkOption {
      type = types.str;
      default = "/mcp";
      description = "URL path the MCP endpoint is mounted at.";
    };

    bearerTokenFile = mkOption {
      # NB: string, not path -- see apiTokenFile.
      type = types.nullOr types.str;
      default = null;
      example = "/run/secrets/netskope-mcp-bearer";
      description = ''
        Absolute path to a runtime file holding a shared secret that callers must
        present as `Authorization: Bearer <token>`.

        Optional for a loopback listener, effectively mandatory for any other:
        without it, anything that can reach {option}`listenAddress` can drive the
        tenant through this server. The `/healthz` endpoint stays open either
        way, so monitoring does not need the secret.
      '';
    };

    openFirewall = mkOption {
      type = types.bool;
      default = false;
      description = "Open {option}`port` in the firewall.";
    };

    requestTimeout = mkOption {
      type = types.str;
      default = "30s";
      description = "Timeout for a single Netskope API request, as a Go duration.";
    };

    logLevel = mkOption {
      type = types.enum [
        "debug"
        "info"
        "warn"
        "error"
      ];
      default = "info";
      description = ''
        Log verbosity. `debug` logs the path and status of every API request;
        request bodies and query strings are never logged, because event and SCIM
        filters carry user identities.
      '';
    };

    user = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Run as this user instead of a systemd DynamicUser. The default (null) is
        preferred: this service has no state, so it has no reason to own a uid
        that outlives it.
      '';
    };

    group = mkOption {
      type = types.nullOr types.str;
      default = cfg.user;
      defaultText = literalExpression "config.services.netskope-mcp.user";
      description = "Group to run as when {option}`user` is set.";
    };

    installCli = mkOption {
      type = types.bool;
      default = cfg.enable;
      defaultText = literalExpression "config.services.netskope-mcp.enable";
      description = ''
        Put the binary on the system PATH. Set this with {option}`enable` left
        false on a workstation that only wants to spawn the server over stdio
        from an MCP client -- no daemon, no listening socket.
      '';
    };

    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = ''
        Extra command-line arguments. Never put a secret here: argv is visible to
        every user on the machine through `ps`.
      '';
    };

    environment = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = ''
        Extra environment variables for the unit. Never put a secret here either:
        the environment is readable from `/proc/<pid>/environ`. Use
        {option}`apiTokenFile`, which goes through systemd credentials instead.
      '';
    };
  };

  config = lib.mkMerge [
    (mkIf cfg.installCli {
      environment.systemPackages = [ cfg.package ];
    })

    (mkIf cfg.enable {
      assertions = [
        {
          assertion = cfg.apiTokenFile != null;
          message = "services.netskope-mcp.apiTokenFile must be set to a runtime path holding a Netskope REST API v2 token.";
        }
        {
          assertion = (cfg.tenant != null) != (cfg.baseUrl != null);
          message = "services.netskope-mcp: set exactly one of `tenant` (a short name, e.g. \"acme\") or `baseUrl` (a full URL, for a regional tenant such as https://acme.eu.goskope.com).";
        }
        {
          assertion = cfg.apiTokenFile == null || lib.hasPrefix "/" cfg.apiTokenFile;
          message = "services.netskope-mcp.apiTokenFile must be an absolute path.";
        }
        {
          assertion = cfg.bearerTokenFile == null || lib.hasPrefix "/" cfg.bearerTokenFile;
          message = "services.netskope-mcp.bearerTokenFile must be an absolute path.";
        }
        {
          assertion = !staticUser || cfg.group != null;
          message = "services.netskope-mcp.group must be set when user is set.";
        }
        {
          assertion = cfg.toolGroups != [ ] || cfg.enableInternetAccess;
          message = "services.netskope-mcp.toolGroups is empty: the server would expose no tools.";
        }
        {
          assertion = lib.hasPrefix "/" cfg.path;
          message = "services.netskope-mcp.path must begin with a slash.";
        }
        {
          # An openFirewall that opens a port nothing outside the host can reach
          # reads as protection while providing none. Say so at eval time.
          assertion = !cfg.openFirewall || !isLoopback cfg.listenAddress;
          message = "services.netskope-mcp.openFirewall has no effect while listenAddress is ${cfg.listenAddress}: the socket is not reachable from outside the host. Set listenAddress, or drop openFirewall.";
        }
      ];

      warnings =
        optional (!isLoopback cfg.listenAddress && cfg.bearerTokenFile == null) ''
          services.netskope-mcp.listenAddress is ${cfg.listenAddress} with no bearerTokenFile:
          the MCP endpoint is unauthenticated and fronts a Netskope tenant administration
          token, so anything that can reach that address can read (and possibly rewrite) your
          tenant's security configuration through it. Set bearerTokenFile, or keep the
          listener on loopback behind a reverse proxy that authenticates.
        ''
        ++ optional cfg.allowDestructive ''
          services.netskope-mcp.allowDestructive is true: delete actions are registered on
          publishers, private apps, NPA policy rules and SCIM objects, and a model that
          misreads a request can remove tenant configuration that Netskope cannot restore.
          Scope the API token read-only for anything you do not intend to be deletable --
          the token's own grants are the defence that does not depend on this server.
        ''
        ++ optional (cfg.apiTokenFile != null && lib.hasPrefix builtins.storeDir cfg.apiTokenFile) ''
          services.netskope-mcp.apiTokenFile points into ${builtins.storeDir}, which is
          world-readable. Use a path materialised at runtime instead: sops-nix
          (/run/secrets/...), agenix (/run/agenix/...), or a root-owned 0400 file.
        ''
        ++ optional (cfg.logLevel == "debug") ''
          services.netskope-mcp.logLevel is "debug", which logs the path and status of every
          Netskope API request. Bodies and query strings are excluded deliberately, but the
          paths alone reveal which objects are being read.
        '';

      users = mkIf staticUser {
        users.${cfg.user} = {
          isSystemUser = true;
          group = cfg.group;
          description = "netskope-mcp service user";
        };
        groups.${cfg.group} = { };
      };

      systemd.services.netskope-mcp = {
        description = "Netskope REST API v2 MCP server";
        documentation = [ "https://github.com/lcleveland/netskope-mcp" ];
        wantedBy = [ "multi-user.target" ];
        after = [ "network-online.target" ];
        wants = [ "network-online.target" ];

        # NB: no NETSKOPE_API_TOKEN, and no token in any other variable. The
        # token arrives only through LoadCredential= below; the binary reads it
        # from $CREDENTIALS_DIRECTORY. tests/eval.nix asserts this.
        environment = {
          NETSKOPE_MCP_LOG_LEVEL = cfg.logLevel;
        }
        // cfg.environment;

        serviceConfig = {
          Type = "exec";
          ExecStart = "${lib.getExe cfg.package} ${lib.escapeShellArgs serveArgs}";
          Restart = "on-failure";
          RestartSec = 5;

          LoadCredential = [
            "api-token:${cfg.apiTokenFile}"
          ]
          ++ optional (cfg.bearerTokenFile != null) "http-token:${cfg.bearerTokenFile}";

          User = mkIf staticUser cfg.user;
          Group = mkIf staticUser cfg.group;
          DynamicUser = !staticUser;

          # This process holds a credential and speaks HTTPS. It has no state, no
          # writable path, and no need for a single capability, so it can be shut
          # down considerably harder than a typical service.
          AmbientCapabilities = [ "" ];
          CapabilityBoundingSet = [ "" ];
          DevicePolicy = "closed";
          LockPersonality = true;
          # Go generates no code at runtime, unlike a JIT runtime would.
          MemoryDenyWriteExecute = true;
          NoNewPrivileges = true;
          PrivateDevices = true;
          PrivateTmp = true;
          PrivateUsers = true;
          ProcSubset = "pid";
          ProtectClock = true;
          ProtectControlGroups = true;
          ProtectHome = true;
          ProtectHostname = true;
          ProtectKernelLogs = true;
          ProtectKernelModules = true;
          ProtectKernelTunables = true;
          ProtectProc = "invisible";
          ProtectSystem = "strict";
          RemoveIPC = true;
          # AF_NETLINK is not optional: the binary is built with CGO_ENABLED=0,
          # and Go's pure resolver enumerates interfaces over netlink. Without it
          # every DNS lookup fails, and the failure looks like a dead tenant.
          RestrictAddressFamilies = [
            "AF_INET"
            "AF_INET6"
            "AF_NETLINK"
          ];
          RestrictNamespaces = true;
          RestrictRealtime = true;
          RestrictSUIDSGID = true;
          SystemCallArchitectures = "native";
          SystemCallFilter = [
            "@system-service"
            "~@privileged"
            "~@resources"
          ];
          UMask = "0077";

          # The only socket this service has any business binding is its own.
          SocketBindDeny = "any";
          SocketBindAllow = "tcp:${toString cfg.port}";
        };
      };

      networking.firewall.allowedTCPPorts = mkIf cfg.openFirewall [ cfg.port ];
    })
  ];

  meta.maintainers = [ ];
}
