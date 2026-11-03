.PHONY: phase1-quick phase1-model phase1-check phase1-release-gate

phase1-quick:
	python3 -m unittest discover -s tests/phase1 -p 'test_*.py'

phase1-model:
	go test ./model ./tools/phase1 ./tests/phase1 ./cmd/modelcheck
	go run ./cmd/modelcheck -model model/lease-safety.go -results artifacts/modelcheck

phase1-check:
	tools/phase1/check.sh

phase1-release-gate: phase1-check
	@test "$$(python3 -c 'import json; print(json.load(open("artifacts/phase1/evidence.json"))["requirements"]["NFR-038"])')" = passed || { echo 'release gate blocked: NFR-038 is not passed'; exit 1; }
