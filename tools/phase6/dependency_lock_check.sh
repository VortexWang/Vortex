#!/bin/sh
# Fail closed on generator bytes, resolved dependencies and reviewed licenses.
set -eu
export PYTHONDONTWRITEBYTECODE=1
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
python3 -m unittest discover -s tools/phase6 -p 'test_sqlc_license_check.py'
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
work=$(mktemp -d /tmp/vortex-phase6-lock.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
sh tools/phase6/install_sqlc.sh "$work"
if command -v sqlc >/dev/null 2>&1; then cmp -s "$(command -v sqlc)" "$work/sqlc"; fi
for destination in one two; do
  mkdir "$work/$destination"
  cp -R backend/db/migrations backend/db/query backend/db/sqlc.yaml "$work/$destination/"
  "$work/sqlc" generate -f "$work/$destination/sqlc.yaml"
done
for source in "$work/one/generated/"*.go; do
  name=$(basename "$source")
  cmp -s "$source" "$work/two/generated/$name"
  cmp -s "$source" "backend/db/generated/$name"
done
test "$(find backend/db/generated -name '*.go' | wc -l | tr -d ' ')" = "$(find "$work/one/generated" -name '*.go' | wc -l | tr -d ' ')"
echo 'sqlc v1.31.1: two clean generations and committed source match byte-for-byte'
python3 tools/phase6/dependency_lock_check.py "$work"
