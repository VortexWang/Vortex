# 06-06 Task3 generic exact-reconciliation parent guard

Candidate development evidence on Go1.27.3 native Darwin arm64 and Docker Linux
arm64, isolated PostgreSQL18.6 DBs. Not completed06-06 or final Phase6 acceptance.

Review found that generic `ReconcileExact`, unlike ordinary final/return methods,
could finalize an unknown delegated parent without any child stop proof. The RED
reproduction funds103, issues51, seals/returns the never-issued52, then marks the
issued parent remainder unknown. A generic zero-actual receipt incorrectly
returned success and released that retained issued hold.

- `red.log`: reproduces actual service acceptance of the unsafe generic receipt.
- `green.log`: allocated parents return stable `ErrShareUnknown` before settlement
  or audit commit; the complete authority snapshot is unchanged. Existing ordinary
 72-hour funded risk/late-exact/audit-rollback regression continues to pass.
- `backend-all-race.log`: complete native backend race suite.
- `linux-race-build.log`: Linux service/control/worker/timeauthority race and backend
  amd64/arm64 builds. Cross-build is not native amd64 execution/performance.

The guard covers both unresolved and risk-budget-settled delegated parents; it
does not infer a complete issuer inventory or Node final receipt from a generic
32-byte proof. Dedicated child closure and explicit recovery remain to be built.
No schema/query/generated code changed in this unit; the preceding sealed-redelivery
generation evidence covers the unchanged files. CR-007 is approved; CR-008 remains
unprepared/unapproved. Phase6 stays8/11, with no06-06 SUMMARY or Actions acceptance.
