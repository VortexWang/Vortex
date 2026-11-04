---
quick_id: 261105-9j7
status: complete
completed: 2026-11-05
---

# Native Host Preflight Summary

Added `tools/phase4/native-host-preflight.sh`, a fail-closed host evidence
check, and `tools/phase4/NATIVE-HOST-QUALIFICATION.md` with Ubuntu installation
and formal qualification steps. The Makefile exposes
`phase4-native-preflight`, requiring an explicit network interface and provider
attestation file. A macOS negative run produced valid JSON and correctly
returned nonzero.
