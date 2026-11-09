# 06-06 Task3 Node local credit closure boundary

Development evidence on approved Go1.27.3, native Darwin arm64 and Docker Linux
arm64. This is a local two-step safety primitive, not complete child closure,
wire transport, funded-parent settlement, clean-source acceptance or Actions.

`StopCreditGrant` commits an immutable boot/issuer-bound stop and permanent
revocation under SQLite FULL. It does not close sessions, resolve existing
writable tickets or return source balances. `FinalizeStoppedCreditGrant` checks
the stop, every ticket/outcome, original usage payload digest and exact source
conservation in one transaction. Pending or unknown writes prevent final
evidence. Successful local evidence is immutable and replays after reopen.

- `focused-native.log`: identity isolation, late/duplicate reservation denial,
  exact known/returned ticket closure, unknown upper-bound retention, injected
  stop/final write rollback, concurrent stop/reserve and corrupt inventory tests.
- `relay-native.log`: real Engine TCP relay and Agent durable gate. A destination
  write is paused after pre-write reservation. Stop cannot finalize it; after
  the original37 bytes write and settle, final evidence records37 used/4059 unused.
  The next chunk is denied and no further bytes reach the real TCP destination.
- `full-native-race.log`: complete Agent/Store/identity/upgrade/Hub/contracts and
  Node CLI race suites.
- `linux-race-build.log`: Linux arm64 Agent/Store/Hub/contracts race all passed;
  the subsequent build command failed because the CLI path was mistyped as
  `./node/cmd/vortex-node`. The raw failure is retained.
- `linux-build-corrected.log`: corrected `./cmd/vortex-node` builds passed for
  linux/amd64 and linux/arm64. amd64 is a cross-build, not native execution.
- `full-native-race-wrong-cli-path.log`: the same path typo made this earlier
  command fail despite its actual library packages passing. The corrected full
  command result is independently retained above.

Injected SQLite failures demonstrate transaction rollback, not physical power
failure. The TCP test uses real sockets but is a finite regression, not a
throughput or system acceptance result. Existing source balances remain held;
the local snapshot is explicitly not an authenticated wire receipt or authority
to release PostgreSQL funds. Complete issuer/usage inventory matching, delayed
uninstalled grants, direct authenticated transport, sustained runtime funding and
explicit recovery remain pending. CR-007 is approved; CR-008 remains unprepared
and unapproved. Phase6 stays8/11; no06-06 SUMMARY is generated.
