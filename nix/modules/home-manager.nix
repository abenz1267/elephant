flake: {
  config,
  lib,
  pkgs,
  ...
}:
with lib; let
  cfg = config.programs.elephant;
  settingsFormat = pkgs.formats.toml {};

  # Available providers
  providerOptions = {
    desktopapplications = "Desktop application launcher";
    files = "File search and management";
    clipboard = "Clipboard history management";
    runner = "Command runner";
    symbols = "Symbols and emojis";
    calc = "Calculator and unit conversion";
    menus = "Custom menu system";
    providerlist = "Provider listing and management";
    websearch = "Web search integration";
    todo = "Todo list";
    unicode = "Unicode symbol search";
    bluetooth = "Basic Bluetooth management";
    windows = "Find and focus windows";
  };
in {
  imports = [
    # Deprecated: delete with v3.0.0 release
    (lib.mkRenamedOptionModule ["programs" "elephant" "config"] ["programs" "elephant" "settings"])
  ];

  options.programs.elephant = {
    enable = mkEnableOption "Elephant launcher backend";

    package = mkOption {
      type = types.package;
      default = flake.packages.${pkgs.stdenv.system}.elephant-with-providers;
      defaultText = literalExpression "flake.packages.\${pkgs.stdenv.system}.elephant-with-providers";
      description = "The elephant package to use.";
    };

    providers = mkOption {
      type = types.listOf (types.enum (attrNames providerOptions));
      default = attrNames providerOptions;
      example = [
        "files"
        "desktopapplications"
        "calc"
      ];
      description = ''
        List of built-in providers to enable (install). Available providers:
        ${concatStringsSep "\n" (mapAttrsToList (name: desc: "  - ${name}: ${desc}") providerOptions)}
      '';
    };

    installService = mkOption {
      type = types.bool;
      default = true;
      description = "Create a systemd service for elephant.";
    };

    debug = mkOption {
      type = types.bool;
      default = false;
      description = "Enable debug logging for elephant service.";
    };

    settings = mkOption {
      description = ''
        elephant/elephant.toml configuration as Nix attributes.
        `elephant generatedoc` to view your installed version's options.
      '';
      default = {};
      type = types.submodule {
        freeformType = settingsFormat.type;
      };
      example = ''
        {
          auto_detect_launch_prefix = false;
        }
      '';
    };

    provider = mkOption {
      description = "Provider specific settings";
      type = types.attrsOf (types.submodule {
        options = {
          settings = mkOption {
            description = ''
              Provider specific toml configuration as Nix attributes.
              `elephant generatedoc` to view your installed providers version options.
            '';
            type = types.submodule {
              freeformType = settingsFormat.type;
            };
            default = {};
          };
        };
      });
      default = {};
      example = ''
        websearch.settings = {
          entries = [
            {
              name = "NixOS Options";
              url = "https://search.nixos.org/options?query=%TERM%";
            }
          ];
        };
      '';
    };
  };

  config = mkIf cfg.enable {
    home.packages = [cfg.package];

    # Install providers to user config
    xdg.configFile =
      {
        # Generate elephant config
        "elephant/elephant.toml" = mkIf (cfg.settings != {}) {
          source = settingsFormat.generate "elephant.toml" cfg.settings;
        };
      }
      //
      # Generate provider files
      builtins.listToAttrs
      (map
        (
          provider:
            lib.nameValuePair
            "elephant/providers/${provider}.so"
            {
              source = "${cfg.package}/lib/elephant/providers/${provider}.so";
              force = true; # Required since previous version used activation script
            }
        )
        cfg.providers)
      # Generate provider configs
      // (mapAttrs'
        (
          name: {settings, ...}:
            lib.nameValuePair
            "elephant/${name}.toml"
            {
              source = settingsFormat.generate "${name}.toml" settings;
            }
        )
        cfg.provider);

    systemd.user.services.elephant = mkIf cfg.installService {
      Unit = {
        Description = "Elephant launcher backend";
        After = ["graphical-session.target"];
        PartOf = ["graphical-session.target"];
        ConditionEnvironment = "WAYLAND_DISPLAY";
      };

      Service = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/elephant ${optionalString cfg.debug "--debug"}";
        Restart = "on-failure";
        RestartSec = 1;

        X-Restart-Triggers = [
          (builtins.hashString "sha256" (builtins.toJSON {
            inherit (cfg) settings providers provider debug;
          }))
        ];

        # Clean up socket on stop
        ExecStopPost = "${pkgs.coreutils}/bin/rm -f /tmp/elephant.sock";
      };

      Install = {
        WantedBy = ["graphical-session.target"];
      };
    };
  };
}
