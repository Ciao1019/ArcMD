#!/bin/sh
set -eu

# Resolve the project from the script so it can be launched from any directory.
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

# Keep development settings and documents separate from the installed app.
export ARCMD_HOME="${ARCMD_HOME:-$project_dir/.mygo/data}"
export CGO_ENABLED=0

printf 'Development data: %s\n' "$ARCMD_HOME"
exec go run github.com/egoist/mygo/cmd/mygo@v0.7.4 dev "$@"
