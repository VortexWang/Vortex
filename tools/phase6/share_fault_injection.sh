#!/bin/sh
set -eu
share_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$share_root/backend"
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
: "${PHASE6_DATABASE_URL:?real PostgreSQL required}"
GOTOOLCHAIN=local go test -race ./internal/service ./internal/worker -count=1 -v -run 'ShareRenewalClosure|ShareHandoff(FaultSIGKILL|LostReceipt|CrashBeforeSend|ConnectionReduction|RealMTLS|RateCap|NodeReceipt)|RecoveredShareClosureActualMTLS|Hub(FundedActualMTLS|EffectiveEpochActualMTLS|RecoveryAdmissionActualMTLS|RecoveryPreflightActualMTLS)|NodeRecoverySourcesActualMTLS|Runtime(LeaseActualMTLS|Recovery)'
