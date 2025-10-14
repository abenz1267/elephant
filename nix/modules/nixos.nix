flake: {
  config,
  lib,
  pkgs,
  ...
}:
with lib; let
  cfg = config.services.elephant;

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
  };
in {
  options.services.elephant = {
    enable = mkEnableOption "Elephant launcher backend system service";

    package = mkOption {
      type = types.package;
      default = flake.packages.${pkgs.stdenv.system}.elephant-with-providers;
      defaultText = literalExpression "flake.packages.\${pkgs.stdenv.system}.elephant-with-providers";
      description = "The elephant package to use.";
    };

    user = mkOption {
      type = types.str;
      default = "elephant";
      description = "User under which elephant runs.";
    };

    group = mkOption {
      type = types.str;
      default = "elephant";
      description = "Group under which elephant runs.";
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
        List of providers to enable. Available providers:
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

    config = mkOption {
      type = types.attrs;
      default = {};
      example = literalExpression ''
        {
          providers = {
            files = {
              min_score = 50;
            };
            desktopapplications = {
              launch_prefix = "uwsm app --";
            };
          };
        }
      '';
      description = "Elephant configuration as Nix attributes.";
    };
  };

  config = mkIf cfg.enable {
    environment.systemPackages = [cfg.package];

    # Install providers to system config
    environment.etc =
      {
        # Generate elephant config
        "xdg/elephant/elephant.toml" = mkIf (cfg.config != {}) {
          source = (pkgs.formats.toml {}).generate "elephant.toml" cfg.config;
        };
      }
      # Generate provider files
      // builtins.listToAttrs
      (map
        (
          provider:
            lib.nameValuePair
            "xdg/elephant/providers/${provider}.so"
            {
              source = "${cfg.package}/lib/elephant/providers/${provider}.so";
            }
        )
        cfg.providers);

    systemd.services.elephant = mkIf cfg.installService {
      description = "Elephant launcher backend";
      wantedBy = ["multi-user.target"];
      after = ["network.target"];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        ExecStart = "${cfg.package}/bin/elephant ${optionalString cfg.debug "--debug"}";
        Restart = "on-failure";
        RestartSec = 1;

        # Security settings
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ReadWritePaths = [
          "/var/lib/elephant"
          "/tmp"
        ];

        # Clean up socket on stop
        ExecStopPost = "${pkgs.coreutils}/bin/rm -f /tmp/elephant.sock";
      };

      environment = {
        HOME = "/var/lib/elephant";
      };
    };
  };
}
