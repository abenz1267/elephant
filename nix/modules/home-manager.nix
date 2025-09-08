flake:
{
  config,
  lib,
  pkgs,
  ...
}:
with lib;
let
  cfg = config.programs.elephant;

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
  };
in
{
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
      default = { };
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
    home.packages = [ cfg.package ];

    # Install providers to user config
    home.activation.elephantProviders = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
      $DRY_RUN_CMD mkdir -p $HOME/.config/elephant/providers
      $DRY_RUN_CMD rm -f $HOME/.config/elephant/providers/*.so

      # Copy enabled providers
      ${concatStringsSep "\n" (
        map (provider: ''
          if [[ -f "${cfg.package}/lib/elephant/providers/${provider}.so" ]]; then
            $DRY_RUN_CMD cp "${cfg.package}/lib/elephant/providers/${provider}.so" "$HOME/.config/elephant/providers/"
            $VERBOSE_ECHO "Installed elephant provider: ${provider}"
          fi
        '') cfg.providers
      )}
    '';

    # Generate elephant config file
    xdg.configFile."elephant/elephant.toml" = mkIf (cfg.config != { }) {
      source = (pkgs.formats.toml { }).generate "elephant.toml" cfg.config;
    };

    systemd.user.services.elephant = mkIf cfg.installService {
      Unit = {
        Description = "Elephant launcher backend";
        After = [ "graphical-session-pre.target" ];
        PartOf = [ "graphical-session.target" ];
        ConditionEnvironment = "WAYLAND_DISPLAY";
      };

      Service = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/elephant ${optionalString cfg.debug "--debug"}";
        Restart = "on-failure";
        RestartSec = 1;

        # Clean up socket on stop
        ExecStopPost = "${pkgs.coreutils}/bin/rm -f /tmp/elephant.sock";
      };

      Install = {
        WantedBy = [ "graphical-session.target" ];
      };
    };
  };
}
