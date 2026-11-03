#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
COMMIT=$(git -C "$ROOT" rev-parse HEAD)
DIRTY=false
[ -n "$(git -C "$ROOT" status --porcelain)" ] && DIRTY=true
START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
RUN_ID=$(date -u +%Y%m%dT%H%M%SZ)-$$
RESULTS="$ROOT/artifacts/phase1/runs/$RUN_ID"
mkdir -p "$RESULTS"
HOST=$(uname -s)
HOST_ARCH=$(uname -m)
DOCKER_OS=$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || printf 'unavailable')
DOCKER_ARCH_RAW=$(docker info --format '{{.Architecture}}' 2>/dev/null || printf 'unavailable')
case "$DOCKER_ARCH_RAW" in
  aarch64) DOCKER_ARCH=arm64 ;;
  x86_64) DOCKER_ARCH=amd64 ;;
  *) DOCKER_ARCH=$DOCKER_ARCH_RAW ;;
esac
IMAGE_DIGEST=""
FAIL=0

run_required() {
  name=$1; shift
  set +e
  "$@" >"$RESULTS/$name.log" 2>&1
  code=$?
  set -e
  printf '%s=%s\n' "$name" "$code" >> "$RESULTS/status.env"
  [ "$code" -eq 0 ] || FAIL=1
}

run_expected_blocked() {
  name=$1; shift
  set +e
  "$@" >"$RESULTS/$name.log" 2>&1
  code=$?
  set -e
  printf '%s=%s\n' "$name" "$code" >> "$RESULTS/status.env"
}

cd "$ROOT"
run_required python-tests python3 -m unittest discover -s tests/phase1 -p 'test_*.py'
run_required lock-check python3 tools/phase1/baseline.py check-lock
run_required contract-check python3 tools/phase1/contracts.py check-frozen
run_required requirement-map python3 tools/phase1/contracts.py check-map
run_required go-tests go test ./model ./tools/phase1 ./tests/phase1 ./cmd/modelcheck
run_required model-report go run ./cmd/modelcheck -model model/lease-safety.go -results "$RESULTS/modelcheck"
run_required evidence-disposition python3 tools/phase1/baseline.py evidence

if [ "$DOCKER_OS" = "unavailable" ]; then
  echo 'Docker is unavailable' >"$RESULTS/docker.log"
  FAIL=1
else
  GOOS=linux GOARCH="$DOCKER_ARCH" CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o test/netns/fixture/bin/fixture ./test/netns/fixture
  run_required docker-build docker compose -f compose.phase1.yaml build --no-cache
  IMAGE_DIGEST=$(docker image inspect vortex-phase1:local --format '{{index .RepoDigests 0}}' 2>/dev/null || docker image inspect vortex-phase1:local --format '{{.Id}}')
  run_required network-smoke tools/phase1/netns-smoke.sh
fi

END=$(date -u +%Y-%m-%dT%H:%M:%SZ)
python3 - "$RESULTS/evidence.json" "$COMMIT" "$DIRTY" "$START" "$END" "$HOST" "$HOST_ARCH" "$DOCKER_OS" "$DOCKER_ARCH" "$IMAGE_DIGEST" "$FAIL" <<'PY'
import json, pathlib, sys
out, commit, dirty, start, end, host, host_arch, docker_os, docker_arch, digest, fail = sys.argv[1:]
statuses={}
p=pathlib.Path(out).parent/'status.env'
if p.exists():
    for line in p.read_text().splitlines():
        k,v=line.split('=',1); statuses[k]=int(v)
base=pathlib.Path(out).parent
artifacts=[str(x.relative_to(base.parent.parent)) for x in base.rglob('*') if x.is_file() and x.name != 'evidence.json']
data={
  'schema_version':1, 'commit_sha':commit, 'dirty':dirty=='true',
  'command':'make phase1-check', 'started_at':start, 'ended_at':end,
  'environment':{'host_os':host,'host_arch':host_arch,'docker_os':docker_os,'docker_arch':docker_arch},
  'image_digest':digest or None, 'checks':statuses, 'requirements':{'NFR-038':'out-of-scope'},
  'artifacts':sorted(artifacts), 'status':'passed' if fail=='0' else 'failed',
  'note':'Network fixture is smoke coverage only; Engine throughput, UDP correctness and leak acceptance remain Phase 2.'
}
pathlib.Path(out).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
PY
[ "$FAIL" -eq 0 ]
