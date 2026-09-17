#!/usr/bin/env bash
# Copies the Claude Code plugin tree out of this repository into a directory of
# its own. The repository root doubles as the plugin root, so the Go sources
# beside the plugin files have to be left behind: an install carries only what
# Claude Code reads plus the launcher in bin/.
#
# The caller puts the platform binaries in <destination>/bin.
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "usage: assemble-plugin.sh <destination>" >&2
    exit 2
fi

repository_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
mkdir -p -- "$1"
destination=$(cd -- "$1" && pwd -P)

mkdir -p "$destination/.claude-plugin" "$destination/bin"
cp "$repository_dir/.claude-plugin/plugin.json" "$destination/.claude-plugin/plugin.json"
cp -R "$repository_dir/hooks" "$destination/hooks"
cp -R "$repository_dir/skills" "$destination/skills"
cp "$repository_dir/README.md" "$destination/README.md"
if [[ -f "$repository_dir/LICENSE" ]]; then
    cp "$repository_dir/LICENSE" "$destination/LICENSE"
fi
install -m 0755 "$repository_dir/bin/agent-to-nvim" "$destination/bin/agent-to-nvim"
