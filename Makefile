.PHONY: phase1-quick phase1-model phase1-check phase1-release-gate phase2-engine-quick phase2-engine-check phase2-benchmark-smoke phase2-benchmark
.PHONY: phase3-vlink-check phase3-vlink-fuzz phase3-vlink-benchmark phase4-m2-check phase4-m2-benchmark

phase4-m2-check:
	tools/phase4/check.sh

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
