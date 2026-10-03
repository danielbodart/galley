# galley

## Looking at and driving the window

To see the window, or click and type in it, while working on it, use the
`waydriver` MCP server. Don't start `gtk4-broadwayd` by hand for this.
WayDriver runs the window in a headless mutter of its own, with a private
session bus and runtime directory, so it never touches the desktop. It
presses keys and clicks through the compositor, the way a person would. It
reads the widget tree over AT-SPI and returns screenshots as images.
Broadway gives you none of that: GTK 4 has no way to synthesise a key event,
which is why the tests below have a test control.

Build the checkout first: `nix build .#galley`. Then start a session whose
command is a script along these lines:

```sh
result/bin/galley-daemon &
for i in $(seq 100); do [ -S "$XDG_RUNTIME_DIR/galley/sock" ] && break; sleep 0.1; done
result/bin/galley --question --title="Allow this request?" --text="…" &
sleep 0.5; result/bin/galley --show   # the window never raises itself
wait
```

- Use absolute paths to `result/bin`.
- Pass `app_name: "galley"`. That is the name the window gives AT-SPI (see
  `daemon/main.js`).
- The window joins AT-SPI only once it is shown, so the `--show` is not
  optional.
- Then use `dump_tree`, `take_screenshot`, `click_by_text`, `press_key`,
  `type_text` and the rest. To see the answer, have the script write the
  client's stdout and exit status to a file.

## Tests stay on broadway

The end-to-end and appearance tests run on broadway. Don't move them to
WayDriver. They run inside `nix flake check`'s sandbox, where there is no
mutter, PipeWire or AT-SPI. The README's Development section has the
commands.
