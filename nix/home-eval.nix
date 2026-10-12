# The home-manager module, evaluated against just the options it sets, so
# the check needs no home-manager input: the socket private, the service
# running the window with its checked settings file and nothing of the
# session's environment changed, the settings file in ~/.config the same
# file, keyed as the options are, starting at login and again on failure
# only when services are named, and galley under its own name only. The
# settings are built, so a check of them that fails fails this.
{ pkgs, lib, self }:

let
  stub = {
    options = {
      home.packages = lib.mkOption { type = lib.types.listOf lib.types.package; default = [ ]; };
      systemd.user.services = lib.mkOption { type = lib.types.attrsOf lib.types.anything; default = { }; };
      systemd.user.sockets = lib.mkOption { type = lib.types.attrsOf lib.types.anything; default = { }; };
      xdg.configFile = lib.mkOption { type = lib.types.attrsOf lib.types.anything; default = { }; };
    };
  };
  eval = settings: (lib.evalModules {
    modules = [ stub self.homeModules.default { _module.args.pkgs = pkgs; services.galley = settings; } ];
  }).config;

  on = eval { enable = true; };
  accessible = eval { enable = true; accessibility = true; };
  withServices = eval { enable = true; services = { ssh-signer = "SSH signer"; sudo-gate = "50% \"sudo\""; }; };
  off = eval { };

  socket = on.systemd.user.sockets.galley.Socket;
  service = on.systemd.user.services.galley.Service;
  names = c: map (p: p.name) c.home.packages;
  # The settings file, which the result names, so building the check
  # builds it and runs `galley check` on it; and what it was written from.
  settings = c: c.xdg.configFile."galley/config.json".source;
  read = c: builtins.fromJSON (settings c).config;
in
assert lib.assertMsg (socket.ListenStream == "%t/galley/sock") "socket at ${socket.ListenStream}";
assert lib.assertMsg (socket.DirectoryMode == "0700" && socket.SocketMode == "0600") "socket not private";
assert lib.assertMsg (builtins.attrNames on.systemd.user.sockets == [ "galley" ]) "sockets ${toString (builtins.attrNames on.systemd.user.sockets)}";
assert lib.assertMsg (withServices.systemd.user.sockets == on.systemd.user.sockets) "services changed the sockets";
assert lib.assertMsg (!(on.systemd.user.services.galley ? Install)) "the window starts at login with no services";
assert lib.assertMsg (withServices.systemd.user.services.galley.Install.WantedBy == [ "graphical-session.target" ]) "the window with services does not start at login";
assert lib.assertMsg (lib.elem "graphical-session.target" withServices.systemd.user.services.galley.Unit.After) "the window with services not after the session";
assert lib.assertMsg (withServices.systemd.user.services.galley.Service.Restart == "on-failure") "the window with services not restarted";
assert lib.assertMsg (builtins.attrNames service == [ "ExecStart" ]) "service sets ${toString (builtins.attrNames service)}: the window takes the session's environment whole, its bus for the settings portal";
assert lib.assertMsg (service.ExecStart == "${self.packages.${pkgs.stdenv.hostPlatform.system}.galley}/bin/galley-daemon -config ${settings on}") "service runs ${service.ExecStart}";
assert lib.assertMsg (lib.all (c: c.systemd.user.services.galley.Service.ExecStart == "${builtins.head c.home.packages}/bin/galley-daemon -config ${settings c}") [ accessible withServices ]) "a service runs another file than its settings";
assert lib.assertMsg (read on == { accessibility = false; services = { }; }) "settings ${builtins.toJSON (read on)}";
assert lib.assertMsg (read accessible == { accessibility = true; services = { }; }) "accessible settings ${builtins.toJSON (read accessible)}";
assert lib.assertMsg (read withServices == { accessibility = false; services = { ssh-signer = "SSH signer"; sudo-gate = "50% \"sudo\""; }; }) "services settings ${builtins.toJSON (read withServices)}";
assert lib.assertMsg (off.xdg.configFile == { }) "disabled but has settings";
assert lib.assertMsg (names on == [ "galley-${self.packages.${pkgs.stdenv.hostPlatform.system}.galley.version}" ]) "packages ${toString (names on)}";
assert lib.assertMsg (off.systemd.user.sockets == { } && off.home.packages == [ ]) "disabled but configured";
{
  inherit socket service;
  settings = map settings [ on accessible withServices ];
}
