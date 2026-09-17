#!/usr/bin/env bash
# Builds one binary per supported platform for a release tag, plus a SHA256SUMS
# file beside them so two builds of the same tag can be compared. The same
# script runs in CI and on a laptop, so a release can be reproduced by hand.
set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: build-release.sh <tag> <output-directory>" >&2
    exit 2
fi

tag=$1
repository_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
binary=agent-to-nvim
platforms=(darwin/arm64 darwin/amd64 linux/amd64)

mkdir -p -- "$2"
output_dir=$(cd -- "$2" && pwd -P)

# A CI image carries sha256sum and macOS carries shasum; the two write the same
# line format.
if command -v sha256sum >/dev/null 2>&1; then
    checksum_command=(sha256sum)
else
    checksum_command=(shasum -a 256)
fi

# A binary left by an earlier tag would sit beside sums that do not list it, so
# this script's own output names are cleared out first.
rm -f -- "$output_dir"/${binary}_* "$output_dir/SHA256SUMS"

assets=()
for platform in "${platforms[@]}"; do
    operating_system=${platform%/*}
    architecture=${platform#*/}
    asset="${binary}_${tag}_${operating_system}_${architecture}"
    (
        cd "$repository_dir"
        CGO_ENABLED=0 GOOS="$operating_system" GOARCH="$architecture" \
            go build -trimpath -ldflags "-X main.version=$tag" \
            -o "$output_dir/$asset" .
    )
    assets+=("$asset")
    echo "built $asset"
done

# The sums are written from inside the output directory so the names in them
# stay relative, which is what a verifying `-c` run expects.
(cd "$output_dir" && "${checksum_command[@]}" "${assets[@]}" >SHA256SUMS)
echo "wrote SHA256SUMS for ${#assets[@]} binaries in $output_dir"
