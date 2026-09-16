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

Netskope replaced the token flow with **RBAC v3**, and which flow your tenant uses decides
where you go. A tenant banner reading *"RBACv3 has been enabled for your tenants, please use
the Admin Users screen to create tokens henceforth"* means the first flow below.

#### RBAC v3

Permissions live on a **role**, and a role grants *functions* — abstract capabilities —
rather than endpoints. You no longer tick `/api/v2/...` paths directly:

1. **Settings → Administration → Roles.** Create a role and enable only the functions the
   work needs. Each function shows the API v2 endpoints it covers, in the right-hand panel
   and behind the information icon beside it. That panel is the authoritative endpoint →
   function mapping, and it is how you turn the endpoint list below into a set of functions
   to enable.
2. **Pick a level per function:** **View** (read), **Manage** (read and write), or
   **Manage & Apply** (read and write, changes applied immediately). *Max Permissions* means
   an inherently read-only function will not offer Manage at all.
3. **Optionally set the role's IP allowlist**, which overrides the global allowlist.
4. **Settings → Administration → Administrators & Roles → Administrators → Service
   Account.** Name the account, attach the role, set the token expiry in days, click
   **Create**. The role must already exist.
5. **Copy the token when it appears — it is shown once.** You can defer generation and come
   back to it from the ellipsis on the service account's row; the API Credential column
   there also revokes, regenerates and re-expires it.

Roughly, **View** is the old Read and **Manage** is the old Read + Write, so the table below
still tells you which endpoints need more than read.

#### Legacy tokens

The old flow — **Settings → Tools → REST API v2**, with per-endpoint Read / Read + Write
grants — is what tokens created before the switch used, and those tokens keep working until
they expire. On an RBAC v3 tenant that screen no longer issues tokens or endpoint grants,
and an existing token cannot be extended or reissued: edit, revoke or delete only. So treat
a legacy token's expiry date as a migration deadline, not a renewal date.

**Scope the token read-only unless you specifically intend a model to change the tenant.**
The role attached to the service account is the defence that does not depend on this server
being correct; `allowDestructive` is the inner one.

#### Grants the token needs

Every endpoint the server touches, what read access to it buys, and what read + write buys
on top. Under RBAC v3 you feed the endpoint column into the role editor's endpoint panel to
find the function to enable; under the legacy flow it is the grant list directly:

| Endpoint | Tool | **View** / Read | **Manage** / Read + Write adds |
|---|---|---|---|
| `/api/v2/infrastructure/publishers` | `netskope_publishers` (also the `netskope_tenant_info` probe) | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/infrastructure/publisherupgradeprofiles` | `netskope_publisher_upgrade_profiles` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/infrastructure/lbrokers` | `netskope_local_brokers` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/steering/apps/private` | `netskope_private_apps` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/steering/apps/private/tags` | `netskope_private_app_tags` | `list` | `create`, `update`, `delete`† |
| `/api/v2/policy/npa/rules` | `netskope_npa_policy_rules` | `list`, `get` | `create`, `update`‡, `delete`† |
| `/api/v2/policy/internetaccess/rules` | `netskope_realtime_policy_rules` | `list`, `get`§ | — read-only in code, opt-in§ |
| `/api/v2/policy/internetaccess/groups` | `netskope_realtime_policy_groups` | `list`, `get`§ | — read-only in code, opt-in§ |
| `/api/v2/policy/npa/policygroups` | `netskope_npa_policy_groups` | `list`, `get` | `create`, `update`‡, `delete`† |
| `/api/v2/policy/urllist` | `netskope_url_lists` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/profiles/customcategories` | `netskope_custom_categories` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/scim/Users` | `netskope_scim_users` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/scim/Groups` | `netskope_scim_groups` | `list`, `get` | `create`, `update`, `delete`† |
| `/api/v2/reporting/aa/reports` | `netskope_reports` | `list`, `get` | — read-only in code |
| `/api/v2/events/datasearch/*` | `netskope_event_search`, `netskope_alert_search` | search | — read-only in code |

† `delete` additionally requires `allowDestructive = true`; see [Write safety](#write-safety).

‡ `update` on the NPA policy endpoints is sent as `PATCH`, not `PUT`: the gateway registers
no `PUT` route at item level there. See `Resource.UpdateMethod`.

§ These two grants do nothing yet: the `internetaccess` policy API is still in development
and Netskope has to enable it per tenant, so the endpoints 403 no matter how wide the role
is. See [tenant-side gates](#verifying-the-endpoints).

A read-only deployment needs read on every row and nothing more. `netskope_reports`,
`netskope_realtime_policy_rules` and the event tools cannot write whatever the role
allows — the first two declare only `list` and `get` in the resource table, the event
tools are a bare GET — so **Manage** on a function covering only
`/api/v2/reporting/aa/reports` or `/api/v2/events/datasearch/*` grants capability nothing
will ever use.

Real-time protection and NPA are separate rulebooks on separate routes, so a role scoped to
one does not read the other: `netskope_realtime_policy_rules` needs
`/api/v2/policy/internetaccess/*` and says nothing about private apps, and
`netskope_npa_policy_rules` the reverse. Granting the `internetaccess` half is not currently
enough on its own — see the `§` note above.

Tool groups you do not enable need no grants at all: their tools are never registered, so
the endpoints are never called. The one endpoint to cover regardless is
`/api/v2/infrastructure/publishers`, which the `core` group probes to prove the token works
— without read access there, `netskope_tenant_info` reports a valid token with too narrow a
role rather than a healthy tenant.

One consequence of the function-level model worth planning around: a single function can
cover more endpoints than you wanted, and a role is the only place to say no. If enabling
the function that covers `/api/v2/steering/apps/private` also brings endpoints this server
never calls, that is a wider token than the table implies — read the endpoint panel before
accepting it, and split across two roles and two service accounts if the extra reach
matters.

It cuts the other way too, and this one is measured rather than inferred: granting **Manage**
on the NPA section covered `/api/v2/steering/*` and `/api/v2/infrastructure/*` but *not*
`/api/v2/policy/npa/*`, whose writes still returned 403. The NPA tools in the table above
span at least two functions, so "NPA is writable" is not a conclusion you can draw from one
grant — verify per endpoint with the probe in
[Verifying the endpoints](#verifying-the-endpoints).

#### If you want writes

Two independent gates, and both must open:

1. **The role.** Set **Manage** on only the functions covering endpoints you intend to
   change, and leave the rest at **View**. This is the gate that does not depend on this
   server being correct, and the only one that stops `update` — which can disable a policy
   rule or repoint a private app at a different host.
2. **`allowDestructive`.** Leave it `false` and `delete` is never registered, whatever the
   role permits; set it `true` and `delete` becomes callable on every enabled resource whose
   role allows it. There is no per-resource form of this flag, so a tenant where deletes are
   acceptable for private app tags but not for policy rules is expressed by keeping the
   policy function at **View**, not by a setting here.

The narrow-write posture worth copying: **Manage** on the one or two functions the work
actually touches, **View** everywhere else, `allowDestructive = false`. Widen one function
at a time.

Two things to check in the role editor rather than assume, because neither the function
boundaries nor the grant granularity are uniform:

- whether `/api/v2/events/datasearch/` arrives as one endpoint or one per index
  (`application`, `audit`, `page`, `infrastructure`, `network`, `alert`, `incident`) — the
  path is built per-type on each call, so if they are separate you need every index you
  intend to query;
- whether `/api/v2/steering/apps/private/tags` is covered separately from its parent path.

A missing grant is a 403, which the client maps to a message naming the endpoint, so the
practical route is to grant read on `publishers`, confirm `netskope_tenant_info` passes,
then widen until nothing 403s. The two gates fail differently, which is how you tell them
apart: a `create` or `update` the role does not cover is a 403 from the tenant, while a
`delete` blocked by `allowDestructive` never leaves the process — it comes back as `action
"delete" is not enabled for <tool>; available actions are ...`.

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
| `policy` | `netskope_url_lists`, `netskope_custom_categories` |
| `internetaccess` | `netskope_realtime_policy_rules`, `netskope_realtime_policy_groups` — **not registered by default**§ |
| `events` | `netskope_event_search`, `netskope_alert_search` |
| `scim` | `netskope_scim_users`, `netskope_scim_groups` |
| `reporting` | `netskope_reports` |

Narrow the surface with `toolGroups` — it is not only a safety knob, since every registered
tool costs context in the client's tool list.

§ `internetaccess` is the one group not registered by default, because until Netskope
enables those routes on your tenant every call to them 403s — see the `§` note above — and a
tool that always fails is worse than one the model was never shown. Turn it on with
`enableInternetAccess = true` (or `--tool-groups ...,internetaccess`) once the routes answer.

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
| `enableInternetAccess` (default `false`) | register the real-time protection policy tools |
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

The resource tables are written from documentation and prior art, **not** from your tenant,
and tenants differ in which v2 routes they expose at all. Check a group before relying on it.

If your tenant serves the Swagger document, diff against it:

```sh
curl -s -K ~/.netskope-curlrc 'https://acme.goskope.com/apidocs/?include_beta_routes=1' \
  | jq -r '.paths | keys[]'
```

Not every tenant has that route — it 404s on some — in which case probe the paths directly.
The gateway distinguishes the two failures that matter, so a bare status code is enough:

```sh
for p in /api/v2/infrastructure/publishers /api/v2/policy/urllist /api/v2/scim/Users; do
  printf '%-44s %s\n' "$p" \
    "$(curl -s -K ~/.netskope-curlrc -o /dev/null -w '%{http_code}' "https://acme.goskope.com$p?limit=1")"
done
```

- **200** — the route exists and the role covers it.
- **403** — the route exists, the role does not cover it. Widen the role.
- **404 `no Route matched with those values`** — the route is absent from this tenant.
  Nothing you can grant will fix it.

Routes are registered **per method**, not per path, so check the verb you actually need.
On one tenant `GET /api/v2/policy/npa/rules/{id}` is routed while `PUT` to the same path
404s and only `PATCH` exists — which is why `Resource.UpdateMethod` exists. A 404 on a
write whose `GET` works means the wrong verb, not a missing object.

Keep the token out of `argv`: put `header = "Netskope-API-Token: ..."` in a `0400` curl
config and pass it with `-K`, rather than interpolating `$(cat ...)` into the command line
where `ps` can see it.

Watch for a **200 carrying `{"status":"error"}`**. Some routes answer a missing object that
way instead of with a 404 — the client treats such a body as a failure rather than data, but
when probing by hand a bare status code will tell you nothing is wrong.

Three known tenant-side gates:

- **`npa_api_policy_enabled`** is off by default and needs Netskope support to enable. If
  every `netskope_npa_policy_rules` call 403s, that flag is why.
- **Real-time protection policy over the API is not generally available.** The
  `/api/v2/policy/internetaccess/*` routes are still in development at Netskope and have to
  be enabled per tenant by Netskope, the same way `npa_api_policy_enabled` does. Until they
  are, `netskope_realtime_policy_rules` and `netskope_realtime_policy_groups` 403 on every
  call with *"the token has no grant for this endpoint"* — which reads exactly like a role
  that needs widening, but no role you can build in the UI will clear it. Do not chase this
  in the grant table. Observed on a tenant where every other group answered 200 — NPA
  policy included — and only these two 403d, under a role that covered them. Read inline
  policy in the Netskope UI meanwhile.
- A role that does not cover an endpoint produces a 403 that looks identical to a bug. The
  client maps it to *"the token has no grant for this endpoint; widen the role attached to
  the service account..."* precisely so it does not.

A 404 here has meant a wrong path in this table far more often than a missing route on the
tenant. Every path corrected so far was ours: `npa/brokers` → `infrastructure/lbrokers`,
`reporting/reports` → `reporting/aa/reports`, `policy/customcategory` →
`profiles/customcategories`, and `policy/npa/rules` → `policy/internetaccess/rules` for
real-time protection, which had been pointed at the NPA rulebook and so returned
private-access rules for an inline query. The tenant's own Swagger
(`https://<tenant>/apidocs/`, admin UI session — an API token will not open it) is the
authority; the public docs do not enumerate these. Genuinely absent: the `audit` and
`infrastructure` `datasearch` indices, which exist only on the `dataexport` family.

A wrong path is one line in a `[]Resource` table in `internal/tools/table_*.go`, not a
rewritten function — the tables are deliberately data for exactly this reason.

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
