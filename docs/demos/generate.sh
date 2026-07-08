#!/usr/bin/env bash
# Builds the demo ultra binary and renders every tape in tapes/ to a gif under
# docs/assets/demos. Run it directly or through `mise run gen-docs --demos`.
set -euo pipefail

cd "$(dirname "$0")"

if ! command -v vhs >/dev/null 2>&1; then
	echo "vhs not found; install it with 'mise install' (it is pinned in mise.toml)" >&2
	exit 1
fi

for dep in ttyd ffmpeg; do
	if ! command -v "$dep" >/dev/null 2>&1; then
		echo "$dep not found; vhs needs it to render (brew install ttyd ffmpeg)" >&2
		exit 1
	fi
done

go build -o ultra .

for tape in tapes/*.tape; do
	echo "rendering $tape"
	vhs "$tape"
done
