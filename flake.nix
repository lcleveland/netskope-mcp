{
  description = "Netskope REST API v2 MCP server, packaged as a NixOS module";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;

      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = lib.genAttrs systems;
      pkgsFor = system: nixpkgs.legacyPackages.${system};
    in
    {
      overlays.default = import ./overlay.nix;

      packages = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        rec {
          netskope-mcp = pkgs.callPackage ./pkgs/netskope-mcp.nix { };
          default = netskope-mcp;
        }
      );

      nixosModules = {
        netskope-mcp =
          { pkgs, ... }:
          {
            imports = [ ./modules/netskope-mcp.nix ];
            # Default to this flake's own build, so importing the module is
            # enough -- no overlay needed.
            services.netskope-mcp.package =
              lib.mkDefault
                self.packages.${pkgs.stdenv.hostPlatform.system}.netskope-mcp;
          };
        default = self.nixosModules.netskope-mcp;
      };

      checks = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          # The package itself, which runs the Go test suite in its checkPhase.
          package = self.packages.${system}.netskope-mcp;
        }
        # Module eval checks and the VM test. Both are cheap enough to keep in
        # `nix flake check`; the eval checks are what catch a regression in the
        # secrets handling, and they need no VM at all.
        // import ./tests/eval.nix { inherit pkgs self lib; }
        // lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          module = import ./tests/module.nix { inherit pkgs self; };
        }
      );

      formatter = forAllSystems (system: (pkgsFor system).nixfmt-tree);

      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go
              pkgs.gopls
              pkgs.gotools
              pkgs.go-tools
              pkgs.delve
              pkgs.nixfmt
              pkgs.curl
              pkgs.jq
            ];
            # No cgo in this tree, and the sandbox has no C compiler.
            env.CGO_ENABLED = "0";
          };
        }
      );
    };
}
