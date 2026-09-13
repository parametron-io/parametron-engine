{
  description = "Parametron Engine development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs { inherit system; };
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = [
          pkgs.go_1_26
          pkgs.gcc
          pkgs.python3
          pkgs.freecad
        ];

        shellHook = ''
          echo "Parametron Engine dev shell"
          echo "Go: $(go version)"
          echo "Python: $(python3 --version)"
          echo "FreeCAD: $(freecadcmd --version | head -n 1 || true)"
        '';
      };
    };
}
