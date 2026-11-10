# 06-06 Task3 authenticated Node credit-close transport

Candidate control1.6.0, independently negotiated `credit-close-v1` on direct,
UUID-pinned backend sessions only. Regional Hub sessions do not advertise or
execute this capability. Original issue bytes are carried without granting new
authority or refreshing trusted time. Commands bind backend/Node/boot/grant and
canonical payload SHA. FULL local stop precedes unsettled/final receipts; lost
receipts replay durable evidence. Local final is not PostgreSQL release.

- focused-native.log: actual TLS1.3 lost receipt replay, pending-to-final and
  unpinned session rejection.
- focused-scope-native.log: wrong boot/backend/grant/SHA rejected before stop.
- full-native-race.log: complete Agent/identity/Store/upgrade/Hub/contracts/CLI.
- backend-native-race.log: complete backend native race.
- linux-race-build.log: Linux arm64 Agent/Store/Hub/contracts/CLI and backend
  service/control/worker/timeauthority race; Node/backend linux amd64+arm64
  builds. amd64 is compilation evidence only.
- generation.json: two independent clean protobuf generations match both files.

All checks passed. This is development evidence, not clean-source or paired
Actions acceptance. Backend persistent close command/receipt verification and
complete issued-child/known-usage reconciliation remain pending, as do sustained
funding, explicit boot recovery and full06-06 verification. Existing local ticket
digest cannot be reconstructed from PG usage alone; an independent shared known
usage inventory is required before financial release. Phase6 stays8/11; no
06-06 SUMMARY. CR-007 approval does not approve the unprepared CR-008.
