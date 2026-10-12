# galley's window as a socket-activated user service, for home-manager.
#
# The socket is always there; the window starts on the first question, or at
# login when services are set, and stays, holding the queue, until the
# session ends. Nothing here puts galley where zenity was: a caller names
# galley. accessibility and services are the window's settings, written to
# ~/.config/galley/config.json under the same names and checked by
# `galley check` when the configuration is built.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.galley;

  # The window's settings, keyed as these options are, checked as the window
  # will read them when the configuration is built.
  configFile = pkgs.runCommand "galley-config.json"
    {
      nativeBuildInputs = [ cfg.package ];
      config = builtins.toJSON { inherit (cfg) accessibility services; };
      passAsFile = [ "config" ];
    } ''
    cp "$configPath" "$out"
    galley check "$out"
  '';
in
{
  options.services.galley = {
    enable = lib.mkEnableOption "galley, a zenity drop-in whose dialogs stack in one window";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.galley;
      defaultText = lib.literalExpression "galley.packages.\${system}.galley";
      description = "The galley package: the `galley` client and the window it talks to.";
    };

    accessibility = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Let the window be read and worked through GTK's accessibility bus,
        as a screen reader needs. Off, the window sets GTK_A11Y=none before
        it makes a widget: that bus is open to any process in the session,
        and through it such a process could press Allow on a question it
        asked itself.
      '';
    };

    services = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      example = { ssh-signer = "SSH signer"; sudo-gate = "sudo"; };
      description = ''
        System users whose services may post to the window, by user name,
        each with the label the window shows for it. When any is set, the
        window starts at login and binds /run/galley-ask/<user>.sock for
        them, a directory the system must make (0755, owned by this user).
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    # Where the window looks by default, so one started by hand finds it too.
    xdg.configFile."galley/config.json".source = configFile;

    systemd.user.sockets.galley = {
      Unit.Description = "galley's question queue (socket)";
      Socket = {
        ListenStream = "%t/galley/sock";
        SocketMode = "0600";
        # The socket's privacy is its directory's: a password goes down it.
        DirectoryMode = "0700";
      };
      Install.WantedBy = [ "sockets.target" ];
    };

    systemd.user.services.galley = {
      Unit = {
        Description = "galley's question queue";
        Requires = [ "galley.socket" ];
        After = [ "galley.socket" "graphical-session.target" ];
      };
      # Nothing else of the environment is set or dropped: the window takes
      # the user manager's, which GNOME fills from the session's, and with it
      # the session bus, where the settings portal tells libadwaita the
      # desktop's dark style, accent colour and contrast.
      # The settings are named by their store path, the same file as the one
      # in ~/.config, so that a change to them changes the unit, which
      # restarts the window: it reads them once, at start.
      Service = {
        ExecStart = "${cfg.package}/bin/galley-daemon -config ${configFile}";
      } // lib.optionalAttrs (cfg.services != { }) {
        # Nothing else brings the services' socket back.
        Restart = "on-failure";
      };
    } // lib.optionalAttrs (cfg.services != { }) {
      # The window binds the services' socket itself, so that a service's
      # peer is the window; it starts at login so the socket is there first.
      Install.WantedBy = [ "graphical-session.target" ];
    };
  };
}
