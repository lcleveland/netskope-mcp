# Nixpkgs overlay adding the MCP server.
#
#   nixpkgs.overlays = [ netskope-mcp.overlays.default ];
#
# gives you `pkgs.netskope-mcp`. Importing `nixosModules.default` from the flake
# instead defaults `services.netskope-mcp.package` to this flake's own build, so
# for the NixOS module the overlay is optional.
final: _prev: {
  netskope-mcp = final.callPackage ./pkgs/netskope-mcp.nix { };
}
