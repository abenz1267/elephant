# Elephant Nix Integration

This document describes how to use Elephant with Nix, including Home Manager and NixOS modules.

## Quick Installation

### Using Nix Profile

```bash
# Install elephant with all providers
nix profile install github:abenz1267/elephant#elephant-with-providers

# Setup providers (run once)
elephant-setup

# Start elephant service
elephant --debug
```

### Using Nix Run (temporary)

```bash
# Run elephant temporarily
nix run github:abenz1267/elephant#elephant-with-providers

# In another terminal, setup and test
elephant-setup
elephant listproviders
```

## Home Manager Integration

Add elephant to your Home Manager configuration:

### Basic Configuration

```nix
{
  # In your home.nix or equivalent
  imports = [ 
    inputs.elephant.homeManagerModules.default 
  ];

  programs.elephant = {
    enable = true;
    autoStart = true;  # Start with user session
    debug = false;     # Set to true for debugging
  };
}
```

### Advanced Configuration

```nix
{
  programs.elephant = {
    enable = true;
    autoStart = true;
    debug = false;
    
    # Select specific providers
    providers = [
      "files"
      "desktopapplications" 
      "calc"
      "runner"
      "clipboard"
    ];
    
    # Custom elephant configuration
    config = {
      providers = {
        files = {
          min_score = 50;
          icon = "folder";
        };
        desktopapplications = {
          launch_prefix = "uwsm app --";
          min_score = 60;
        };
        calc = {
          icon = "accessories-calculator";
        };
      };
    };
  };
}
```

### Available Providers

- `files` - File search and management
- `desktopapplications` - Desktop application launcher  
- `calc` - Calculator and unit conversion
- `runner` - Command runner
- `clipboard` - Clipboard history management
- `symbols` - Symbols and emojis
- `websearch` - Web search integration
- `menus` - Custom menu system
- `providerlist` - Provider listing and management

### Service Management

When `autoStart = true`, elephant runs as a systemd user service:

```bash
# Service status
systemctl --user status elephant

# Start/stop manually
systemctl --user start elephant
systemctl --user stop elephant

# View logs  
journalctl --user -u elephant -f
```

## NixOS Integration

For system-wide installation on NixOS:

```nix
{
  # In your configuration.nix
  imports = [ 
    inputs.elephant.nixosModules.default 
  ];

  services.elephant = {
    enable = true;
    debug = false;
    
    # Custom system config
    config = {
      # Global elephant configuration
    };
  };
}
```

## Flake Input Setup

Add elephant to your flake inputs:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    
    # Add elephant input
    elephant = {
      url = "github:abenz1267/elephant";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, home-manager, elephant, ... }: {
    # Your configuration here
  };
}
```

## Available Packages

- `elephant` - Just the elephant binary
- `elephant-providers` - Just the provider plugins
- `elephant-with-providers` - Complete installation (default)

## Usage Examples

### Query Providers

```bash
# List all providers
elephant listproviders

# Query files
elephant query "files;Documents;5;false"

# Search applications
elephant query "desktopapplications;firefox;3;false"

# Calculator
elephant query "calc;2+2;1;false"

# Symbols/emojis
elephant query "symbols;heart;5;false"
```

### API Integration

Elephant provides a Protocol Buffer API over Unix sockets for building custom launchers:

```bash
# Socket location
/tmp/elephant.sock

# Protocol definitions
# See pkg/pb/ directory for .proto files
```

## Development

### Building from Source

```bash
# Clone and build
git clone https://github.com/abenz1267/elephant
cd elephant

# Build with Nix
nix build

# Development shell
nix develop

# Build and test providers
nix develop -c ./build-providers.sh
```

### Contributing

See the main README.md for contribution guidelines.

## Troubleshooting

### Plugin Compatibility Issues

If you see "plugin was built with a different version" errors:

1. Use the Nix packages (they ensure compatibility)
2. Or rebuild everything in the same environment:
   ```bash
   nix develop -c bash -c "
     go build -o ~/.local/bin/elephant cmd/elephant.go
     ./build-providers.sh
   "
   ```

### Service Issues

```bash
# Check service status
systemctl --user status elephant

# View logs
journalctl --user -u elephant -f

# Restart service
systemctl --user restart elephant
```

### Providers Not Loading

```bash
# Check providers directory
ls -la ~/.config/elephant/providers/

# Run setup again
elephant-setup

# Check elephant config
cat ~/.config/elephant/elephant.toml
```