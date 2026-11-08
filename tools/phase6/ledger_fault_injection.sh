#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root/backend"
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
: "${PHASE6_DATABASE_URL:?real PostgreSQL required}"
: "${PHASE6_VALKEY_ADDR:?real Valkey required}"
GOTOOLCHAIN=local go test -race ./internal/service ./internal/worker -count=1 -v -run 'UsageFaultPowerLoss|UsageActualAgentHubPostgres|UsageIdempotent|UnknownRiskBudget72Hour|UnknownExactFinal|UsageReconciliationWorker'
