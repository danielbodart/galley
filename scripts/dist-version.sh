#!/usr/bin/env bash
# The version the tarballs in DIR were built as, from the one directory
# each holds, galley-VERSION/ (scripts/dist.sh): what `mise run dist` gave
# them, which a local build's timestamp makes impossible to work out again.
#
# usage: scripts/dist-version.sh DIR
set -euo pipefail

dir=${1:?usage: scripts/dist-version.sh DIR}
versions=$(for f in "$dir"/galley-linux-*.tar.gz; do tar -tzf "$f" | awk -F/ 'NR == 1 { print $1 }'; done | sort -u)
if [ "$(wc -l <<<"$versions")" -ne 1 ] || [[ $versions != galley-* ]]; then
    echo "dist-version: the tarballs in $dir are not of one version: $versions" >&2
    exit 1
fi
echo "${versions#galley-}"
