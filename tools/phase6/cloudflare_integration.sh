#!/bin/sh
# No set -x: live tokens must never enter console or evidence.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
missing=false
for variable in CLOUDFLARE_API_TOKEN CLOUDFLARE_ZONE_ID PHASE6_DATABASE_URL; do
  case "$variable" in
    CLOUDFLARE_API_TOKEN) value=${CLOUDFLARE_API_TOKEN:-};;
    CLOUDFLARE_ZONE_ID) value=${CLOUDFLARE_ZONE_ID:-};;
    PHASE6_DATABASE_URL) value=${PHASE6_DATABASE_URL:-};;
  esac
  if test -z "$value"; then printf '%s required; real Cloudflare CRUD NOT EXECUTED\n' "$variable" >&2; missing=true; fi
done
unset value
if test -z "${CLOUDFLARE_TOKEN_POLICY_FILE:-}" && test -z "${CLOUDFLARE_POLICY_TOKEN:-}"; then
  echo 'reviewed CLOUDFLARE_TOKEN_POLICY_FILE required (or independent read-only inspector); real CRUD NOT EXECUTED' >&2
  missing=true
fi
test "$missing" = false || exit 78
if test -n "${CLOUDFLARE_TOKEN_POLICY_FILE:-}"; then
  case "$CLOUDFLARE_TOKEN_POLICY_FILE" in
    /*) ;;
    *) CLOUDFLARE_TOKEN_POLICY_FILE="$root/$CLOUDFLARE_TOKEN_POLICY_FILE";;
  esac
  export CLOUDFLARE_TOKEN_POLICY_FILE
fi
export GOTOOLCHAIN=local
test "$(go env GOVERSION)" = go1.27.3 || { echo 'Go 1.27.3 required' >&2; exit 1; }
cd backend
exec go test -tags cloudflareintegration ./internal/control -run '^TestCloudflareControlledCRUD$' -count=1 -v -timeout 3m
