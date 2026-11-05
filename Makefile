.PHONY: phase1-quick phase1-model phase1-check phase1-release-gate phase2-engine-quick phase2-engine-check phase2-benchmark-smoke phase2-benchmark phase5-wave1-check

phase5-wave1-check:
	tools/phase5/wave1_check.sh
.PHONY: phase3-vlink-check phase3-vlink-fuzz phase3-vlink-benchmark phase4-m2-check phase4-runtime-smoke phase4-runtime-mixed phase4-m2-benchmark

phase4-m2-check:
	tools/phase4/check.sh

phase4-runtime-smoke:
	tools/phase4/topology-smoke.sh

phase4-runtime-comparison:
	tools/phase4/comparison.sh

phase4-runtime-mixed:
	tools/phase4/qualification.sh

.PHONY: phase4-runtime-modes
phase4-runtime-modes:
	sh tools/phase4/modes.sh

phase4-m2-benchmark:
	tools/phase4/benchmark.sh

phase3-vlink-check: phase3-vlink-fuzz
	cd engine && go test -race ./...
	tools/vlink/docker-test.sh
	tools/phase3/benchmark.sh

phase3-vlink-fuzz:
	cd engine && go test ./vlink -run '^$$' -fuzz '^FuzzReadFrame$$' -fuzztime=2s

phase2-engine-quick:
	cd engine && go test -race ./...

phase2-engine-check: phase2-engine-quick
	tools/engine/docker-test.sh

phase2-benchmark-smoke:
	cd engine && go test ./relay -run '^$$' -bench '^BenchmarkTCPRelayBareTCP$$' -benchtime=1s -count=1

phase2-benchmark:
	sh tools/phase2/benchmark.sh

phase1-quick:
	python3 -m unittest discover -s tests/phase1 -p 'test_*.py'

phase1-model:
	go test ./model ./tools/phase1 ./tests/phase1 ./cmd/modelcheck
	go run ./cmd/modelcheck -model model/lease-safety.go -results artifacts/modelcheck

phase1-check:
	tools/phase1/check.sh

phase1-release-gate: phase1-check
	@set -eu; evidence="$$(find artifacts/phase1/runs -type f -name evidence.json -print | sort | tail -n 1)"; \
	test -n "$$evidence" || { echo 'phase1 evidence manifest is missing'; exit 1; }; \
	test "$$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["requirements"]["NFR-038"])' "$$evidence")" = out-of-scope || { echo 'project restart disposition missing: NFR-038 must remain out-of-scope'; exit 1; }

.PHONY: phase4-runtime-recovery phase4-runtime-snapshots
phase4-runtime-recovery:
	python3 tools/phase4/recovery.py --backend $${BACKEND:-native}

phase4-runtime-snapshots:
	python3 tools/phase4/snapshots.py

.PHONY: phase4-d04-scan phase4-qualification-gate
phase4-d04-scan:
	python3 tools/phase4/d04_scan.py --output $${D04_OUTPUT:-artifacts/phase4/d04-matrix.json}

phase4-qualification-gate:
	python3 tools/phase4/qualification_gate.py \
		--runtime "$${RUNTIME_REPORT:?RUNTIME_REPORT is required}" \
		--tls "$${TLS_REPORT:?TLS_REPORT is required}" \
		--host "$${HOST_REPORT:?HOST_REPORT is required}" \
		--trials "$${QUALIFICATION_TRIALS:-0}" \
		--warmup-seconds "$${QUALIFICATION_WARMUP_SECONDS:-0}" \
		--output "$${GATE_OUTPUT:-artifacts/phase4/qualification-gate.json}"

.PHONY: phase4-native-preflight
phase4-native-preflight:
	tools/phase4/native-host-preflight.sh --install \
		--interface "$${QUALIFICATION_INTERFACE:?QUALIFICATION_INTERFACE is required}" \
		--attestation-file "$${HOST_ATTESTATION:?HOST_ATTESTATION is required}" \
		--output "$${PREFLIGHT_OUTPUT:-artifacts/phase4/native-host-preflight.json}"

.PHONY: phase4-runtime-credit
phase4-runtime-credit:
	python3 tools/phase4/credit.py --backend $${BACKEND:-native}
	python3 tools/phase4/credit.py --backend $${BACKEND:-native} --protocol udp
