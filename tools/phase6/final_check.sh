#!/bin/sh
# One host producer. Successful runtime evidence is not a successful pair.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
producer=${1:?docker or actions required}
evidence=${2:?absolute outside-source evidence directory required}
: "${PHASE6_EXPECTED_SHA:?expected SHA required}"
case "$producer" in docker) test "${GITHUB_ACTIONS:-false}" != true;; actions) test "${GITHUB_ACTIONS:-false}" = true; test "${GITHUB_SHA:-}" = "$PHASE6_EXPECTED_SHA"; test -n "${GITHUB_RUN_ID:-}";; *) exit 2;; esac
case "$evidence" in "$root"|"$root"/*) echo 'evidence must be outside source' >&2; exit 1;; /*) ;; *) exit 2;; esac
test "$(git rev-parse HEAD)" = "$PHASE6_EXPECTED_SHA"
test -z "$(git status --porcelain=v1)"
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
out=$evidence/$producer
test ! -e "$out" || { echo 'refusing to overwrite host evidence' >&2; exit 1; }
mkdir -p "$out"
export PHASE6_SOURCE_ROOT="$root" PHASE6_EVIDENCE_DIR="$out" PHASE6_THROUGH_PLAN=06-11
export COMPOSE_PROJECT_NAME="vortex-p6-$producer-$(printf '%s' "$PHASE6_EXPECTED_SHA" | cut -c 1-12)"
# Use isolated services and dynamically allocated host ports, not another test
# project's PG or Valkey. A port collision fails explicitly and can be retried
# only in a new evidence directory.
ports=$(python3 - <<'PY'
import socket
sockets=[socket.socket(),socket.socket()]
for s in sockets:s.bind(('127.0.0.1',0))
print(*(s.getsockname()[1] for s in sockets))
for s in sockets:s.close()
PY
)
PHASE6_PG_PORT=$(printf '%s' "$ports" | cut -d ' ' -f 1)
PHASE6_VALKEY_PORT=$(printf '%s' "$ports" | cut -d ' ' -f 2)
export PHASE6_PG_PORT PHASE6_VALKEY_PORT
compose=deploy/backend/compose.test.yml
printf '%s\n' "$PHASE6_EXPECTED_SHA" > "$out/commit.txt"
git rev-parse HEAD HEAD^{tree} > "$out/host-source-identity.txt"
git status --porcelain=v1 > "$out/host-source-status-before.txt"
export PYTHONDONTWRITEBYTECODE=1
result=0
(
 docker compose -f "$compose" up -d --wait postgres valkey
 docker compose -f "$compose" config > "$out/compose.yml"
 docker version > "$out/docker-version.txt"
 docker info --format '{{json .}}' > "$out/docker-info.json"
 docker compose -f "$compose" images --format json > "$out/images.json"
 docker compose -f "$compose" exec -T postgres psql -U vortex -c 'SELECT version();' > "$out/postgres-version.txt"
 docker compose -f "$compose" exec -T valkey valkey-cli INFO server > "$out/valkey-version.txt"
 # Linux bind mounts retain the container's root owner, including private
 # 0600 reports. Return artifacts to the invoking user even when a test fails;
 # retain file modes and the original test failure for validation/upload.
 docker compose -f "$compose" run --rm -e PHASE6_EVIDENCE_DIR=/evidence -e GOFLAGS=-p=1 -e GOMAXPROCS=2 backend-test sh -c '
  code=0
  sh tools/phase6/final_container_check.sh || code=$?
  chown -R "$1" /evidence || exit $?
  exit "$code"
 ' phase6-evidence "$(id -u):$(id -g)"
) > "$out/runner.log" 2>&1 || result=$?
# Every operation above must fail closed even inside an outer || compound.
# The container's final completion marker proves all ordered stages finished.
if test ! -f "$out/final-completion.txt"; then result=1; fi
cf=78
if test "$result" -eq 0; then
 (
  if test -n "${PHASE6_CLOUDFLARE_ENV_FILE:-}"; then
   test "$(stat -f '%Lp' "$PHASE6_CLOUDFLARE_ENV_FILE" 2>/dev/null || stat -c '%a' "$PHASE6_CLOUDFLARE_ENV_FILE")" = 600 || exit 1
   set -a
   . "$PHASE6_CLOUDFLARE_ENV_FILE" || exit 1
   set +a
  fi
  export PHASE6_DATABASE_URL="postgres://vortex:phase6-local-test-only@127.0.0.1:$PHASE6_PG_PORT/vortex?sslmode=disable"
  code=0
  sh tools/phase6/cloudflare_integration.sh || code=$?
  # This subshell is an operand of &&/||, which disables implicit errexit.
  # Propagate the guard failure explicitly instead of exiting with CRUD's code.
  python3 - "$out" <<'PY' || exit $?
import hashlib,os,pathlib,sys
folder=pathlib.Path(sys.argv[1]);policy=os.getenv('CLOUDFLARE_TOKEN_POLICY_FILE')
if policy:(folder/'cloudflare-policy-sha256.txt').write_text(hashlib.sha256(pathlib.Path(policy).read_bytes()).hexdigest()+'\n')
for key in ['CLOUDFLARE_API_TOKEN','CLOUDFLARE_POLICY_TOKEN']:
 secret=os.getenv(key,'').encode()
 if secret:
  assert not any(secret in p.read_bytes() for p in folder.rglob('*') if p.is_file()), 'secret bytes found in evidence'
print('credential byte leakage check passed')
PY
  exit "$code"
 ) > "$out/cloudflare.log" 2>&1 && cf=0 || cf=$?
 if test "$cf" -ne 0 && test "$cf" -ne 78; then result=$cf; fi
fi
git status --porcelain=v1 > "$out/host-source-status-after.txt"
if test -s "$out/host-source-status-after.txt" || test "$(git rev-parse HEAD)" != "$PHASE6_EXPECTED_SHA"; then result=1; fi
python3 tools/phase6/final_evidence_check.py stamp "$out" "$PHASE6_EXPECTED_SHA" "$producer" "$result" "$cf"
# Retain containers for evidence inspection; no unrelated project is stopped.
test "$result" -eq 0 || exit "$result"
python3 tools/phase6/final_evidence_check.py host "$out" "$PHASE6_EXPECTED_SHA"
