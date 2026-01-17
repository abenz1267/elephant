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
        default = self.packages.${pkgs.stdenv.system}.elephant-with-providers;

        # Main elephant binary
        elephant = pkgs.buildGo125Module {
          pname = "elephant";
          version = lib.trim (builtins.readFile ./cmd/elephant/version.txt);

          src = ./.;

          vendorHash = "sha256-tO+5x2FIY1UBvWl9x3ZSpHwTWUlw1VNDTi9+2uY7xdU=";

          buildInputs = with pkgs; [
            protobuf
          ];

          nativeBuildInputs = with pkgs; [
            protoc-gen-go
            makeWrapper
          ];

          # Build from cmd/elephant/elephant.go
          subPackages = [
            "cmd/elephant"
          ];

          postFixup = ''
             wrapProgram $out/bin/elephant \
            	    --prefix PATH : ${lib.makeBinPath (with pkgs; [ fd ])}
          '';

          meta = with lib; {
            description = "Powerful data provider service and backend for building custom application launchers";
            homepage = "https://github.com/abenz1267/elephant";
            license = licenses.gpl3Only;
            maintainers = [ ];
            platforms = platforms.linux;
          };
        };

        # Providers package - builds all providers with same Go toolchain
        elephant-providers = pkgs.buildGo125Module rec {
          pname = "elephant-providers";
          version = lib.trim (builtins.readFile ./cmd/elephant/version.txt);

          src = ./.;

          vendorHash = "sha256-tO+5x2FIY1UBvWl9x3ZSpHwTWUlw1VNDTi9+2uY7xdU=";

          buildInputs = with pkgs; [
            wayland
          ];

          nativeBuildInputs = with pkgs; [
            protobuf
            protoc-gen-go
          ];

          excludedProviders = [
            "archlinuxpkgs"
            "dnfpackages"
          ];

          buildPhase = ''
            runHook preBuild

            echo "Building elephant providers..."

            EXCLUDE_LIST="${lib.concatStringsSep " " excludedProviders}"

            is_excluded() {
              target="$1"
              for e in $EXCLUDE_LIST; do
                [ -z "$e" ] && continue
                if [ "$e" = "$target" ]; then
                  return 0
                fi
              done
              return 1
            }

            if [ -d ./internal/providers ]; then
              for dir in ./internal/providers/*; do
                [ -d "$dir" ] || continue
                provider=$(basename "$dir")
                if is_excluded "$provider"; then
                  echo "Skipping excluded provider: $provider"
                  continue
                fi
                set -- "$dir"/*.go
                if [ -e "$1" ]; then
                  echo "Building provider: $provider"
                  if ! go build -buildmode=plugin -o "$provider.so" ./internal/providers/"$provider"; then
                    echo "⚠ Failed to build provider: $provider"
                    exit 1
                  fi
                  echo "Built $provider.so"
                else
                  echo "Skipping $provider: no .go files found"
                fi
              done
            else
              echo "No providers directory found at ./internal/providers"
            fi

            runHook postBuild
          '';

          installPhase = ''
            runHook preInstall

            mkdir -p $out/lib/elephant/providers

            # Copy all built .so files
            for so_file in *.so; do
              if [[ -f "$so_file" ]]; then
                cp "$so_file" "$out/lib/elephant/providers/"
                echo "Installed provider: $so_file"
              fi
            done

            runHook postInstall
          '';

          meta = with lib; {
            description = "Elephant providers (Go plugins)";
            homepage = "https://github.com/abenz1267/elephant";
            license = licenses.gpl3Only;
            platforms = platforms.linux;
          };
        };

        # Combined package with elephant + providers
        elephant-with-providers = pkgs.stdenv.mkDerivation {
          pname = "elephant-with-providers";
          version = lib.trim (builtins.readFile ./cmd/elephant/version.txt);

          dontUnpack = true;

          buildInputs = [
            self.packages.${pkgs.stdenv.system}.elephant
            self.packages.${pkgs.stdenv.system}.elephant-providers
          ];

          nativeBuildInputs = with pkgs; [
            makeWrapper
          ];

          installPhase = ''
            mkdir -p $out/bin $out/lib/elephant
            cp ${self.packages.${pkgs.stdenv.system}.elephant}/bin/elephant $out/bin/
            cp -r ${
              self.packages.${pkgs.stdenv.system}.elephant-providers
            }/lib/elephant/providers $out/lib/elephant/
          '';

          postFixup = ''
            wrapProgram $out/bin/elephant \
                  --prefix PATH : ${
                    lib.makeBinPath (
                      with pkgs;
                      [
                        wl-clipboard
                        libqalculate
                        imagemagick
                        bluez
                      ]
                    )
                  }
          '';

          meta = with lib; {
            description = "Elephant with all providers (complete installation)";
            homepage = "https://github.com/abenz1267/elephant";
            license = licenses.gpl3Only;
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
