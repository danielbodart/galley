{
  description = "A zenity drop-in whose dialogs stack in one window";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      lib = nixpkgs.lib;
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = lib.genAttrs systems;

      # Go at the minor mise.toml pins, for the dev shell and the end-to-end
      # check's test binary. The client itself is never built here (below).
      go = pkgs: pkgs.go_1_27;

      # What the window is built of: GJS, and the libraries it reaches
      # through GObject introspection.
      windowInputs = pkgs: [ pkgs.gjs pkgs.gtk4 pkgs.libadwaita pkgs.glib ];

      # THE PACKAGE IS THE RELEASE TARBALL, not a build from source (Dan,
      # 2026-11-15): the client is built once, by mise's Go in CI's dist
      # job, installed and checked there, signed and published, and building
      # it again here only made a second client that was not the one
      # released. So a release's tag is a commit of its own, a child of the
      # released trunk commit, that adds nix/release.json: the version, and
      # for each system the tarball's URL and hash (scripts/release-json.sh
      # writes it; ci.yml's release job commits and tags it). A flake
      # fetched as github:danielbodart/galley/v<V> cannot see its tag, only
      # its commit, which is why the version is in the file.
      #
      # On trunk there is no such file and no package: pin a release tag.
      # Absent rather than an error, so `nix flake check` and the dev shell
      # still work on trunk.
      releaseFile = ./nix/release.json;
      release =
        if builtins.pathExists releaseFile
        then lib.importJSON releaseFile
        else null;

      # The tarball installed as the flake's package always was: the client
      # byte for byte the release's (static, so neither stripped nor
      # patched, which would make it differ from the file the tarball's
      # SHA256SUMS line vouches for), and the window, which is JavaScript,
      # run by nixpkgs' gjs with GTK 4 and libadwaita through wrapGAppsHook4
      # rather than by the system's gjs as the tarball's own galley-daemon
      # is.
      mkGalley = pkgs: { version, tarball }: pkgs.stdenvNoCC.mkDerivation {
        pname = "galley";
        inherit version;
        src = tarball;
        # dist.sh lays the tarball out as it installs, under galley-VERSION/.
        sourceRoot = "galley-${version}";
        dontConfigure = true;
        dontBuild = true;

        nativeBuildInputs = [ pkgs.wrapGAppsHook4 pkgs.gobject-introspection ];
        buildInputs = windowInputs pkgs;
        # Only the window is wrapped: the client needs none of GTK, and a
        # wrapper script would cost it a shell's start.
        dontWrapGApps = true;
        dontStrip = true;
        dontPatchELF = true;

        installPhase = ''
          runHook preInstall

          install -Dm0755 bin/galley $out/bin/galley

          # The window, as the tarball has it: without the test control,
          # which dist.sh leaves out.
          install -d $out/share/galley
          install -m644 share/galley/*.js $out/share/galley/
          cat > $out/bin/galley-daemon <<EOF
          #!${pkgs.runtimeShell}
          exec ${pkgs.gjs}/bin/gjs -m $out/share/galley/main.js "\$@"
          EOF
          chmod +x $out/bin/galley-daemon

          # Its desktop file, which GNOME needs to show its notifications.
          install -d $out/share/applications
          substitute share/applications/io.github.danielbodart.Galley.desktop \
            $out/share/applications/io.github.danielbodart.Galley.desktop \
            --replace-fail @galley@ $out/bin/galley

          # The user units, for a system without the home-manager module.
          install -Dm644 share/systemd/user/galley.socket $out/share/systemd/user/galley.socket
          substitute share/systemd/user/galley.service $out/share/systemd/user/galley.service \
            --replace-fail @daemon@ $out/bin/galley-daemon

          runHook postInstall
        '';

        postFixup = ''
          wrapGApp $out/bin/galley-daemon
        '';

        meta = {
          description = "A zenity drop-in whose dialogs stack in one window";
          homepage = "https://github.com/danielbodart/galley";
          license = lib.licenses.mit;
          mainProgram = "galley";
          # The two the release ships, as release.json lists them.
          platforms = if release == null then [ ] else builtins.attrNames release.assets;
          # The client is the release's binary; the window is its source.
          sourceProvenance = [ lib.sourceTypes.binaryNativeCode lib.sourceTypes.fromSource ];
        };
      };

      # The release's package for each system it has a tarball for.
      releasePackages =
        lib.mapAttrs
          (system: asset:
            let
              pkgs = nixpkgs.legacyPackages.${system};
              galley = mkGalley pkgs {
                inherit (release) version;
                tarball = pkgs.fetchurl { inherit (asset) url hash; };
              };
            in
            { inherit galley; default = galley; })
          release.assets;
    in
    {
      packages = if release == null then { } else releasePackages;

      # The window as a socket-activated user service. See nix/home.nix.
      homeModules.default = import ./nix/home.nix self;

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in
        {
          default = pkgs.mkShell {
            packages = [ (go pkgs) pkgs.gopls pkgs.gtk4 pkgs.dbus ] ++ windowInputs pkgs;
            nativeBuildInputs = [ pkgs.gobject-introspection ];
            CGO_ENABLED = "0";
          };
        });

      # What needs Nix to check. gofmt, vet and the client's tests are mise
      # run check's, and the tarball installed on a system without Nix is
      # CI's dist job's (mise run dist-check). The checks of the package --
      # it, its wrapper, the end-to-end test, the home-manager module --
      # exist where nix/release.json does: on a release tag, and in CI's nix
      # job, which writes one for the tarballs its own run built
      # (scripts/nix-local-release.sh; mise run nix-check does the same
      # here).
      checks = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          pkg = self.packages.${system}.galley;
        in
        {
          # The window's own logic -- the queue's order and the sanitising of
          # markup -- under plain gjs, with no display.
          window-units = pkgs.runCommand "galley-window-units"
            {
              # gobject-introspection's setup hook gathers GI_TYPELIB_PATH
              # from the inputs, as it does for the package.
              nativeBuildInputs = [ pkgs.gjs pkgs.gobject-introspection ];
              buildInputs = windowInputs pkgs;
            }
            ''
              cd ${./.}
              gjs -m tests/units.js
              touch $out
            '';

          # The window follows the desktop's dark style, accent colour and
          # high contrast, at start and as they change while it is open, by
          # the settings portal and by GSettings: the real window on
          # broadway and a private session bus, with a stand-in portal.
          appearance = pkgs.runCommand "galley-appearance"
            {
              nativeBuildInputs = [ pkgs.gjs pkgs.gobject-introspection pkgs.gtk4 pkgs.dbus ];
              buildInputs = windowInputs pkgs;
              GSETTINGS_SCHEMA_DIR = "${pkgs.gsettings-desktop-schemas}/share/gsettings-schemas/${pkgs.gsettings-desktop-schemas.name}/glib-2.0/schemas";
            }
            ''
              export HOME=$TMPDIR
              cd ${./.}
              gjs -m tests/appearance.js
              touch $out
            '';

          shellcheck = pkgs.runCommand "galley-shellcheck"
            { nativeBuildInputs = [ pkgs.shellcheck ]; }
            ''
              cd ${./.}
              shellcheck scripts/*.sh packaging/install.sh packaging/galley-daemon
              touch $out
            '';
        } // lib.optionalAttrs (self.packages ? ${system}) {
          galley = pkg;

          # The package's window finds the desktop's settings, as the
          # appearance check's does: its wrapper carries the schemas and
          # dconf's GSettings backend.
          wrapper = pkgs.runCommand "galley-wrapper" { } ''
            for want in gsettings-desktop-schemas dconf; do
              grep -q "$want" ${pkg}/bin/galley-daemon || {
                echo "galley-daemon's wrapper has no $want" >&2
                exit 1
              }
            done
            touch $out
          '';

          # The release's client against the real window, drawn by GTK's
          # broadway backend so there is no display to need: every mode's
          # exit code and output, stacking, withdrawal and the keys, pressed
          # through a test-only control the package does not ship. Go builds
          # the tests, not the client: GALLEY_E2E_CLIENT is the package's.
          end-to-end = pkgs.runCommand "galley-end-to-end"
            {
              nativeBuildInputs = [ (go pkgs) pkgs.gtk4 pkgs.gjs pkgs.gobject-introspection ];
              buildInputs = windowInputs pkgs;
            }
            ''
              export HOME=$TMPDIR GOCACHE=$TMPDIR/go-cache GOPROXY=off GOTOOLCHAIN=local CGO_ENABLED=0
              cp -r ${./.} src && chmod -R u+w src && cd src
              export GALLEY_E2E_CLIENT=${pkg}/bin/galley
              export GALLEY_E2E_DAEMON="${pkgs.gjs}/bin/gjs -m $PWD/daemon/main.js"
              export GALLEY_E2E_BROADWAYD=${pkgs.gtk4}/bin/gtk4-broadwayd
              # GTK's own schemas, as the package's wrapper gives the window:
              # the colour chooser keeps its custom colours in GSettings, and
              # GLib aborts on a schema it cannot find.
              export XDG_DATA_DIRS=${pkgs.gtk4}/share/gsettings-schemas/${pkgs.gtk4.name}''${XDG_DATA_DIRS:+:$XDG_DATA_DIRS}
              export GSETTINGS_BACKEND=memory
              go test -count=1 -v ./tests/
              touch $out
            '';

          # The home-manager module's units, as written: the socket in the
          # private runtime directory, the service running the window.
          home-module =
            let
              eval = import ./nix/home-eval.nix { inherit pkgs self lib; };
            in
            pkgs.writeText "galley-home-module" (builtins.toJSON eval);
        });

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixpkgs-fmt);
    };
}
