#!/usr/bin/env bash
# Publishes the plugin as the `dist` branch: the plugin tree at the branch root,
# with the launcher and one binary per platform in bin/. Claude Code clones the
# root of the ref a marketplace entry names, so the branch carries the plugin
# tree rather than this repository's layout, and the binaries are committed so
# an install brings them along.
#
# The marketplace file stays on main: Claude Code reads the catalogue from the
# default branch and the plugin from dist.
#
# The branch is rewritten as a single commit each time, so its history holds no
# binary blobs beyond the current release. That rewrite has no parent in common
# with what is on the remote, which --force-with-lease cannot express; the plain
# --force here is confined to this generated branch.
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
    echo "usage: publish-dist.sh <tag> <binaries-directory> [remote]" >&2
    exit 2
fi

tag=$1
binaries_dir=$(cd -- "$2" && pwd -P)
remote=${3:-origin}
repository_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
binary=agent-to-nvim
platforms=(darwin_arm64 darwin_amd64 linux_amd64)

fail() {
    echo "publish-dist: $1" >&2
    exit 1
}

for platform in "${platforms[@]}"; do
    [[ -f "$binaries_dir/${binary}_${tag}_${platform}" ]] ||
        fail "no ${binary}_${tag}_${platform} in $binaries_dir; run scripts/build-release.sh first"
done

if [[ -d $remote || $remote == *:* || $remote == *://* ]]; then
    remote_url=$remote
else
    remote_url=$(git -C "$repository_dir" remote get-url "$remote" 2>/dev/null) ||
        fail "no remote named $remote in this checkout"
fi

commit_name=$(git -C "$repository_dir" config user.name || true)
commit_email=$(git -C "$repository_dir" config user.email || true)
[[ -n $commit_name && -n $commit_email ]] ||
    fail "no git identity to commit with; configure user.name and user.email"

work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT

"$repository_dir/scripts/assemble-plugin.sh" "$work_dir"
for platform in "${platforms[@]}"; do
    install -m 0755 "$binaries_dir/${binary}_${tag}_${platform}" "$work_dir/bin/${binary}_${platform}"
done

# The tag is the version a laptop installs, and Claude Code keeps one cache
# directory per version, so the manifest on dist carries the tag.
manifest="$work_dir/.claude-plugin/plugin.json"
version=${tag#v}
version_lines=$(grep -c '"version"' "$manifest")
[[ $version_lines -eq 1 ]] ||
    fail "the plugin manifest holds $version_lines version keys, want exactly one"
# awk carries the tag as data rather than as a replacement pattern, so a tag
# holding punctuation lands verbatim, and the line is rebuilt around the colon
# so its indentation and its trailing comma survive.
awk -v version="$version" '
    /"version"/ {
        colon = index($0, ":")
        trailing = ($0 ~ /,[[:space:]]*$/) ? "," : ""
        printf "%s \"%s\"%s\n", substr($0, 1, colon), version, trailing
        next
    }
    { print }
' "$manifest" >"$manifest.rewritten"
mv -- "$manifest.rewritten" "$manifest"
grep -qF "\"version\": \"$version\"" "$manifest" ||
    fail "could not write the version $version into the plugin manifest"

cd "$work_dir"
git init -q .
git checkout -q -b dist
git config user.name "$commit_name"
git config user.email "$commit_email"
git add .
git commit -q -m "agent-to-nvim $tag"
git push --force "$remote_url" "HEAD:refs/heads/dist"
echo "published $tag to the dist branch"
