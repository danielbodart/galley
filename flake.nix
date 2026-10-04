{
  description = "A zenity drop-in whose dialogs stack in one window";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;

      # MAJOR.MINOR.PATCH as scripts/version.sh makes it, as far as Nix can:
      # the MAJOR is committed, 0 saying the interfaces are still moving; the
      # MINOR, the commit count, Nix knows only from a git+ ref (a github:
      # ref, or a tag of a release, gives none, so 0); the PATCH, CI's run
      # number, it never knows. The release tarballs carry the whole of it.
      major = nixpkgs.lib.fileContents ./VERSION;
      version = "${major}.${toString (self.revCount or 0)}.0";

      # What the window is built of: GJS, and the libraries it reaches
      # through GObject introspection.
      windowInputs = pkgs: [ pkgs.gjs pkgs.gtk4 pkgs.libadwaita pkgs.glib ];

      mkGalley = pkgs: pkgs.buildGoModule {
        pname = "galley";
        inherit version;
        src = ./.;

        # No dependencies beyond the standard library: the client is the
        # thin end, and a password passes through it.
        vendorHash = null;

        # Static, so the client starts in a millisecond or two: it runs once
        # per question, and a sudo prompt waits on it.
        env.CGO_ENABLED = 0;
        ldflags = [ "-s" "-w" "-X" "main.version=${version}" ];
        subPackages = [ "cmd/galley" ];

        nativeBuildInputs = [ pkgs.wrapGAppsHook4 pkgs.gobject-introspection ];
        buildInputs = windowInputs pkgs;
        # Only the window is wrapped: the client needs none of GTK, and a
        # wrapper script would cost it a shell's start.
        dontWrapGApps = true;

        postInstall = ''
          # The window. test-control.js is not installed: it is how the
          # end-to-end check presses keys, and a window without it has no way
          # to be answered but a person.
          mkdir -p $out/share/galley
          for f in daemon/*.js; do
            [ "$(basename "$f")" = test-control.js ] && continue
            install -m644 "$f" $out/share/galley/
          done
          cat > $out/bin/galley-daemon <<EOF
          #!${pkgs.runtimeShell}
          exec ${pkgs.gjs}/bin/gjs -m $out/share/galley/main.js "\$@"
          EOF
          chmod +x $out/bin/galley-daemon

          # Its desktop file, which GNOME needs to show its notifications.
          mkdir -p $out/share/applications
          substitute nix/io.github.danielbodart.Galley.desktop \
            $out/share/applications/io.github.danielbodart.Galley.desktop \
            --replace-fail @galley@ $out/bin/galley

          # The user units, for a system without the home-manager module.
          mkdir -p $out/share/systemd/user
          install -m644 nix/galley.socket $out/share/systemd/user/galley.socket
          substitute nix/galley.service $out/share/systemd/user/galley.service \
            --replace-fail @daemon@ $out/bin/galley-daemon
        '';

        postFixup = ''
          wrapGApp $out/bin/galley-daemon
        '';

        doCheck = true;

        meta = {
          description = "A zenity drop-in whose dialogs stack in one window";
          homepage = "https://github.com/danielbodart/galley";
          license = nixpkgs.lib.licenses.mit;
          mainProgram = "galley";
          platforms = nixpkgs.lib.platforms.linux;
        };
      };
    in
    {
      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in
        rec {
          galley = mkGalley pkgs;
          default = galley;
        });

      # The window as a socket-activated user service. See nix/home.nix.
      homeModules.default = import ./nix/home.nix self;

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in
        {
          default = pkgs.mkShell {
            packages = [ pkgs.go pkgs.gopls pkgs.gtk4 pkgs.dbus ] ++ windowInputs pkgs;
            nativeBuildInputs = [ pkgs.gobject-introspection ];
            CGO_ENABLED = "0";
          };
        });

      checks = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          pkg = self.packages.${system}.galley;
        in
        {
          # The build, which is also the client's tests.
          inherit (self.packages.${system}) galley;

          gofmt = pkgs.runCommand "gofmt"
            { nativeBuildInputs = [ pkgs.go ]; }
            ''
              cd ${./.}
              unformatted=$(gofmt -l .)
              if [ -n "$unformatted" ]; then
                echo "not gofmt'd:" >&2
                echo "$unformatted" >&2
                exit 1
              fi
              touch $out
            '';

          govet = pkg.overrideAttrs (_: {
            pname = "galley-vet";
            checkPhase = ''
              runHook preCheck
              go vet ./...
              runHook postCheck
            '';
          });

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
          # broadway and a private session bus, with a stand-in portal. And
          # the package's window finds the desktop's settings: its wrapper
          # carries the schemas and dconf's GSettings backend.
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
              for want in gsettings-desktop-schemas dconf; do
                grep -q "$want" ${pkg}/bin/galley-daemon || {
                  echo "galley-daemon's wrapper has no $want" >&2
                  exit 1
                }
              done
              touch $out
            '';

          # The client against the real window, drawn by GTK's broadway
          # backend so there is no display to need: every mode's exit code
          # and output, stacking, withdrawal and the keys, pressed through a
          # test-only control the package does not ship.
          end-to-end = pkg.overrideAttrs (old: {
            pname = "galley-end-to-end";
            nativeCheckInputs = (old.nativeCheckInputs or [ ]) ++ [ pkgs.gtk4 pkgs.gjs ];
            preCheck = ''
              export GALLEY_E2E_DAEMON="${pkgs.gjs}/bin/gjs -m $PWD/daemon/main.js"
              export GALLEY_E2E_BROADWAYD=${pkgs.gtk4}/bin/gtk4-broadwayd
              export HOME=$TMPDIR
              # GTK's own schemas, as the package's wrapper gives the window:
              # the colour chooser keeps its custom colours in GSettings, and
              # GLib aborts on a schema it cannot find.
              export XDG_DATA_DIRS=${pkgs.gtk4}/share/gsettings-schemas/${pkgs.gtk4.name}''${XDG_DATA_DIRS:+:$XDG_DATA_DIRS}
              export GSETTINGS_BACKEND=memory
            '';
            checkPhase = ''
              runHook preCheck
              go test -count=1 -v ./tests/
              runHook postCheck
            '';
          });

          shellcheck = pkgs.runCommand "galley-shellcheck"
            { nativeBuildInputs = [ pkgs.shellcheck ]; }
            ''
              cd ${./.}
              shellcheck scripts/*.sh packaging/install.sh packaging/galley-daemon
              touch $out
            '';

          # The release tarball for a system without Nix, as CI makes it,
          # installed into a prefix and taken out again: the client says the
          # version it was given, the units and desktop file name where the
          # programs went, and the test control stays behind.
          dist = pkgs.runCommand "galley-dist"
            {
              nativeBuildInputs = [ pkgs.go pkgs.gjs pkgs.gobject-introspection ];
              buildInputs = windowInputs pkgs;
            }
            ''
              export HOME=$TMPDIR GOCACHE=$TMPDIR/go-cache GOPROXY=off
              cp -r ${./.} src && chmod -R u+w src
              arch=${pkgs.stdenv.hostPlatform.uname.processor}
              bash src/scripts/dist.sh 7.8.9 "$arch" out
              tar -xzf out/galley-linux-$arch.tar.gz
              p=$TMPDIR/prefix
              sh galley-7.8.9/install.sh --prefix=$p --no-systemd
              [ "$($p/bin/galley --version)" = 7.8.9 ]
              grep -qx "ExecStart=$p/bin/galley-daemon" $p/share/systemd/user/galley.service
              grep -qx "Exec=$p/bin/galley --show" $p/share/applications/io.github.danielbodart.Galley.desktop
              [ -f $p/share/galley/main.js ] && [ ! -e $p/share/galley/test-control.js ]
              sh galley-7.8.9/install.sh --prefix=$p --no-systemd --uninstall
              [ -z "$(find $p -type f)" ]
              touch $out
            '';

          # The home-manager module's units, as written: the socket in the
          # private runtime directory, the service running the window.
          home-module =
            let
              eval = import ./nix/home-eval.nix { inherit pkgs self; lib = nixpkgs.lib; };
            in
            pkgs.writeText "galley-home-module" (builtins.toJSON eval);
        });

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixpkgs-fmt);
    };
}
