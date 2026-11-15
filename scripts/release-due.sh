#!/usr/bin/env bash
# Whether the commit checked out is due a release: prints true and exits 0
# if anything in RELEASE_PATHS (mise.toml, which says what is in it and
# why) changed since the last release, else prints false and exits 1. With
# no release yet, it is due. CI's due job and the weekly update ask it
# (mise run release-due, which sets RELEASE_PATHS).
#
# The last release is the trunk commit its tag's commit is a child of, not
# the tag's own commit (scripts/last-release.sh): that adds
# nix/release.json, which is the release's output, not its input.
set -euo pipefail
unset CDPATH
shopt -s inherit_errexit

: "${RELEASE_PATHS:?RELEASE_PATHS is not set: run this as mise run release-due}"
read -r -a paths <<<"$RELEASE_PATHS"

cd "$(git rev-parse --show-toplevel)"

due() { echo true; exit 0; }

read -r _ last < <(scripts/last-release.sh) || due

[ -z "$(git diff --name-only "$last" HEAD -- "${paths[@]}")" ] || due

echo false
exit 1
