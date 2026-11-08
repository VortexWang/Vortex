#!/bin/sh
# One entrypoint for exact committed source, host production and paired evidence.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
through=06-02
expected=${GITHUB_SHA:-}
produce=
evidence=${PHASE6_EVIDENCE_DIR:-}
while test "$#" -gt 0; do
  case "$1" in
    --through-plan) through=$2; shift 2;;
    --expected-sha) expected=$2; shift 2;;
    --produce) produce=$2; shift 2;;
    --evidence-dir) evidence=$2; shift 2;;
    *) echo "unknown option: $1" >&2; exit 2;;
  esac
done
test -n "$expected" || { echo '--expected-sha or GITHUB_SHA required' >&2; exit 1; }
test "$(git rev-parse HEAD)" = "$expected" || { echo 'expected SHA differs from HEAD' >&2; exit 1; }
test -z "$(git status --porcelain=v1)" || { echo 'source must be clean, including untracked files' >&2; exit 1; }
export PHASE6_EXPECTED_SHA="$expected"
export GOTOOLCHAIN=local
test "$(go env GOVERSION)" = go1.27.3
# Never silently claim future implementations from the current test suite.
case "$through" in 06-01|06-08|06-09|06-02|06-10|06-03|06-04|06-05) ;; *) echo 'producer not implemented for requested plan' >&2; exit 1;; esac
test -n "$evidence" || evidence="/tmp/vortex-phase6-evidence/$expected"
evidence=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$evidence")
case "$evidence" in "$root"|"$root"/*) echo 'evidence must be outside source checkout' >&2; exit 1;; esac
mkdir -p "$evidence"
if test -z "$produce"; then
  exec sh tools/phase6/evidence_manifest.sh --through-plan "$through" --expected-sha "$expected" --evidence-dir "$evidence"
fi
case "$produce" in
  docker) test "${GITHUB_ACTIONS:-false}" != true;;
  actions) test "${GITHUB_ACTIONS:-false}" = true; test -n "${GITHUB_RUN_ID:-}"; test "${GITHUB_SHA:-}" = "$expected";;
  *) echo 'producer must be docker or actions' >&2; exit 1;;
esac
out="$evidence/$produce"
test ! -e "$out/result.json" || { echo 'refusing to overwrite completed evidence' >&2; exit 1; }
mkdir -p "$out"
printf '%s\n' "$expected" > "$out/commit.txt"
git status --porcelain=v1 > "$out/source-status-before.txt"
git rev-parse HEAD HEAD^{tree} > "$out/source-identity.txt"
work=$(mktemp -d /tmp/vortex-phase6-openapi.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
result=0
(
  ruby tools/phase6/openapi_validate.rb contracts/openapi/v1/openapi.yaml &&
  cp contracts/openapi/v1/openapi.yaml "$work/one.yaml" &&
  cp contracts/openapi/v1/openapi.yaml "$work/two.yaml" &&
  cmp -s "$work/one.yaml" "$work/two.yaml" &&
  cmp -s "$work/one.yaml" backend/openapi/v1/openapi.yaml
) > "$out/openapi.log" 2>&1 || result=$?
if test "$result" -eq 0; then
  sh tools/phase6/dependency_lock_check.sh > "$out/dependencies.log" 2>&1 || result=$?
fi
if { test "$through" = 06-03 || test "$through" = 06-04 || test "$through" = 06-05; } && test "$result" -eq 0; then
  candidate=0
  sh tools/phase6/rule_contract_check.sh > "$out/rule-contract.log" 2>&1 || candidate=$?
  test "$candidate" -eq 0 || result=$candidate
  # Candidate/runtime validation cannot grant owner freeze approval.
  freeze=0
  for contract in database public-api control.v1 runtime-config; do
    python3 tools/phase1/contracts.py check-frozen --contract-id "vortex.$contract" > "$out/rule-freeze-$contract.log" 2>&1 || freeze=1
  done
  python3 - "$out" "$expected" "$freeze" "$candidate" <<'PY'
import json,pathlib,sys
out,sha,freeze,candidate=sys.argv[1:]
pathlib.Path(out,'rule-contract.json').write_text(json.dumps(dict(commit=sha,candidate_validation='passed' if candidate=='0' else 'failed',freeze_status='approved' if freeze=='0' else 'pending_owner_approval'),indent=2)+'\n')
PY
fi
if test "$result" -eq 0; then
  PHASE6_THROUGH_PLAN="$through" PHASE6_SOURCE_ROOT="$root" PHASE6_EVIDENCE_DIR="$out" sh tools/phase6/tracer_real.sh > "$out/runner.log" 2>&1 || result=$?
fi
if { test "$through" = 06-10 || test "$through" = 06-03 || test "$through" = 06-04 || test "$through" = 06-05; } && test "$result" -eq 0; then
  cloudflare_result=0
  PHASE6_DATABASE_URL="postgres://vortex:phase6-local-test-only@127.0.0.1:${PHASE6_PG_PORT:-15436}/vortex?sslmode=disable" sh tools/phase6/cloudflare_integration.sh > "$out/cloudflare.log" 2>&1 || cloudflare_result=$?
  python3 - "$out" "$expected" "$cloudflare_result" <<'PY'
import hashlib,json,os,pathlib,sys
out,sha,code=sys.argv[1:]
policy=os.getenv('CLOUDFLARE_TOKEN_POLICY_FILE')
policy_hash=hashlib.sha256(pathlib.Path(policy).read_bytes()).hexdigest() if policy and pathlib.Path(policy).is_file() else None
pathlib.Path(out,'cloudflare.json').write_text(json.dumps(dict(commit=sha,exit_code=int(code),status='passed' if code=='0' else 'not_run' if code=='78' else 'failed',policy_sha256=policy_hash,scope_evidence='reviewed_creation_policy' if policy else 'live_policy_inspector'),indent=2)+'\n')
PY
  # Missing external setup is visible pending evidence. A real executed failure
  # fails the producer; a paired acceptance gate also rejects missing setup.
  if test "$cloudflare_result" -ne 0 && test "$cloudflare_result" -ne 78; then result=$cloudflare_result; fi
fi
git status --porcelain=v1 > "$out/source-status-after.txt"
if test -s "$out/source-status-after.txt"; then result=1; fi
if test "$(git rev-parse HEAD)" != "$expected"; then result=1; fi
python3 - "$out" "$expected" "$produce" "$through" "$result" <<'PY'
import hashlib,json,os,pathlib,platform,sys
out,sha,host,through,result=sys.argv[1:]
p=pathlib.Path(out)
files=['commit.txt','openapi.log','dependencies.log','tracer.log','runner.log','source-status-before.txt','source-status-after.txt','source-identity.txt','compose.yml','docker-version.txt','docker-info.json','images.json','postgres-version.txt','valkey-version.txt','build-metadata.txt']
processes=[]
if (p/'processes').is_dir():
    for report in sorted((p/'processes').iterdir()):
        if report.name.endswith('-result.json'):
            stamp=report.name.removesuffix('-result.json')
            processes.append({role:'processes/'+stamp+'-'+role+suffix for role,suffix in [('result','.json'),('backend','.log'),('agent','.log'),('hub','.log')]})
(p/'processes.json').write_text(json.dumps(processes,indent=2)+'\n')
files+=['processes.json']+[name for run in processes for name in run.values()]
external={}
if through in ('06-10','06-03','06-04','06-05'):
    files+=['cloudflare.log','cloudflare.json']
    external['cloudflare']=json.loads((p/'cloudflare.json').read_text()) if (p/'cloudflare.json').is_file() else {'status':'not_run'}
if through in ('06-03','06-04','06-05'):
    files+=['rule-contract.log','rule-contract.json','rule-components.log','rule-builds.log']+['rule-freeze-'+c+'.log' for c in ('database','public-api','control.v1','runtime-config')]
    external['rule_contract']=json.loads((p/'rule-contract.json').read_text()) if (p/'rule-contract.json').is_file() else {'freeze_status':'not_checked'}
if through=='06-05': files+=['ledger-fault.log']
checksums={name:hashlib.sha256((p/name).read_bytes()).hexdigest() for name in files if (p/name).is_file()}
accepted=(not external or external['cloudflare']['status']=='passed') and (through not in ('06-03','06-04','06-05') or external['rule_contract'].get('freeze_status')=='approved')
(p/'result.json').write_text(json.dumps(dict(commit=sha,result=int(result),producer=host,through_plan=through,external_checks=external,acceptance_complete=accepted,source_clean=not (p/'source-status-after.txt').read_text(),checksums=checksums,host=platform.platform(),run_id=os.getenv('GITHUB_RUN_ID'),run_url=('https://github.com/'+os.getenv('GITHUB_REPOSITORY','')+'/actions/runs/'+os.getenv('GITHUB_RUN_ID','')) if host=='actions' else None),indent=2)+'\n')
PY
test "$result" -eq 0 || { echo "producer failed; see $out" >&2; exit "$result"; }
sh tools/phase6/evidence_manifest.sh --through-plan "$through" --expected-sha "$expected" --evidence-dir "$evidence" --host "$produce"
if { test "$through" = 06-03 || test "$through" = 06-04 || test "$through" = 06-05; } && test "$freeze" -ne 0; then
  echo "candidate runtime evidence recorded; contract freeze remains pending owner approval: $out" >&2
  exit 1
fi
