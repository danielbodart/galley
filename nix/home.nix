# galley's window as a socket-activated user service, for home-manager.
#
# The socket is always there; the window starts on the first question and
# stays, holding the queue, until the session ends. Nothing here puts galley
# where zenity was: a caller names galley.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.galley;
  # One Environment= assignment, quoted for systemd: % is a specifier,
  # and inside double quotes \ and " are escaped.
  assignment = s: "\"" + lib.escape [ "\\" "\"" ] (lib.replaceStrings [ "%" ] [ "%%" ] s) + "\"";
  environment = lib.optional (!cfg.accessibility) "GTK_A11Y=none"
    ++ lib.optional (cfg.services != { }) (assignment "GALLEY_SERVICES=${builtins.toJSON cfg.services}");
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
        as a screen reader needs. Off, the window starts with GTK_A11Y=none:
        that bus is open to any process in the session, and through it
        such a process could press Allow on a question it asked itself.
      '';
    };

    services = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      example = { ssh-signer = "SSH signer"; sudo-gate = "sudo"; };
      description = ''
        System users whose services may post to the window, by user name,
        each with the label the window shows for it. When any is set,
        galley-services.socket listens on /run/galley-ask/<user>.sock for
        them, in a directory the system must make (0755, owned by this
        user).
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    systemd.user.sockets.galley = {
      Unit.Description = "galley's question queue (socket)";
      Socket = {
        ListenStream = "%t/galley/sock";
        SocketMode = "0600";
        # The socket's privacy is its directory's: a password goes down it.
        DirectoryMode = "0700";
        FileDescriptorName = "user";
      };
      Install.WantedBy = [ "sockets.target" ];
    };

    # Its own unit, since SocketMode is a unit's.
    systemd.user.sockets.galley-services = lib.mkIf (cfg.services != { }) {
      Unit.Description = "galley's question queue for system services (socket)";
      Socket = {
        ListenStream = "/run/galley-ask/%u.sock";
        SocketMode = "0666";
        FileDescriptorName = "services";
        Service = "galley.service";
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
      Service = {
        ExecStart = "${cfg.package}/bin/galley-daemon";
      } // lib.optionalAttrs (environment != [ ]) {
        Environment = environment;
      };
    };
  };
}
