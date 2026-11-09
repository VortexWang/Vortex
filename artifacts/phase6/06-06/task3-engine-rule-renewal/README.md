# Live Engine rule renewal — development evidence

The original scheduler observed only the static Engine scope and its static
activation flag. A genuine RED used two actual enabled rule Prepare/Commit
transactions and funded low-water grants, yet produced zero renewal requests.

The Agent now holds an atomic reference to its live RuntimeApplier and observes
only actual process-local Engine Active transactions. It selects the latest
active fencing generation per rule; a disabled successor suppresses retained old
transactions. Prepare, unknown/released and earlier-process durable state cannot
supply live execution evidence. A serialized observational cursor handles at most
32 dynamic rule scopes per tick; existing static behavior and all independent
trusted-time/source/path/fence/FULL journal checks remain.

Final regression uses37 real Engine rule lifecycles:33 active, one Prepare-only,
one disabled, one released, and one replaced by a disabled successor. Two bounded
passes persist32+1 requests. Production Agent sender/Hub RuntimeServer/issuer/
Node consumer install those33 over actual mTLS, reconnect without hidden retry,
retain every original200-byte pending hold and keep each original1024-byte parent
at512 issued+512 remaining. All four excluded parents remain256 issued+768
remaining. The test is an explicitly provisioned finite root/child protocol
fixture with partial initial issues; it is not a PG auto-bootstrap proof.

Failed evidence is retained: initial-fixture-compile.log was a test field typo;
red-native.log is the original scheduler failure. actual-mtls-native.log used
manual partial issuance in the flat-three auto-bootstrap shape and timed out;
that bootstrap intentionally requests its whole finite original partition. The
passing explicit root/child fixture respects the separate bootstrap boundary;
no production bootstrap or financial rules were relaxed. green-native.log and
green-lifecycle-native.log precede the final network/reconnect checks.

The first concurrent Linux check also retains two failures: root-linux-race.log
contains the existing silent-stream failover timeout, and
backend-linux-integration-race.log contains a3s regional watchdog/reconnect in
the sealed-after-materialization recovery case. These paths have Renewal=nil or
no RuntimeEngine, so the new dynamic observer is not invoked. Passing isolated
replacements, when recorded, do not relabel these original commands as PASS.
The later docker resource observation is not proof of the timeout's cause.

Only Agent code and candidate lifecycle documentation changed. Engine module,
SQL/protobuf/generators/dependencies and approval/review bytes are unchanged.
Native means Darwin arm64; Linux means Docker arm64 development checkout; six
Linux amd64/arm64 builds mean compilation. Complete post-S/R Engine/pending
rule convergence, recovery/revocation ordering and full06-06 verification remain.
No clean-source same-SHA Docker/Actions pair, native amd64 execution,06-06
completion or CR-008 approval is claimed. Phase6 remains8/11.

Final verification: native complete Agent/Store/Hub/contracts/CLI race passed.
Linux Store/Hub/contracts/CLI packages passed in the original bundle; the whole
bundle remains FAIL. A subsequent isolated complete Agent race passed91.255s,
then sequential isolated backend integration passed36.774s (native45.358s).
The final37-rule mTLS/reconnect case is included in those complete Agent runs.
Six cross-builds passed. Full backend race377.894s/native and392.441s/Linux from
936edcab is inherited; this unit reran the affected integration set, not all
backend tests. None of these are clean-source/Actions acceptance evidence.
