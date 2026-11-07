#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
through=06-02
expected=${PHASE6_EXPECTED_SHA:-${GITHUB_SHA:-}}
evidence=${PHASE6_EVIDENCE_DIR:-}
host=
while test "$#" -gt 0; do
  case "$1" in
    --through-plan) through=$2; shift 2;;
    --expected-sha) expected=$2; shift 2;;
    --evidence-dir) evidence=$2; shift 2;;
    --host) host=$2; shift 2;;
    *) echo "unknown manifest option: $1" >&2; exit 2;;
  esac
done
test -n "$expected" && test "$(git rev-parse HEAD)" = "$expected"
test -n "$evidence" || evidence="/tmp/vortex-phase6-evidence/$expected"
python3 - "$through" "$expected" "$evidence" "$host" <<'PY'
import hashlib,json,pathlib,re,sys
through,sha,evidence,host=sys.argv[1:]
def require(ok,message):
    if not ok: raise SystemExit('manifest failed: '+message)
order=['06-01','06-08','06-09','06-02','06-10','06-03','06-04','06-05','06-06','06-07','06-11']
require(through in order[:6], 'producer not implemented for '+through)
require(host in ('','docker','actions'), 'invalid host')
rows=re.findall(r'^\| ([A-Z][A-Z0-9-]+) \| (06-\d\d) \|',pathlib.Path('docs/coverage/phase6/COVERAGE.md').read_text(),re.M)
require(len(rows)==96 and len(dict(rows))==96, 'full 96-item ownership register required')
planned={}
for plan in order:
    text=pathlib.Path('.planning/phases/06-m4-m4-backend/'+plan+'-PLAN.md').read_text()
    for req in re.search(r'^requirements: \[(.*?)\]',text,re.M).group(1).split(', '):
        require(req not in planned, 'duplicate plan owner '+req)
        planned[req]=plan
require(dict(rows)==planned, 'ownership differs from plan frontmatter')
required=['commit.txt','openapi.log','dependencies.log','tracer.log','runner.log','source-status-before.txt','source-status-after.txt','source-identity.txt','compose.yml','docker-version.txt','docker-info.json','images.json','postgres-version.txt','valkey-version.txt','build-metadata.txt','processes.json']
tests={'06-01':['TestTracerDesiredStateReceipt','TestTracerRealProcesses'], '06-08':['TestTenantRestrictionsIndependentAndImmutable','TestTenantAuditRollbackSharedAuthority','TestMigrationUpgradeTenantAuthority'], '06-09':['TestRestrictionAllCauses','TestRestrictionAuditRollback'], '06-02':['TestNodeGroupOwnershipAndStableMembers','TestNodeGrantValidityHistoryAndAudit','TestNodeLifecycleDrainDeleteAndAuditRollback','TestNodeSegmentPermitIndependentOfGrants','TestPortAtomicFixedMembersAndServing','TestPortPlatformCrossTenantPhysicalConflict','TestPortNoCommonPortAndMemberChanges','TestPortConcurrentWildcardConflict','TestPortProofAuthenticationAndGenerationFence','TestPortRestrictionClockAndProofBackedRelease','TestPortRetentionClearReapplyAndManualHold','TestPortAuditRollbackAndForeignAllocation','TestPortRestrictionAuditRollbackSameTransaction','TestReleaseOldBootAndDisconnectRetainOccupation']}
tests['06-10']=['TestServingRejectsDeletedMember','TestServingRejectsUnhealthyMember','TestServingCurrentSnapshotAndUnknown','TestServingAllCommittedRulesAndFailedNewRule','TestServingPlatformAggregatesAllCommittedTenantRules','TestDNSEntryGroupDomainBinding','TestDNSDurableServingRecomputeAndUnknownReplay','TestDNSAuditRollbackAndCurrentRole','TestCredentialCryptoScopeAndRecovery','TestCredentialEncryptedScopedAuditAndRollback','TestDNSCredentialPolicyIdentityAndExactZone','TestDNSBoundedRetryRedactionAndNoBlindCreate','TestDNSReconcileIdempotentOwnershipAndCleanup']
tests['06-03']=['TestRuleRevisionSchema','TestRuleImmutableRevisionConflictAndTenantScope','TestRuleValidationPathsTargetsAndDirections','TestRuleDependencyRecompileKeepsActiveRevision','TestRuleAuditRollback','TestRuleCompileCommittedArtifactAndAuditRollback','TestCompileArtifactImmutableSchema','TestCompileCanonicalDigestReplay','TestCompileInvalidAuthorityNoArtifacts','TestCompileAutoPortBindingCanonicalReplay','TestCompileGrantPermitAndDirection','TestCompileRuntimeSchemaCompatibility','TestCompileRuntimeBounds','TestApplyPrepareCommitUnknownAndLateReceipt','TestRollbackAuditAtomicSupersedeAndWorkerCrashRecovery','TestCompileWorkerMissingSnapshotThenReadyRestart','TestCompileWorkerAuditRollback','TestCompileWorkerMissingSnapshotPreservesPriorAuthority','TestCompileWorkerConflictRetainsAuthority','TestCompileWorkerConflictPreservesPriorActiveArtifact','TestRuleFixedPortRejectsUnallocatedCompilePaths','TestRuleAutoPortConsumesDurableAllocation','TestRulePortLegacyInflightCannotBeOverwritten','TestRuleAutoPortRejectsMissingOrChangedMemberAllocation','TestRulePortActualAgentSocketsUnknownAndQuery','TestRuleCapabilityLegacyAndReceiptIsolation']
for producer in ([host] if host else ['docker','actions']):
    folder=pathlib.Path(evidence)/producer
    require((folder/'result.json').is_file(), producer+' result.json missing')
    result=json.loads((folder/'result.json').read_text())
    require(result.get('commit')==sha and result.get('result')==0 and result.get('source_clean') is True, producer+' result/SHA/clean assertion')
    require(result.get('producer')==producer, producer+' host identity')
    require(result.get('through_plan') in order and order.index(result['through_plan'])>=order.index(through), producer+' insufficient scope')
    if producer=='actions':
        require(str(result.get('run_id','')).isdigit() and result.get('run_url','').startswith('https://github.com/'), 'Actions provenance missing')
    require((folder/'processes.json').is_file(), 'real process index missing')
    processes=json.loads((folder/'processes.json').read_text())
    require(len(processes)==2, 'normal and race real process reports required')
    process_files=[]
    for run in processes:
        require(set(run)=={'result','backend','agent','hub'}, 'incomplete process report')
        for name in run.values():
            require(re.fullmatch(r'processes/[a-f0-9-]{36}-(result\.json|backend\.log|agent\.log|hub\.log)',name) is not None, 'invalid process filename')
        process_files.extend(run.values())
        report=json.loads((folder/run['result']).read_text())
        require(report.get('commit')==sha and report.get('state')=='succeeded' and report.get('occupation_held') is True and report.get('disconnect_state')=='unknown' and report.get('hub_mode')=='blocked' and report.get('hub_signer_epoch')==0 and report.get('wrong_role_rejected') is True, 'real process assertions failed')
    require(len(set(process_files))==len(process_files), 'duplicate process evidence')
    external_files=[]
    if order.index(through)>=order.index('06-10'):
        external_files=['cloudflare.log','cloudflare.json']
        cf=json.loads((folder/'cloudflare.json').read_text())
        require(cf.get('commit')==sha and cf==result.get('external_checks',{}).get('cloudflare'), 'Cloudflare metadata/SHA mismatch')
        require(cf.get('status') in ('passed','not_run'), 'Cloudflare execution failed')
        if cf.get('status')=='passed':
            require(cf.get('exit_code')==0 and '--- PASS: TestCloudflareControlledCRUD' in (folder/'cloudflare.log').read_text(), 'real CRUD pass evidence missing')
        else:
            require(host and cf.get('exit_code')==78 and result.get('acceptance_complete') is False, 'paired acceptance requires real Cloudflare CRUD')
    if order.index(through)>=order.index('06-03'):
        external_files+=['rule-contract.log','rule-contract.json','rule-components.log','rule-builds.log']+['rule-freeze-'+c+'.log' for c in ('database','public-api','control.v1','runtime-config')]
        rule=json.loads((folder/'rule-contract.json').read_text())
        require(rule.get('commit')==sha and rule==result.get('external_checks',{}).get('rule_contract') and rule.get('candidate_validation')=='passed', 'rule contract candidate metadata/SHA')
        require(rule.get('freeze_status') in ('approved','pending_owner_approval'), 'rule contract freeze not checked')
        if rule['freeze_status']!='approved':
            require(host and result.get('acceptance_complete') is False, 'paired acceptance requires owner contract freeze approval')
        require('two clean generations' in (folder/'rule-contract.log').read_text(), 'generated/wire contract evidence missing')
        component_log=(folder/'rule-components.log').read_text()
        require('--- SKIP:' not in component_log and '\nFAIL' not in component_log and 'WARNING: DATA RACE' not in component_log, 'failed/skipped/racy rule component tests')
        for target in ['TestLegacyRuntimeSchemaBranchPreservesControlValidation','TestLegacyRuntimeSchemaRejectsUnsupportedBranches','TestRuleCommandPrepareCommitQueryRestartRealRuntime','TestRuleConfigScopeIndependentAndRestartHolds','TestPrepareCommitHotSessionAndAbortRealSockets','TestReleaseProvesClosedSocketsAndReapplyBindsFreshEndpoint','TestApplyMultihopForwardAndReverseRealTLS']:
            require(len(re.findall(r'--- PASS: '+target+r' \([^\n]*',component_log))>=2, 'rule component normal/race evidence missing '+target)
        builds=(folder/'rule-builds.log').read_text()
        for arch in ('amd64','arm64'):
            require(builds.count('GOARCH='+arch)>=2 and builds.count('GOOS=linux')>=4, 'Linux target compilation missing '+arch)
    for name in required+process_files+external_files:
        f=folder/name
        require(f.is_file(), producer+'/'+name+' missing')
        require(result.get('checksums',{}).get(name)==hashlib.sha256(f.read_bytes()).hexdigest(), producer+'/'+name+' checksum mismatch')
    require((folder/'commit.txt').read_text()==sha+'\n', 'SHA file mismatch')
    require(not (folder/'source-status-before.txt').read_text() and not (folder/'source-status-after.txt').read_text(), 'dirty source')
    log=(folder/'tracer.log').read_text()
    require('go version go1.27.3 linux/' in log, 'pinned Linux toolchain missing')
    require('--- SKIP:' not in log and '\nFAIL' not in log and 'WARNING: DATA RACE' not in log, 'failed/skipped/racy tests')
    for plan in order[:order.index(through)+1]:
        for target in tests[plan]:
            require(len(re.findall(r'--- PASS: '+target+r' \([^\n]*',log))>=2, producer+' normal/race test evidence missing '+target)
    require('OpenAPI lint passed:' in (folder/'openapi.log').read_text(), 'lint not passed')
    require('two clean generations and committed source match byte-for-byte' in (folder/'dependencies.log').read_text(), 'sqlc deterministic evidence missing')
print('manifest passed: 96 owners; '+('paired Docker/Actions' if not host else host+' producer only; pairing pending'))
PY
if test -z "$host"; then
  cmp --silent "$evidence/docker/commit.txt" "$evidence/actions/commit.txt"
fi
