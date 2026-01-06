{
  description = ''
    Elephant - a powerful data provider service and backend for building custom application launchers and desktop utilities.
  '';

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default-linux";
  };

  outputs =
    {
      self,
      nixpkgs,
      systems,
      ...
    }:
    let
      inherit (nixpkgs) lib;
      eachSystem = f: lib.genAttrs (import systems) (system: f nixpkgs.legacyPackages.${system});
    in
    {
      formatter = eachSystem (pkgs: pkgs.alejandra);

      devShells = eachSystem (pkgs: {
        default = pkgs.mkShell {
          name = "elephant-dev-shell";
          inputsFrom = [ self.packages.${pkgs.stdenv.system}.elephant ];
          buildInputs = with pkgs; [
            go
            gcc
            protobuf
            protoc-gen-go
          ];
        };
      });

      packages = eachSystem (pkgs: {
        default = self.packages.${pkgs.stdenv.system}.elephant;

        # Main elephant binary
        elephant = pkgs.buildGo125Module {
          pname = "elephant";
          version = lib.trim (builtins.readFile ./version.txt);

          src = ./.;

          vendorHash = "sha256-tO+5x2FIY1UBvWl9x3ZSpHwTWUlw1VNDTi9+2uY7xdU=";

          buildInputs = with pkgs; [
            protobuf
            wayland
          ];

          nativeBuildInputs = with pkgs; [
            protoc-gen-go
            makeWrapper
          ];

          # Build from main.go
          subPackages = [
            "."
          ];

          postFixup = ''
             wrapProgram $out/bin/elephant \
            	    --prefix PATH : ${lib.makeBinPath (with pkgs; [ 
                    fd
                    wl-clipboard
                    libqalculate
                    imagemagick
                    bluez
                  ])}
          '';

          meta = with lib; {
            description = "Powerful data provider service and backend for building custom application launchers";
            homepage = "https://github.com/abenz1267/elephant";
            license = licenses.gpl3Only;
            maintainers = [ ];
            platforms = platforms.linux;
          };
        };
      });

      homeManagerModules = {
        default = self.homeManagerModules.elephant;
        elephant = import ./nix/modules/home-manager.nix self;
      };

      nixosModules = {
        default = self.nixosModules.elephant;
        elephant = import ./nix/modules/nixos.nix self;
      };
    };
}