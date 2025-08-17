flake:
{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.elephant;
in
{
  options.services.elephant = {
    enable = mkEnableOption "Elephant launcher backend system service";

    package = mkOption {
      type = types.package;
      default = flake.packages.${pkgs.system}.elephant-with-providers;
      defaultText = literalExpression "flake.packages.\${pkgs.system}.elephant-with-providers";
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

    debug = mkOption {
      type = types.bool;
      default = false;
      description = "Enable debug logging.";
    };

    config = mkOption {
      type = types.attrs;
      default = {};
      description = "Elephant configuration as Nix attributes.";
    };
  };

  config = mkIf cfg.enable {
    users.users.${cfg.user} = {
      description = "Elephant launcher backend user";
      group = cfg.group;
      isSystemUser = true;
      home = "/var/lib/elephant";
      createHome = true;
    };

    users.groups.${cfg.group} = {};

    # Install providers system-wide
    environment.etc."xdg/elephant/providers" = {
      source = "${cfg.package}/lib/elephant/providers";
    };

    # System-wide config
    environment.etc."xdg/elephant/elephant.toml" = mkIf (cfg.config != {}) {
      source = (pkgs.formats.toml {}).generate "elephant.toml" cfg.config;
    };

    systemd.services.elephant = {
      description = "Elephant launcher backend";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

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
        ReadWritePaths = [ "/var/lib/elephant" "/tmp" ];
        
        # Clean up socket on stop
        ExecStopPost = "${pkgs.coreutils}/bin/rm -f /tmp/elephant.sock";
      };

      environment = {
        HOME = "/var/lib/elephant";
      };
    };

    # Add elephant to system packages
    environment.systemPackages = [ cfg.package ];
  };
}