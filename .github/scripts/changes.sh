#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Decides which CI jobs a change needs, so documentation-only changes skip the build
# and test pipeline. Writes go/web/images/deps/workflows=true|false to $GITHUB_OUTPUT.
#
# Everything runs for scheduled and manual runs, for new branches (no base to compare
# with), and when anything under .github/ changes (the pipeline itself changed).
#
# Inputs (environment): EVENT, BASE (the commit to compare HEAD with).
set -euo pipefail

out="${GITHUB_OUTPUT:-/dev/stdout}"
keys=(go web images deps workflows)

run_all() {
	echo "Running every job: $1"
	for k in "${keys[@]}"; do echo "$k=true" >>"$out"; done
	exit 0
}

case "${EVENT:-}" in
schedule | workflow_dispatch) run_all "$EVENT run" ;;
esac
if [ -z "${BASE:-}" ] || [ "$BASE" = 0000000000000000000000000000000000000000 ] ||
	! git cat-file -e "$BASE^{commit}" 2>/dev/null; then
	run_all "no base commit to compare with"
fi

files=$(git diff --name-only "$BASE" HEAD)
echo "Changed files:"
while IFS= read -r f; do echo "  $f"; done <<<"$files"

has() { if grep -qE "$1" <<<"$files"; then echo true; else echo false; fi; }

if [ "$(has '^\.github/')" = true ]; then
	run_all "the CI configuration changed"
fi

{
	echo "go=$(has '^(go\.(mod|sum)|cmd/|internal/|tools/|sqlc\.yaml|\.golangci\.yml|Makefile)')"
	echo "web=$(has '^web/')"
	echo "images=$(has '^(Dockerfile|\.dockerignore|deploy/|compose[^/]*\.ya?ml|go\.(mod|sum)|cmd/|internal/|web/)')"
	echo "deps=$(has '(^|/)(go\.(mod|sum)|package\.json|pnpm-lock\.yaml)$')"
	echo "workflows=false"
} | tee -a "$out"
