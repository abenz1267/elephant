flake:
{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.programs.elephant;
  
  # Available providers
  providerOptions = {
    files = "File search and management";
    desktopapplications = "Desktop application launcher";
    calc = "Calculator and unit conversion";
    runner = "Command runner";
    clipboard = "Clipboard history management";
    symbols = "Symbols and emojis";
    websearch = "Web search integration";
    menus = "Custom menu system";
    providerlist = "Provider listing and management";
  };
in
{
  options.programs.elephant = {
    enable = mkEnableOption "Elephant launcher backend";

    package = mkOption {
      type = types.package;
      default = flake.packages.${pkgs.system}.elephant-with-providers;
      defaultText = literalExpression "flake.packages.\${pkgs.system}.elephant-with-providers";
      description = "The elephant package to use.";
    };

    providers = mkOption {
      type = types.listOf (types.enum (attrNames providerOptions));
      default = attrNames providerOptions;
      example = [ "files" "desktopapplications" "calc" ];
      description = ''
        List of providers to enable. Available providers:
        ${concatStringsSep "\n" (mapAttrsToList (name: desc: "  - ${name}: ${desc}") providerOptions)}
      '';
    };

    autoStart = mkOption {
      type = types.bool;
      default = false;
      description = "Whether to automatically start elephant service with the session.";
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
    home.packages = [ cfg.package ];

    # Install providers to user config
    home.activation.elephantProviders = lib.hm.dag.entryAfter ["writeBoundary"] ''
      $DRY_RUN_CMD mkdir -p $HOME/.config/elephant/providers
      
      # Copy enabled providers
      ${concatStringsSep "\n" (map (provider: ''
        if [[ -f "${cfg.package}/lib/elephant/providers/${provider}.so" ]]; then
          $DRY_RUN_CMD cp "${cfg.package}/lib/elephant/providers/${provider}.so" "$HOME/.config/elephant/providers/"
          $VERBOSE_ECHO "Installed elephant provider: ${provider}"
        fi
      '') cfg.providers)}
    '';

    # Generate elephant config file
    xdg.configFile."elephant/elephant.toml" = mkIf (cfg.config != {}) {
      source = (pkgs.formats.toml {}).generate "elephant.toml" cfg.config;
    };

    # Auto-start service if enabled
    systemd.user.services.elephant = mkIf cfg.autoStart {
      Unit = {
        Description = "Elephant launcher backend";
        After = [ "graphical-session-pre.target" ];
        PartOf = [ "graphical-session.target" ];
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