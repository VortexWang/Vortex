# 06-06 Task3: cross-epoch funding inventory

Development checkout on baseaacfec06. Phase6 remains8/11,06-06 incomplete and
CR-008 unprepared/unapproved. No clean-source same-SHA Docker/Actions claim.

RuntimeFundingHistory no longer filters signer_epoch. Every original parent in
one tenant/Node/Node boot/billing snapshot/path/fence context remains visible,
including different signer epochs/Hub boots. The scheduler and the transactional
PrepareHubAllocation recheck consume that complete ordered parent/reservation
inventory. Independent epoch/admission/share/financial eligibility is unchanged.
The nonce still includes its desired signer epoch; only the predecessor inventory
spans epochs. Existing complete-final/zero-child-seal final classification remains.

A real PG fixture reserves two distinct financial parents via ReserveUsage and
stores a canonical earlier-epoch allocation row solely to test inventory scope;
it does not infer a live boot/admission or fabricate a recoverable authority.
Both unknown parents are visible. Tenant/Node/boot/snapshot/path/fence variations
return no rows and inventory reads change no authority or accounting. An actual
Hub SQLite FULL zero-child seal finalizes only the current epoch parent through
the production PG seal consumer. The earlier unknown parent remains unclosed;
three real funding-worker calls create no new financial parent/hold. Existing
complete-parent refill/concurrency/audit regressions remain in the focused/full
suites. This is financial inventory hardening, not a complete effective-epoch
refill or new network recovery acceptance test.

Retained attempts:
- red-native.log initially omitted the old required epoch argument and returned
  no rows. red-exact-epoch-native.log supplies the existing current epoch7 and
  proves only one of the two financial parents was visible (expected RED).
- green-native.log failed vet after an over-broad textual edit removed the
  desired epoch from the nonce format arguments. The argument is restored;
  nonce scope is unchanged. No financial guard was loosened.
- green-corrected-native.log treated the worker's conservative ErrShareUnknown
  as unexpected. The fixture now accepts exactly that existing pending error,
  while still requiring unchanged financial/allocation rows. green-complete-
  native.log passes with the actual zero-child seal and worker checks.

Two isolated sqlc1.31.1 generations match all23 worktree outputs. Four Backend/Hub
linuxamd64/arm64 cross-builds pass. Final complete native/Linux backend race
outcomes are in manifest.json. No migration,protobuf/root Agent/Hub source,
approved snapshot,public API or dependency changes. Earlier root/protobuf evidence
is inherited unchanged; Linux execution isarm64, not nativeamd64.

Remaining06-06: actual effective-epoch funding eligibility after complete finals,
RuntimeServer/pending-rule convergence and complete producer/fault/clean-source
checks before concreteCR-008 review.06-07 HTTP and06-11 same-SHA pairing pending.

Final complete backend native race passes(service302.138s); Linux arm64 race passes(service318.172s). Four Backend/Hub cross-builds and two isolated23-output sqlc generations pass.
