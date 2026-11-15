#!/usr/bin/env bash
# Tests scripts/release-json.sh against v0.39.29's real SHA256SUMS: the
# file it writes is the one flake.nix reads, with each hash in the SRI form
# `nix hash convert --hash-algo sha256 --to sri` gives (worked out with it
# once, and written here, since check runs where there may be no nix), and
# a SHA256SUMS missing a tarball, or a version that is not one, makes
# nothing. Run by mise run check (the scripts-test task).
set -euo pipefail
unset CDPATH

script=$(cd "$(dirname "$0")" && pwd)/release-json.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

cat >"$work/SHA256SUMS" <<'SUMS'
a17bdc3139ee172b8d0d3c760eeea0a9532f3faebd016de906d709f667208069  ./galley-linux-aarch64.tar.gz
db1c8000053cca0e8e646fc776bf4f22f8806928a6c403546dbe9547ff72fe9d  ./galley-linux-x86_64.tar.gz
SUMS

cat >"$work/want.json" <<'JSON'
{
  "version": "0.39.29",
  "assets": {
    "x86_64-linux": {
      "url": "https://github.com/danielbodart/galley/releases/download/v0.39.29/galley-linux-x86_64.tar.gz",
      "hash": "sha256-2xyAAAU8yg6OZG/Hdr9PIviAaSimxANUbb6VR/9y/p0="
    },
    "aarch64-linux": {
      "url": "https://github.com/danielbodart/galley/releases/download/v0.39.29/galley-linux-aarch64.tar.gz",
      "hash": "sha256-oXvcMTnuFyuNDTx2Du6gqVMvP669AW3pBtcJ9mcggGk="
    }
  }
}
JSON

if ! "$script" 0.39.29 "$work/SHA256SUMS" >"$work/got.json"; then
    fail "a release's SHA256SUMS: refused"
elif ! diff -u "$work/want.json" "$work/got.json"; then
    fail "a release's SHA256SUMS: not the release.json flake.nix wants"
fi

# The same, as sha256sum names files given without ./.
sed 's|  \./|  |' "$work/SHA256SUMS" >"$work/bare"
if ! "$script" 0.39.29 "$work/bare" | diff -q "$work/want.json" - >/dev/null; then
    fail "a SHA256SUMS naming the tarballs without ./: not the same release.json"
fi

grep -v aarch64 "$work/SHA256SUMS" >"$work/short"
if "$script" 0.39.29 "$work/short" >/dev/null 2>&1; then
    fail "a SHA256SUMS without aarch64: accepted"
fi

cat "$work/SHA256SUMS" "$work/SHA256SUMS" >"$work/twice"
if "$script" 0.39.29 "$work/twice" >/dev/null 2>&1; then
    fail "a SHA256SUMS naming each tarball twice: accepted"
fi

if "$script" v0.39.29 "$work/SHA256SUMS" >/dev/null 2>&1; then
    fail "a version with a v: accepted"
fi

if [ $failures -gt 0 ]; then
    exit 1
fi
echo "release-json: ok"
