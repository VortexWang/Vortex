# 06-06 Task3 never-installed child credit tombstones

Candidate local safety evidence on Go1.27.3, native Darwin arm64 and Docker
Linux arm64. No wire command/receipt, PostgreSQL release or full06-06 acceptance
is claimed.

An original Hub issue can be FULL-committed while the Node never receives it.
`StopUninstalledCreditGrant` commits the entire original grant identity, a
permanent revocation and an immutable zero-use witness atomically. It refuses
any already installed grant or contradictory typed unknown evidence. A late
valid installation stays revoked; different scope/amount is rejected before
authority commit. The per-connection SQLite immediate writer transaction
serializes installation and absence observation across independent connections.

`Node.CloseIssuedCreditGrant` validates the full original1.2 issue against the
current Node/boot and declared Hub reference issuers before choosing the matched
installed or never-installed path. It performs no issue/application/clock/root
update. Installation and closure now share a single record conversion helper.
Only the local reference-head observation (`ShareRevision`) is excluded from
issued-identity comparison; original lineage/amount/boot/epoch/times must match.
An authenticated pinned backend transport remains required before this consumer
can be exposed remotely. A new boot cannot attest another boot never ran.

- `focused-native.log`: late-install/reopen/replay, scope mutation rollback,
  installed-history negative, FULL write-failure rollback and8 independent
  two-connection stop/installer races, plus previous closure regressions.
- `consumer-native.log`: actual FULL Hub issue/renewal bytes, current boot and
  issuer isolation, never-installed result versus installed-grant closure.
- `consumer-unknown-native.log`: the above plus a real Engine grant batch marked
  typed unknown. Transport ACK does not allow final evidence or return its64-byte
  upper bound;4032 unused/64 unknown stay in the original grant.
- `full-native-race.log`: full Agent/identity/Store/upgrade/Hub/contracts/Node CLI.
- `linux-race-build.log`: Linux arm64 Agent/Store/Hub/contracts/Node CLI race and
  linux/amd64+arm64 Node builds. amd64 is cross-build evidence only.

All commands passed. Injected failures are transaction rollback evidence, not
physical power-loss testing. Local evidence is not an authenticated wire receipt
or authority to release a financial parent. Complete original issuer/usage
inventory verification, dedicated transport, sustained funding/renewal, explicit
boot recovery, full producer and CR-008 remain pending. Phase6 stays8/11 and no
06-06 SUMMARY is produced. CR-007 authorization does not approve CR-008.
