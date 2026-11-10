"""Validate one clean Docker candidate run; never infer owner/Actions approval."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--folder', type=Path, required=True)
parser.add_argument('--sha', required=True)
args = parser.parse_args()
folder = args.folder


def require(ok, message):
    if not ok:
        raise SystemExit('limits evidence failed: ' + message)


def clean_log(name):
    text = (folder / name).read_text()
    require(not re.search(r'^FAIL\b|--- FAIL:|--- SKIP:|WARNING: DATA RACE', text, re.M), name + ' failed/skipped/racy')
    return text


def passed(text, names, minimum):
    for name in names:
        require(len(re.findall(r'--- PASS: ' + re.escape(name) + r' \(', text)) >= minimum, 'missing repeated pass: ' + name)


result = json.loads((folder / 'result.json').read_text())
require(result['commit'] == args.sha and result['result'] == 0 and result['source_clean'] is True, 'producer result/SHA/cleanliness')
require(result['actions'] == 'not_run', 'candidate must not fabricate Actions evidence')
for name in ['source-status-before.txt', 'source-status-after.txt']:
    require((folder / name).read_text() == '', name + ' is dirty')
identity = (folder / 'source-identity.txt').read_text().splitlines()
tree = subprocess.check_output(['git', 'rev-parse', args.sha + '^{tree}'], text=True).strip()
require(identity == [args.sha, tree], 'source tree identity')
trace = clean_log('tracer.log')
require('go version go1.27.3 linux/arm64' in trace or 'go version go1.27.3 linux/amd64' in trace, 'Linux pinned Go execution')
passed(trace, [
    'TestShareConcurrentLinearChainCapacityKindsAndAuditRollback',
    'TestShareRenewalClosureCompletePagesAuditRollbackAndCASOnlyRelease',
    'TestDistributionPropertiesConservationFloorAndPermutation',
    'TestAllocationWindowConnectionShortagePersistsRealQueue',
    'TestShareBootstrapCommitWaitsForBothKindsAtEveryPreparedNode',
    'TestRuntimeFundingSchedulerRequiresCASAndKeepsOneFiniteParentAcrossRetries',
    'TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts',
    'TestRuntimeRecoveryUpdateActualMTLSRenewalLostAfterFULLConsumesOnlyR',
    'TestRuntimeRecoveryPendingEngineCommitAutomaticallyRenewsAfterSR',
    'TestNodeBootRecoveryActualMTLSCapabilityLostFULLReceiptReplay',
    'TestHubRecoveryAdmissionActualMTLSExplicitProofFULLLossReconnectAndNewIssue',
    'TestEffectiveRuntimeFundingAfterInstalledOldCreditFinalKeepsOriginalShare',
], 2)
components = clean_log('rule-components.log')
passed(components, [
    'TestPureOlderPathRevocationKeepsFloorsFundsAndUnknownScope',
    'TestAuthenticatedOlderPathPureRevocationKeepsCurrentPathAndUnknownDebt',
    'TestHandoffFullDiskDoesNotPartiallyTransfer',
    'TestServiceFullDiskCannotStopOrSelectCandidate',
    'TestPrepareCommitHotSessionAndAbortRealSockets',
    'TestApplyMultihopForwardAndReverseRealTLS',
], 2)
fault = clean_log('share-fault.log')
passed(fault, [
    'TestShareHandoffFaultSIGKILLAfterDurableCASAndExpiredWorkerRecovery',
    'TestShareHandoffConnectionReductionWaitsForRealRuntimeDrain',
    'TestRuntimeRecoveryPendingEngineCommitAutomaticallyRenewsAfterSR',
    'TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts',
], 1)
ledger = clean_log('ledger-fault.log')
passed(ledger, ['TestUsageFaultPowerLossAuditTransactionRollback', 'TestUnknownRiskBudget72HourFreezeLateExactAndAuditRollback'], 1)
for name, count in [('rule-builds.log', 2), ('hub-builds.log', 1)]:
    text = (folder / name).read_text()
    for arch in ['amd64', 'arm64']:
        require(text.count('GOARCH=' + arch) >= count and text.count('GOOS=linux') >= count * 2, name + ' architecture missing')
files = {str(p.relative_to(folder)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(folder.rglob('*')) if p.is_file()}
print(json.dumps(dict(commit=args.sha, tree=tree, status='passed', scope='one clean-source Linux Docker candidate execution',
                      owner_approval=False, actions='not_run', paired_acceptance=False, checksums=files), indent=2))
