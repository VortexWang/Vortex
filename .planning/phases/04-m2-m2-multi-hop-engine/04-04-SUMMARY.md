---
phase: 04-m2-m2-multi-hop-engine
plan: 04
status: complete
---

# Plan 04-04 Summary

Connected the qualification accounting boundary to the real TCP bridge:

- Source/destination and relay target writes reserve before writing and commit
  only successful byte counts.
- Short writes and transport failures mark the reservation unknown.
- Per-process JSONL evidence is written on shutdown through the configured
  `evidence_file`.
- `tools/phase4/qualification.sh` hard-fails when topology logs or accounting
  evidence are missing; it cannot downgrade that absence to environment-limited
  success.

Verification:

```text
cd engine && go test -race ./...
BACKEND=native QUALIFICATION_DIR=<temp> VORTEX_RELAY_BIN=<binary> tools/phase4/qualification.sh
```

Both passed. The native run produced `qualification=topology-and-accounting-pass`
and a non-empty destination accounting JSONL file. Sustained mixed TCP/UDP
load and performance ratios remain Plan 04-05.
