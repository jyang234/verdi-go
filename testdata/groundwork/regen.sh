#!/usr/bin/env bash
# Regenerate the committed groundwork graph goldens from the fixture services.
#
# These goldens are the *input* to groundwork's tests: groundwork consumes
# flowmap's graph JSON, so its fixtures are graphs, not source. We commit the
# source modules too (for provenance and so a flowmap change that shifts the
# graph shape is caught here, the same discipline flowmap uses for its boundary
# contract). Run from the repo root after changing a fixture or the graph schema:
#
#     go run ./cmd/flowmap >/dev/null   # ensure it builds
#     testdata/groundwork/regen.sh
#     git diff testdata/groundwork/goldens   # review the delta
set -euo pipefail
cd "$(dirname "$0")/../.."

flowmap() { go run ./cmd/flowmap "$@"; }

for svc in layeredsvc blindsvc; do
	dir="testdata/groundwork/$svc"
	out="testdata/groundwork/goldens/$svc.graph.json"
	flowmap graph "$dir" >"$out"
	echo "wrote $out"
done
