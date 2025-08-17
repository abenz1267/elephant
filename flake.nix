{
  description = ''
    Elephant - a powerful data provider service and backend for building custom application launchers and desktop utilities.
  '';

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default-linux";
  };

  outputs = {
    self,
    nixpkgs,
    systems,
    ...
  }: let
    inherit (nixpkgs) lib;
    eachSystem = f:
      lib.genAttrs (import systems)
      (system: f nixpkgs.legacyPackages.${system});
  in {
    formatter = eachSystem (pkgs: pkgs.alejandra);

    devShells = eachSystem (pkgs: {
      default = pkgs.mkShell {
        name = "elephant-dev-shell";
        inputsFrom = [self.packages.${pkgs.system}.elephant];
        buildInputs = with pkgs; [
          go
          gcc
          protobuf
          protoc-gen-go
        ];
      };
    });

    packages = eachSystem (pkgs: {
      default = self.packages.${pkgs.system}.elephant-with-providers;
      
      # Main elephant binary
      elephant = pkgs.buildGoModule {
        pname = "elephant";
        version = "0.1.0";
        
        src = ./.;
        
        vendorHash = "sha256-bx6/ww7jE8VxDgSLVB12PeXGgxd3k/Brfjhl3qSJBL4=";
        
        buildInputs = with pkgs; [
          protobuf
        ];
        
        nativeBuildInputs = with pkgs; [
          protoc-gen-go
        ];
        
        # Build from cmd/elephant.go
        subPackages = [ "cmd" ];
        
        # Rename the binary from cmd to elephant
        postInstall = ''
          mv $out/bin/cmd $out/bin/elephant
        '';

        meta = with lib; {
          description = "Powerful data provider service and backend for building custom application launchers";
          homepage = "https://github.com/abenz1267/elephant";
          license = licenses.gpl3Only;
          maintainers = [];
          platforms = platforms.linux;
        };
      };
      
      # Providers package - builds all providers with same Go toolchain  
      elephant-providers = pkgs.buildGoModule {
        pname = "elephant-providers";
        version = "0.1.0";
        
        src = ./.;
        
        vendorHash = "sha256-bx6/ww7jE8VxDgSLVB12PeXGgxd3k/Brfjhl3qSJBL4=";
        
        nativeBuildInputs = with pkgs; [
          protobuf
          protoc-gen-go
        ];
        
        # Override the default build process
        buildPhase = ''
          runHook preBuild
          
          # Available providers
          PROVIDERS=(
            "files"
            "desktopapplications" 
            "calc"
            "runner"
            "clipboard"
            "symbols"
            "websearch"
            "menus"
            "providerlist"
          )
          
          echo "Building elephant providers..."
          
          for provider in "''${PROVIDERS[@]}"; do
            if [[ -d "./internal/providers/$provider" ]]; then
              echo "Building $provider provider..."
              go build -buildmode=plugin -o "$provider.so" ./internal/providers/$provider
              echo "✓ Built $provider.so"
            else
              echo "⚠ Provider $provider not found, skipping"
            fi
          done
          
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
        version = "0.1.0";
        
        dontUnpack = true;
        
        buildInputs = [
          self.packages.${pkgs.system}.elephant
          self.packages.${pkgs.system}.elephant-providers
        ];
        
        installPhase = ''
          mkdir -p $out/bin $out/lib/elephant
          
          # Copy elephant binary
          cp ${self.packages.${pkgs.system}.elephant}/bin/elephant $out/bin/
          
          # Copy providers
          cp -r ${self.packages.${pkgs.system}.elephant-providers}/lib/elephant/providers $out/lib/elephant/
          
          # Create setup script
          cat > $out/bin/elephant-setup <<EOF
#!/usr/bin/env bash
set -e

echo "🐘 Setting up Elephant providers..."

# Create config directory
mkdir -p ~/.config/elephant/providers

# Copy providers to user config
cp $out/lib/elephant/providers/*.so ~/.config/elephant/providers/

echo "✅ Elephant providers installed to ~/.config/elephant/providers/"
echo ""
echo "🚀 Usage:"
echo "  elephant --debug    # Start elephant service"
echo "  elephant listproviders  # List installed providers"
EOF
          chmod +x $out/bin/elephant-setup
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