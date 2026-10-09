# The home-manager module, evaluated against just the options it sets, so
# the check needs no home-manager input: the socket private, the service
# running the window without the accessibility bus unless asked and with
# nothing else of the session's environment changed, the services' socket
# only when services are named, and galley under its own name only.
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
  accessible = eval { enable = true; accessibility = true; };
  withServices = eval { enable = true; services = { ssh-signer = "SSH signer"; sudo-gate = "50% \"sudo\""; }; };
  off = eval { };

  socket = on.systemd.user.sockets.galley.Socket;
  service = on.systemd.user.services.galley.Service;
  names = c: map (p: p.name) c.home.packages;
in
assert lib.assertMsg (socket.ListenStream == "%t/galley/sock") "socket at ${socket.ListenStream}";
assert lib.assertMsg (socket.DirectoryMode == "0700" && socket.SocketMode == "0600") "socket not private";
assert lib.assertMsg (service.ExecStart == "${self.packages.${pkgs.stdenv.hostPlatform.system}.galley}/bin/galley-daemon") "service runs ${service.ExecStart}";
assert lib.assertMsg (service.Environment == [ "GTK_A11Y=none" ]) "service environment ${toString (service.Environment or [ ])}";
assert lib.assertMsg (builtins.attrNames service == [ "Environment" "ExecStart" ]) "service sets ${toString (builtins.attrNames service)}: the window takes the session's environment whole, its bus for the settings portal";
assert lib.assertMsg (!(accessible.systemd.user.services.galley.Service ? Environment)) "accessibility asked for but still off";
assert lib.assertMsg
  (withServices.systemd.user.services.galley.Service.Environment == [
    "GTK_A11Y=none"
    "GALLEY_SERVICES_SOCKET=/run/galley-ask/%u.sock"
    ''"GALLEY_SERVICES={\"ssh-signer\":\"SSH signer\",\"sudo-gate\":\"50%% \\\"sudo\\\"\"}"''
  ]) "services environment ${toString withServices.systemd.user.services.galley.Service.Environment}";
assert lib.assertMsg (names on == [ "galley-${self.packages.${pkgs.stdenv.hostPlatform.system}.galley.version}" ]) "packages ${toString (names on)}";
assert lib.assertMsg (off.systemd.user.sockets == { } && off.home.packages == [ ]) "disabled but configured";
{
  inherit socket service;
}
