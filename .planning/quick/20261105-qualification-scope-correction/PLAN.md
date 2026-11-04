---
quick_id: 261105-qsc
status: complete
---

# Correct Qualification Scope

Align Phase 4 qualification checks with the restarted project baseline:
minimum 4 logical CPUs and 8 GiB memory, without making dedicated resources,
an independent link, or provider attestation hard requirements.

Tasks:

- Remove exclusive-resource and independent-link blockers from the gate.
- Keep minimum host capacity and report consistency checks.
- Make virtualization/provider attestation informational in the preflight.
- Update the native qualification runbook.
