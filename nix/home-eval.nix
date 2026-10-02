# The home-manager module, evaluated against just the options it sets, so
# the check needs no home-manager input: the socket private, the service
# running the window, the zenity name only when asked for.
{ pkgs, lib, self }:

let
  stub = {
    options = {
      home.packages = lib.mkOption { type = lib.types.listOf lib.types.package; default = [ ]; };
      systemd.user.services = lib.mkOption { type = lib.types.attrsOf lib.types.anything; default = { }; };
      systemd.user.sockets = lib.mkOption { type = lib.types.attrsOf lib.types.anything; default = { }; };
    };
  };
  eval = settings: (lib.evalModules {
    modules = [ stub self.homeModules.default { _module.args.pkgs = pkgs; services.galley = settings; } ];
  }).config;

  on = eval { enable = true; };
  zenity = eval { enable = true; zenity = true; };
  off = eval { };

  socket = on.systemd.user.sockets.galley.Socket;
  service = on.systemd.user.services.galley.Service;
  names = c: map (p: p.name) c.home.packages;
in
assert lib.assertMsg (socket.ListenStream == "%t/galley/sock") "socket at ${socket.ListenStream}";
assert lib.assertMsg (socket.DirectoryMode == "0700" && socket.SocketMode == "0600") "socket not private";
assert lib.assertMsg (service.ExecStart == "${self.packages.${pkgs.stdenv.hostPlatform.system}.galley}/bin/galley-daemon") "service runs ${service.ExecStart}";
assert lib.assertMsg (builtins.length on.home.packages == 1) "packages ${toString (names on)}";
assert lib.assertMsg (builtins.elem "galley-zenity" (names zenity)) "no zenity: ${toString (names zenity)}";
assert lib.assertMsg (off.systemd.user.sockets == { } && off.home.packages == [ ]) "disabled but configured";
{
  inherit socket service;
}
