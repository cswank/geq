{
  inputs = {
    # Use a recent stable release, but fix the version to make it reproducible.
    # This version should be updated from time to time.
    nixpkgs.url = "nixpkgs/nixos-24.05";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      with pkgs;
      {
        devShells.default = mkShell {
          buildInputs = [
            sqlite-interactive
          ];
        };
      }
    );
}
