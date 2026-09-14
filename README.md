# netskope-mcp (NixOS)

An [MCP](https://modelcontextprotocol.io) server over the **Netskope REST API v2**, packaged
as a Nix flake with a NixOS module. It lets an MCP client — Claude Code, Claude Desktop —
read and manage a Netskope tenant: Private Access publishers and apps, NPA and inline
policy, URL lists, event and alert search, and SCIM identity.

> **Status: builds and tests clean; not yet run against a live tenant.**
>
> `nix flake check` is green, which covers more than it usually does here. The Go suite
> proves the MCP handshake over *both* transports, the credential precedence ladder, retry
> and rate-limit behaviour, and that the token never appears in an error. The VM test boots
> the real binary against a stub tenant and asserts the things only a booted machine can:
> that the unit reaches `active`, that the credential is delivered by systemd and is not
> readable by another user, that the token appears in **neither `/proc/<pid>/cmdline` nor
> `/proc/<pid>/environ`** while still arriving in the tenant's `Netskope-API-Token` header,
> and that `systemd-analyze security` scores the unit **1.1 (OK)**.
>
> What no sandbox can check: whether the endpoint paths in the resource tables match *your*
> tenant. Netskope moves routes between releases, and the beta-routes flag changes which
> exist. Verify against your own Swagger before trusting any of them — see
> [Verifying the endpoints](#verifying-the-endpoints).

## Quick start

```nix
{
  inputs.netskope-mcp.url = "github:lcleveland/netskope-mcp";

  # ... in your NixOS configuration:
  imports = [ netskope-mcp.nixosModules.default ];

  services.netskope-mcp = {
    enable = true;
    tenant = "acme";                                  # https://acme.goskope.com
    apiTokenFile = "/run/secrets/netskope-api-token";  # sops-nix, agenix, or a 0400 file
  };
}
```

That gives you a hardened systemd unit serving MCP over Streamable HTTP on
`127.0.0.1:8231/mcp`, and the binary on `PATH`. Point a client at it:

```json
{ "mcpServers": { "netskope": { "url": "http://127.0.0.1:8231/mcp" } } }
```

### Without a daemon

A workstation that only wants to spawn the server from its MCP client needs no service at
all — just the binary:

```nix
services.netskope-mcp = {
  enable = false;
  installCli = true;
};
```

```json
{ "mcpServers": { "netskope": {
    "command": "netskope-mcp",
    "args": ["--stdio", "--tenant", "acme",
             "--api-token-file", "/home/you/.config/netskope/token"] } } }
```

The token is passed as a **path**, never as a value: there is deliberately no
`--api-token` flag, because `argv` is visible to every user on the machine through `ps`.

## Secrets

`apiTokenFile` and `bearerTokenFile` are **absolute paths to runtime files**, typed as
strings and never Nix paths, so they are never copied into the world-readable `/nix/store`.
The unit loads them through systemd credentials:

```nix
LoadCredential = [ "api-token:${cfg.apiTokenFile}" ];
```

systemd materialises each one as a `0440` file inside a `0550` directory on a private
tmpfs, reachable by the service's (dynamic) uid through an ACL and by nothing else, torn
down when the unit stops. The server reads it from `$CREDENTIALS_DIRECTORY`.

The consequence worth stating plainly: **the token never enters the Nix store, the unit's
`Environment=`, `argv`, or a `nixos-rebuild` log.** The VM test asserts the last two
directly, by grepping `/proc/<MainPID>/cmdline` and `/proc/<MainPID>/environ` for the
token — so a future refactor that starts passing it the easy way fails the build.

Because the module only ever sees a path, it depends on **neither sops-nix nor agenix**.
Any of these work unchanged:

```nix
apiTokenFile = config.sops.secrets.netskope-api-token.path;  # /run/secrets/...
apiTokenFile = config.age.secrets.netskope-api-token.path;   # /run/agenix/...
apiTokenFile = "/persist/secrets/netskope-api-token";        # a root-owned 0400 file
```

Pointing `apiTokenFile` into the store is legal and warns loudly.

### Getting a token

Netskope tenant UI → **Settings → Administration → Administrators & Roles → Service
Account**, then scope it under **Settings → Tools → REST API v2**. Grants are
per-endpoint.

**Scope the token read-only unless you specifically intend a model to change the tenant.**
The token's own grants are the defence that does not depend on this server being correct;
`allowDestructive` is the inner one.

## Write safety

`allowDestructive` defaults to `false`, and when false the delete actions are **not
registered at all**. They are absent from the JSON schema the model is shown, absent from
the generated tool descriptions, and rejected by the handler. A model cannot be talked into
calling a tool it was never told about — the enforcement is in the registration path, not
in a prompt:

```go
if !o.AllowDestructive {
    actions = slices.DeleteFunc(actions, Action.Destructive)
}
...
prop.Enum = /* only the surviving actions */
```

The VM test asserts the end-to-end consequence: with the default configuration, a live
`tools/list` over the real transport contains no `"delete"` anywhere.

Not gated, and worth understanding: `update` can do as much damage as `delete` — disabling
a policy rule, repointing a private app at a different host. If that matters, the answer is
a read-only token, not a flag here.

## Tools

16 tools by default, shaped one-per-resource with an `action` enum rather than one per
endpoint. That is a deliberate trade: the community Netskope MCP exposes 84 tools, and a
model picks well from ~20 well-described tools and poorly from ~90.

| Group | Tools |
|---|---|
| `core` | `netskope_tenant_info` |
| `npa` | `netskope_publishers`, `netskope_publisher_upgrade_profiles`, `netskope_local_brokers`, `netskope_private_apps`, `netskope_private_app_tags`, `netskope_npa_policy_rules`, `netskope_npa_policy_groups` |
| `policy` | `netskope_url_lists`, `netskope_custom_categories`, `netskope_realtime_policy_rules` |
| `events` | `netskope_event_search`, `netskope_alert_search` |
| `scim` | `netskope_scim_users`, `netskope_scim_groups` |
| `reporting` | `netskope_reports` |

Narrow the surface with `toolGroups` — it is not only a safety knob, since every registered
tool costs context in the client's tool list.

`netskope_tenant_info` is the one to call first when anything fails: it separates a wrong
tenant URL from a rejected token from a token whose per-endpoint grants are too narrow,
which otherwise all look identical.

### Events use `datasearch`, not the `dataexport` iterator

`/api/v2/events/dataexport/*` keeps a **server-side cursor** keyed by a consumer-supplied
`index`, advanced on every `next`. A model calling that would consume and skip pages
belonging to whatever SIEM shares the index — silent, permanent log loss that looks like
nothing at all from this side. `/api/v2/events/datasearch/{type}` is a stateless `GET` and
is the right primitive for ad-hoc investigation, so that is what `netskope_event_search`
uses.

### Results are capped

An unqualified list against a large tenant would otherwise hand the model tens of thousands
of records. Lists are capped at 200 items (100 for private apps) and ~60 KB encoded,
whichever binds first, and a capped result carries a `_truncation` marker telling the model
it is partial and how to narrow the query. Without that marker a model reads a truncated
list as a complete one, which is worse than an error.

## Options

| Option | Purpose |
|---|---|
| `enable` | run the HTTP service |
| `installCli` (default: `enable`) | put the binary on `PATH`; usable with `enable = false` for stdio-only |
| `package` | override the built package |
| `tenant` / `baseUrl` | tenant short name, or a full URL for a regional tenant — exactly one |
| `apiTokenFile` | **required**; runtime path to the REST API v2 token |
| `allowDestructive` (default `false`) | register delete actions |
| `toolGroups` (default: all) | `core`, `npa`, `policy`, `events`, `scim`, `reporting` |
| `listenAddress` (default `127.0.0.1`) / `port` (default `8231`) / `path` (default `/mcp`) | where the endpoint binds |
| `bearerTokenFile` | shared secret required as `Authorization: Bearer`; effectively mandatory off loopback |
| `openFirewall` (default `false`) | open `port`; asserted against a loopback `listenAddress` |
| `requestTimeout` (default `30s`) / `logLevel` (default `info`) | client and logging |
| `user` / `group` (default: `DynamicUser`) | run as a static user instead |
| `extraArgs` / `environment` | escape hatches; never put a secret in either |

Eval-time assertions catch the misconfigurations worth catching early: a missing token,
`tenant` and `baseUrl` both set or both unset, a relative path, and an `openFirewall` that
would open a port nothing outside the host can reach. Warnings cover the ones that are
legal but probably wrong — an unauthenticated non-loopback listener, `allowDestructive`, a
token path inside the store.

## Verifying the endpoints

The resource tables are written from documentation and prior art, **not** from your tenant.
Before relying on a group, diff it against your own Swagger:

```sh
curl -s -H "Netskope-API-Token: $(cat /run/secrets/netskope-api-token)" \
  'https://acme.goskope.com/apidocs/?include_beta_routes=1' | jq -r '.paths | keys[]'
```

A wrong path is one line in a `[]Resource` table in `internal/tools/table_*.go`, not a
rewritten function — the tables are deliberately data for exactly this reason.

Two known tenant-side gates:

- **`npa_api_policy_enabled`** is off by default and needs Netskope support to enable. If
  every `netskope_npa_policy_rules` call 403s, that flag is why.
- Per-endpoint token grants produce 403s that look identical to bugs. The client maps them
  to *"the token has no grant for this endpoint; add it under Settings > Tools > REST API
  v2"* precisely so they do not.

## Packaging approach

`buildGoModule` over the Go tree, `CGO_ENABLED=0` for a fully static binary — an 11 MiB
closure whose only references are `tzdata`, `iana-etc` and `mailcap`. This is the first
from-source build in this set of flakes (`netskope-client`, `falcon-sensor` and
`ninjarmm-ncplayer` all repackage prebuilt vendor binaries), so it is also the first to
carry a `vendorHash`. Refresh it whenever `go.mod` or `go.sum` changes:

```sh
nix build .#netskope-mcp 2>&1 | grep -A1 'got:'
```

The Go tests run in `checkPhase` and are hermetic — every one stands up an `httptest`
server on loopback and none reaches a real host. `netskope.New` takes an injected
`*http.Client` specifically so that stays true and `nix flake check` keeps working in the
sandbox.

`lib.fileset.toSource` keeps `flake.nix`, the module and this README out of the source
hash, so editing prose does not rebuild the binary.

### Hardening

The unit holds a credential and speaks HTTPS; it has no state, no writable path and needs
no capability, so it is locked down further than a typical service: `DynamicUser`, an empty
`CapabilityBoundingSet`, `ProtectSystem=strict`, `ProtectProc=invisible`, `ProcSubset=pid`,
`MemoryDenyWriteExecute` (Go generates no code at runtime), `PrivateUsers`, and
`SocketBindDeny=any` with a single allowed port. `systemd-analyze security` scores it
**1.1**, and the VM test fails if that regresses past 3.0.

One non-obvious entry: `RestrictAddressFamilies` must include **`AF_NETLINK`**. The binary
is built with `CGO_ENABLED=0`, so Go uses its pure resolver, which enumerates interfaces
over netlink. Without it every DNS lookup fails and the failure looks like a dead tenant.

## Impermanence

Nothing to persist. The service has no state directory, no cache and no writable path — it
holds a token in memory and makes HTTPS requests. On a tmpfs-root host the only thing that
needs to survive a reboot is whatever produces `apiTokenFile`, which is your secrets
engine's business rather than this module's.

If stream resumption (the SDK's `EventStore`) is ever enabled, that is the one change that
would introduce state, and it would need a `StateDirectory` and a matching
`ReadWritePaths`.

## Layout

- `flake.nix` — `packages`, `overlays.default`, `nixosModules.{netskope-mcp,default}`, `checks`, `devShell`, `formatter`.
- `overlay.nix` — adds `pkgs.netskope-mcp`.
- `pkgs/netskope-mcp.nix` — the `buildGoModule` derivation.
- `modules/netskope-mcp.nix` — the NixOS module.
- `tests/eval.nix` — module eval checks; no VM, run on every push. These are what catch a regression in the secrets handling.
- `tests/module.nix` — the VM test: real binary, stub tenant.
- `cmd/netskope-mcp/` — flag parsing, signals, mode dispatch.
- `internal/config/` — the credential ladder and its tests.
- `internal/netskope/` — the typed API client: auth, retry, rate limiting, error mapping, redaction.
- `internal/tools/` — the MCP surface. `resource.go` is the shared machinery; `table_*.go` are data.
- `internal/server/` — server construction and the two transports.

## License

MIT.
