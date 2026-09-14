# VM test: the real binary, a stub tenant, and the assertions that only a booted
# machine can make.
#
# The Go suite already proves the MCP protocol works over both transports, so
# this test is aimed squarely at what the module is responsible for: that the
# unit comes up, that the credential is delivered by systemd and is not readable
# by anything else, that the token appears in neither argv nor the environment,
# and that it nonetheless reaches the tenant. Those are the properties that keep
# the secrets design honest across future refactors.
#
# Deliberately not covered here: real Netskope payload shapes, whether the
# endpoint paths in the resource tables are right for a given tenant, or how a
# narrowly-granted token behaves. Those need a real tenant and belong in the
# README's evidence section, not in a sandboxed VM.
{ pkgs, self }:

let
  tenantPort = 9443;
  mcpPort = 8231;

  apiToken = "s3cr3t-tenant-token";
  bearerToken = "bearer-abc";

  # A stub tenant that answers any /api/v2/ GET, but only when the request
  # carries the expected token -- and records every token it saw, so the test can
  # prove the credential actually made it out of systemd and onto the wire.
  stubTenant = pkgs.writers.writePython3Bin "stub-tenant" { } ''
    import http.server
    import json

    EXPECTED = ${builtins.toJSON apiToken}
    SEEN = "/tmp/seen-tokens"


    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            token = self.headers.get("Netskope-API-Token", "")
            with open(SEEN, "a") as fh:
                fh.write(token + "\n")
            if token != EXPECTED:
                self.send_response(401)
                self.end_headers()
                self.wfile.write(b'{"status":"error","message":"bad token"}')
                return
            payload = {"status": "success", "total": 0, "data": []}
            body = json.dumps(payload).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *args):
            pass


    http.server.HTTPServer(("127.0.0.1", ${toString tenantPort}), Handler).serve_forever()
  '';

  # Drives the MCP endpoint the way a client does: initialize, then tools/list,
  # carrying the session id the server hands back. Streamable HTTP may answer
  # either as JSON or as an SSE frame, so accept both.
  mcpProbe = pkgs.writers.writePython3Bin "mcp-probe" { } ''
    import json
    import sys
    import urllib.request

    URL = "http://127.0.0.1:${toString mcpPort}/mcp"
    BEARER = ${builtins.toJSON bearerToken}


    def post(payload, session=None):
        body = json.dumps(payload).encode()
        req = urllib.request.Request(URL, data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        req.add_header("Accept", "application/json, text/event-stream")
        req.add_header("Authorization", "Bearer " + BEARER)
        if session:
            req.add_header("Mcp-Session-Id", session)
        resp = urllib.request.urlopen(req, timeout=20)
        raw = resp.read().decode()
        # An SSE frame is "event: message\ndata: {...}\n\n"; take
        # the data line.
        for line in raw.splitlines():
            if line.startswith("data:"):
                raw = line[5:].strip()
                break
        parsed = json.loads(raw) if raw.strip() else {}
        return resp.headers.get("Mcp-Session-Id"), parsed


    session, init = post({
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {
            "protocolVersion": "2025-06-18",
            "capabilities": {},
            "clientInfo": {"name": "vm-test", "version": "0"},
        },
    })
    name = init.get("result", {}).get("serverInfo", {}).get("name")
    assert name == "netskope-mcp", f"unexpected serverInfo: {init}"

    post({"jsonrpc": "2.0", "method": "notifications/initialized"}, session)
    _, listed = post({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, session)
    tools = listed.get("result", {}).get("tools", [])
    assert len(tools) >= 10, f"expected the full tool surface, got {len(tools)}"

    # allowDestructive is off in this configuration, so no tool may advertise a
    # delete action. This is the gate the whole write-safety story rests on.
    blob = json.dumps(tools)
    assert '"delete"' not in blob, "a tool advertises delete"

    names = sorted(t["name"] for t in tools)
    assert "netskope_tenant_info" in names, names

    # Call a read-only tool, so the token gets exercised end to end against the
    # stub tenant rather than only sitting in the credential directory.
    _, called = post({
        "jsonrpc": "2.0", "id": 3, "method": "tools/call",
        "params": {"name": "netskope_tenant_info", "arguments": {}},
    }, session)
    assert "result" in called, called

    print(f"ok: {len(tools)} tools, session {session}")
    sys.exit(0)
  '';
in
pkgs.testers.runNixOSTest {
  name = "netskope-mcp-module";

  nodes.machine =
    { ... }:
    {
      imports = [ self.nixosModules.netskope-mcp ];

      environment.systemPackages = [
        pkgs.curl
        mcpProbe
      ];

      systemd.services.stub-tenant = {
        description = "Stub Netskope tenant";
        wantedBy = [ "multi-user.target" ];
        before = [ "netskope-mcp.service" ];
        serviceConfig = {
          ExecStart = pkgs.lib.getExe stubTenant;
          Restart = "on-failure";
        };
      };

      # Root-only 0400 files, exactly what sops-nix and agenix produce. The
      # module does not know or care which of them wrote these.
      systemd.tmpfiles.settings."10-netskope-mcp" = {
        "/run/netskope-api-token".f = {
          user = "root";
          group = "root";
          mode = "0400";
          argument = apiToken;
        };
        "/run/netskope-mcp-bearer".f = {
          user = "root";
          group = "root";
          mode = "0400";
          argument = bearerToken;
        };
      };

      services.netskope-mcp = {
        enable = true;
        # Point at the stub. This is what baseUrl is for, beyond regional tenants.
        baseUrl = "http://127.0.0.1:${toString tenantPort}";
        apiTokenFile = "/run/netskope-api-token";
        bearerTokenFile = "/run/netskope-mcp-bearer";
        listenAddress = "127.0.0.1";
        port = mcpPort;
      };
    };

  testScript = ''
    import re

    machine.wait_for_unit("stub-tenant.service")
    machine.wait_for_unit("netskope-mcp.service")
    machine.wait_for_open_port(${toString mcpPort})

    with subtest("health is reachable without the bearer token"):
        # Monitoring must be able to probe liveness without being handed the
        # secret that guards the tenant.
        out = machine.succeed("curl -fsS http://127.0.0.1:${toString mcpPort}/healthz")
        assert '"status":"ok"' in out, out

    with subtest("the MCP endpoint refuses unauthenticated callers"):
        machine.succeed(
            "test \"$(curl -s -o /dev/null -w %{http_code} -X POST "
            "-H 'Content-Type: application/json' -d '{}' "
            "http://127.0.0.1:${toString mcpPort}/mcp)\" = 401"
        )
        machine.succeed(
            "test \"$(curl -s -o /dev/null -w %{http_code} -X POST "
            "-H 'Authorization: Bearer wrong' -H 'Content-Type: application/json' -d '{}' "
            "http://127.0.0.1:${toString mcpPort}/mcp)\" = 401"
        )

    with subtest("a full MCP session works and advertises no delete action"):
        print(machine.succeed("mcp-probe"))

    with subtest("the token reached the tenant"):
        # Proves the whole path: LoadCredential -> $CREDENTIALS_DIRECTORY ->
        # Netskope-API-Token header.
        machine.succeed("grep -qF ${apiToken} /tmp/seen-tokens")

    with subtest("the credential is not readable by anything else"):
        # systemd materialises credentials as 0550 root:root directories
        # holding 0440 files, and grants the service's (dynamic) uid access
        # through an ACL rather than through ownership. The exact modes are
        # systemd's business; what this module depends on is that "other" has
        # no access at all, and that a random user cannot read the token.
        creds = "/run/credentials/netskope-mcp.service"
        for path in (creds, f"{creds}/api-token", f"{creds}/http-token"):
            mode = machine.succeed(f"stat -L -c %a {path}").strip()
            assert mode[-1] == "0", f"{path} is mode {mode}: readable by other"
        machine.fail(f"runuser -u nobody -- cat {creds}/api-token")
        machine.fail(f"runuser -u nobody -- cat {creds}/http-token")
        # And the source files the module was pointed at stay root-only.
        machine.succeed("test \"$(stat -c %a /run/netskope-api-token)\" = 400")

    with subtest("the token is in neither argv nor the environment"):
        # The assertion that keeps the secrets design honest. argv is visible to
        # every user through ps; environ survives into core dumps.
        pid = machine.succeed(
            "systemctl show -p MainPID --value netskope-mcp.service"
        ).strip()
        machine.fail(f"tr '\\0' '\\n' < /proc/{pid}/cmdline | grep -qF ${apiToken}")
        machine.fail(f"tr '\\0' '\\n' < /proc/{pid}/environ | grep -qF ${apiToken}")
        machine.fail(f"tr '\\0' '\\n' < /proc/{pid}/environ | grep -q NETSKOPE_API_TOKEN")
        # ...and neither is the bearer token.
        machine.fail(f"tr '\\0' '\\n' < /proc/{pid}/cmdline | grep -qF ${bearerToken}")

    with subtest("the unit is actually hardened"):
        # A regression that loosens the sandbox should fail the build, not be
        # noticed later. The real number is well under 2; 3.0 leaves headroom for
        # systemd changing its own weighting.
        out = machine.succeed(
            "systemd-analyze security netskope-mcp.service --no-pager | tail -1"
        )
        print(out)
        # "Overall exposure level for netskope-mcp.service: 1.1 OK :-)"
        # -- note the trailing smiley also contains a colon.
        found = re.search(r"level for \S+: ([0-9.]+)", out)
        assert found is not None, f"could not read an exposure score from: {out}"
        score = float(found.group(1))
        assert score < 3.0, f"unit exposure score regressed to {score}"
        # ProtectSystem=strict means no writable system paths at all.
        machine.fail(f"nsenter --mount --target {pid} -- test -w /etc")

    with subtest("it survives a restart"):
        machine.succeed("systemctl restart netskope-mcp.service")
        machine.wait_for_open_port(${toString mcpPort})
        machine.succeed("curl -fsS http://127.0.0.1:${toString mcpPort}/healthz")
  '';
}
