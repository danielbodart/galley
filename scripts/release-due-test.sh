#!/usr/bin/env bash
# Tests scripts/release-due.sh (and scripts/last-release.sh, which it asks)
# against a throwaway repository: which changes since the last release make
# a release, and which do not, with the release tagged as CI tags it, on a
# child of the released trunk commit that adds nix/release.json, or as it
# was tagged before, on the trunk commit itself. Run by mise run check (the
# scripts-test task).
set -euo pipefail
unset CDPATH

src=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Nothing from the user's git config: no signing key, no hooks, no agent.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid
# A few of what mise.toml names, enough to tell released from not.
export RELEASE_PATHS="cmd daemon flake.nix nix/home.nix mise.toml"

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

# A repository whose one commit is released, its tag as CI makes it: a
# commit of its own, on no branch, whose parent is the trunk commit and
# which adds nix/release.json (ci.yml's release job). `fresh old` tags the
# trunk commit itself, as releases were tagged before.
fresh() {
    rm -rf "$work/repo"
    mkdir -p "$work/repo/scripts" "$work/repo/cmd" "$work/repo/daemon" "$work/repo/nix" "$work/repo/docs"
    cd "$work/repo"
    git init -q -b trunk
    cp "$src/release-due.sh" "$src/last-release.sh" scripts/
    echo '[tools]' > mise.toml
    echo 'package main' > cmd/main.go
    echo '// window' > daemon/main.js
    echo '{ }' > flake.nix
    echo '{ }' > nix/home.nix
    echo docs > docs/a.md
    echo lock > flake.lock
    git add -A
    git commit -qm base
    if [ "${1:-}" = old ]; then
        git tag v0.1.1
    else
        release v0.1.1
    fi
}

# release TAG: tags HEAD's release as CI does, on a child commit holding
# nix/release.json, and leaves trunk checked out where it was.
release() {
    git checkout -q --detach
    echo "{\"version\": \"${1#v}\"}" > nix/release.json
    # Even a change on the tag's commit to a released path must not count:
    # release-due diffs from the tag's parent.
    echo '// as the tag sees it' > daemon/main.js
    git add -A
    git commit -qm "Release $1"
    git tag "$1"
    git checkout -q trunk
}

# expect WANT DESCRIPTION: release-due.sh's output and status agree with WANT.
expect() {
    local want=$1 what=$2 out status=0
    git add -A
    git commit -qm "$what" "${@:3}"
    out=$(scripts/release-due.sh) || status=$?
    case $want in
    true) [ "$out" = true ] && [ $status -eq 0 ] || fail "$what: want a release, got '$out' (exit $status)" ;;
    false) [ "$out" = false ] && [ $status -eq 1 ] || fail "$what: want no release, got '$out' (exit $status)" ;;
    esac
}

fresh
echo 'package main // changed' > cmd/main.go
expect true "a change to the Go source"

fresh
echo '// changed' > daemon/main.js
expect true "a change to the window"

fresh
echo '{ x = 1; }' > flake.nix
expect true "a change to how the flake installs a release"

fresh
echo '{ x = 1; }' > nix/home.nix
expect true "a change to the home-manager module"

fresh
echo docs2 > docs/a.md
echo lock2 > flake.lock
expect false "the docs and flake.lock"

fresh
git tag -d v0.1.1 >/dev/null
echo docs2 > docs/a.md
expect true "no release tag yet"

fresh
expect false "nothing since the release (an empty commit)" --allow-empty

fresh old
echo docs2 > docs/a.md
expect false "the docs, since a release tagged on trunk itself"

fresh old
echo 'package main // changed' > cmd/main.go
expect true "the Go source, since a release tagged on trunk itself"

# A tagged-on-trunk release, then a tagged-on-a-child one: the newer is the
# last, so only what changed since it counts.
fresh old
echo 'package main // changed' > cmd/main.go
git commit -qam "source"
release v0.2.7
echo docs2 > docs/a.md
expect false "the docs, since the newer of an old-style and a new-style release"

# A release made from a commit after HEAD (trunk checked out behind it) is
# not HEAD's last release.
fresh
echo 'package main // changed' > cmd/main.go
git commit -qam "source"
release v0.2.9
git checkout -q --detach HEAD~1
echo docs2 > docs/a.md
expect false "the docs, behind a later release"

if [ $failures -gt 0 ]; then
    exit 1
fi
echo "release-due: ok"
