#!/usr/bin/env python3
"""Check bounded clean-source 06-07 raw producer output; not same-SHA Actions pairing."""
import hashlib
import json
from pathlib import Path
import re
import sys

NO_TEST_PACKAGES = {'vortex/backend/'+p for p in ['cmd/vortex-control-peer','cmd/vortex-hub','db','db/generated','internal/testutil','internal/worker']}

REQUIRED = {
 'vortex/backend/internal/httpapi': ['TestOpenAPIExactlyMatchesRegisteredGinRoutes','TestHTTPBusinessReadsOwnershipGrantExpiryAndSafeFields','TestHTTPResourceListCursorScopeOrderingAndMutableBusinessState','TestPublicResourceOrderMigrationBackfillsAndPreservesHistoricalRows','TestSSECommittedEventsReplayScopeAndRESTConvergence','TestSSERetentionGapResyncAndReauthentication','TestSSEEventIDStableAcrossReplaySecond','TestSSEPlatformScopeFutureCursorAndAuditFailure','TestDomainEventSameTransactionAuditRollbackAndDeferredVisibility','TestDomainEventLateCommitAndCrossTenantFlushOrder','TestDomainEventRetentionDeleteFailureRollsBackFloor'],
 'vortex/backend/internal/service': ['TestRecoveryHTTPDurableAdvanceWaitsForFullProofAndRetainsDebt','TestRecoveryHTTPResultAuditFailureRetainsLeaseAndResumesExactly','TestExplicitRecoveryOrchestrationWaitsForActualNodeProofAndRetainsOccupation','TestGroupRevalidationAllTenantsAtomicReplayAndOriginalAuthority','TestPortOperationMigrationPreservesHistoricalUnknownAndDoesNotInventAggregate','TestPortIntentHTTPCommitUsesActualAuthenticatedStoredProofs','TestDNSJobActorBoundConcurrentConsumerUnknownParkAndReplay','TestDNSJobRevokedActorLegacyIsolationAndAuditLeaseRollback'],
 'vortex/backend/internal/auth':['TestAuthTokenRotationReuseRevokesWholeFamilyAndStoresOnlyHashes','TestAuthConcurrentRefreshOneRotationAndReuseRevokesWinner','TestAuthTokenAuditFailureRollsBackRotationAndRevocation'],
}

def check(root,sha):
 root=Path(root)
 assert re.fullmatch('[0-9a-f]{40}',sha), 'invalid SHA'
 identity=(root/'source-identity.txt').read_text().splitlines();assert len(identity)==2 and identity[0]==sha
 for name in ['source-status-before.txt','source-status-after.txt','sqlc-diff.log']:assert not (root/name).read_text().strip(), 'dirty source or generation drift'
 assert int((root/'sqlc-output-count.txt').read_text())==33
 assert (root/'completion.txt').read_text().startswith('backend normal/race, two sqlc generations, six cross-builds')
 results={}
 for kind in ['normal','race']:
  events=[json.loads(line) for line in (root/f'backend-{kind}.jsonl').read_text().splitlines() if line.strip()]
  assert events and not any(e.get('Action')=='fail' or (e.get('Action')=='skip' and ('Test' in e or e.get('Package') not in NO_TEST_PACKAGES)) for e in events), 'failed/skipped backend test'
  # Go emits a package-level skip for packages containing no test files.
  # Only this explicit inventory is permitted; a skipped test always fails.
  passed={(e.get('Package'),e.get('Test')) for e in events if e.get('Action')=='pass'}
  for package,names in REQUIRED.items():
   assert (package,None) in passed, 'package did not finish'
   for name in names:assert (package,name) in passed, 'critical test did not pass: '+name
  for package in ['auth','control','repository','policy','timeauthority']:
   assert ('vortex/backend/internal/'+package,None) in passed, 'missing package '+package
  results[kind]={'packages_without_test_files':sorted(e['Package'] for e in events if e.get('Action')=='skip' and 'Test' not in e),'passed_test_events':sum(1 for _,test in passed if test),'service_seconds':next(e['Elapsed'] for e in events if e.get('Action')=='pass' and e.get('Package')=='vortex/backend/internal/service' and 'Test' not in e)}
 for name in ['native-build-metadata.txt','race-build-metadata.txt','cross-build-metadata.txt']:
  raw=(root/name).read_text();count=6 if name.startswith('cross') else 2
  assert raw.count('vcs.revision='+sha)==count and raw.count('vcs.modified=false')==count, 'build source identity mismatch'
  if name.startswith('cross'):
   assert raw.count('GOOS=linux')==6 and raw.count('GOARCH=amd64')==3 and raw.count('GOARCH=arm64')==3
 return {'source_sha':sha,'result':'passed','scope':'bounded clean-source backend REST/SSE acceptance; Actions pair pending06-11','tests':results,'evidence_sha256':{str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in root.rglob('*') if p.is_file()}}

if __name__=='__main__': print(json.dumps(check(sys.argv[1],sys.argv[2]),indent=2))
