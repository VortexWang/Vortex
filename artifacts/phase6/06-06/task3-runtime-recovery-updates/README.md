# Runtime recovery update delivery — development evidence

RuntimeServer now sends completed finite-recovery updates before bootstrap or
lease traffic, using a bounded32-path scan for the authenticated Node boot.
FULL outbox bytes replay exactly. Each newly materialized path has a distinct
deterministic update ID; historical persisted IDs are preserved. Never-issued
paths are excluded, and transferred original partitions cannot bootstrap again.

A reproduced older-path failure is fixed narrowly: a validated handoff with no
new grants/policy transitions can persist without lowering global path/fencing
admission floors. Every original family and direct recovery fence is still
checked. Ordinary stale heartbeats, stale grants, wrong families and lower signer
epochs remain denied; old paths cannot admit new sessions.

The actual TLS1.3 test uses production RuntimeServer and Agent RunRegion, all
three original FULL-installed authorizations, actual known usage, direct Node
credit fence and successor roots, PG admission and local Hub admission. It checks
epoch8 installation, exact reconnect bytes, one attempt per connection and the
unchanged credit partition. It waits for the original pending usage to receive
its real backend ACK, then completes the inherited financial final/refill
assertions. No Engine forwarding acceptance is implied.

## Failed attempts retained

- `red-native.log`: missing API compilation failure before implementation.
- `actual-mtls-native.log`: first actual Agent attempt timed out.
- `diagnostic-native.log`: a temporary Go overlay exposed `invalid backend bound`;
  fixture recovery time was5s ahead of wall time.
- `diagnostic-corrected-fixture.log`: corrected wall time exposed missing original
  runtime/share families. The settlement-only fixture had installed only credit.
- Corrected fixture reuses its original live Node object, waits for its explicit
  deterministic clock and installs all three original records. No production time
  or family guard was weakened. `diagnostic-full-install.log` then passes.
- `older-path-red.log`: a genuine global-floor rejection of a valid draining-path
  handoff. `older-path-green.log` covers the narrow fix and negative cases.

The diagnostic overlay lives outside the repository and only prints the existing
Connect result; no overlay is used for final acceptance runs. Initial green/full
runs preceding the final floor fix or strengthened connection assertion are kept
as intermediate evidence.

- The strengthened assertion caught a missing online UsageBackend in both `backend-*-final-race.log` runs. Epoch persistence
  alone had hidden a subsequent stream failure; those runs remain FAIL.
- `diagnostic-connection-retry.log` exposes the missing online backend.
- `actual-full-chain-native.log`/`diagnostic-full-backend.log` add actual Hub/backend
  role-mTLS and PG ingestion, but the fixed fixture clock correctly fails the
  strict newer-heartbeat check. The next fixture uses one live advancing clock
  and carries that progress back into the following deterministic assertions.

Final complete runs use `backend-*-complete-race.log`; the initial `*-final-*`
backend attempts are failures. Root final race runs pass. The later legacy test
adds only focused test coverage.

Native is Darwin arm64. Linux runs are Docker arm64 against real PostgreSQL and
Valkey. Six Linux cross-builds cover Node/Backend/Hub amd64+arm64 and prove only
compilation. No SQL/protobuf/generator input, dependency or approved snapshot
changed; lifecycle.md is candidate documentation. These are development-checkout
runs, not a clean-source same-SHA Docker/Actions pair. Phase6 remains8/11;06-06,
06-07 and06-11 remain incomplete. CR-008 is unprepared and unapproved.

Further06-06 convergence work includes a seal/child-close arriving before the
Node first receives the recovery envelope, plus actual Engine/pending deployment
Commit and post-final fresh-parent delivery. This unit tests recovery delivery
before original-parent closure; it does not claim every ordering has converged.
