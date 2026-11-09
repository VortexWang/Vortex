# P4-Q1 formal gate boundary (2026-11-06)

The machine-generated gate was run against the current clean Docker diagnostic
reports:

```text
python3 tools/phase4/qualification_gate.py \
  --runtime /tmp/vortex-phase4-runtime-fdaea8f/report.json \
  --tls /tmp/vortex-phase4-comparison-fdaea8f/comparison.json \
  --host /tmp/vortex-phase4-comparison-fdaea8f/host.json \
  --trials 0 \
  --trial-reports /tmp/vortex-phase4-runtime-fdaea8f/report.json \
  --warmup-report /tmp/vortex-phase4-runtime-fdaea8f/report.json \
  --warmup-seconds 5 \
  --output /tmp/vortex-phase4-qualification-gate-fdaea8f.json
```

The result is `not-qualified`. Reasons are preserved in the output JSON:

- runtime and independent TLS reports are not formal `pass` reports;
- the warmup is 5 seconds instead of 60 seconds;
- the same-load TLS ratio is below the formal threshold;
- the report is Docker/OrbStack rather than a native Linux host;
- host memory, non-loopback qualification interface and Linux resource
  commands are absent; and
- zero formal trials were submitted, below the required three.

The direct host preflight also fails closed at
`/tmp/vortex-phase4-native-preflight-20261106.json`: the current host is
Darwin/arm64, not Ubuntu 24.04 Linux, and has no qualifying Linux interface or
resource command evidence. These are environment facts, not implementation
failures. P4-Q1 remains open until an eligible native Ubuntu host with the
required resource/link attestation produces one clean SHA, 60-second warmup,
three 300-second runtime trials and an independent TLS comparison.

