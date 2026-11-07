# Phase 6 DNS and provider access boundary

`ServingService.RecomputeDNS` reads current member/incarnation versions, confirmed
TCP/UDP reservations, ownership/grant, account restrictions and persisted actor
role before committing desired DNS, operation, typed resource task, Outbox and
audit together. Only entry groups using DNS scheduling qualify. `ApplyDNS` checks
the current serving set again before provider I/O. Provider uncertainty is
`unknown`; no DNS result releases port occupation. HTTP wiring belongs to 06-07
and Phase 7 consumes the asynchronous operation contract.

`00004_entry_group_domains.sql` appends a persistent one-domain-per-group binding
with a globally unique normalized DNS name and a foreign key to the enrolled
credential. The first recompute binds it in the same audit/task/Outbox transaction;
later requests and task application must match this immutable name, access ID and
zone. Only the group owner may bind it; a grant does not give a tenant control of
a platform group's DNS. The verified zone name is stored with credential metadata
and bounds the permitted DNS suffix. Legacy credentials with no verified zone name
fail closed and need explicit re-enrollment before binding.

Platform owners recompute their group from all active tenants' committed rules.
Each member must have every committed rule's complete confirmed protocol set for
the current boot/generation and member snapshot. An uncommitted new rule does not
remove a member already serving other rules. Tenant read views remain scoped.
An explicitly unhealthy node is excluded even while its lifecycle is online.
Health is evaluated per member and excluded from the shared configuration digest;
changing one member's health does not invalidate confirmed ports on the others.
Removing DNS never releases retained ports or grants migration of existing flows.

AES-256-GCM binds tenant, access ID, zone and key version as authenticated data.
Only ciphertext, nonce, version and zone scope enter PostgreSQL. Deployment keys
remain outside the database; retain old versions for ciphertext/backups. A lost
key fails closed and requires explicit re-enrollment. Tokens never enter tasks,
audit summaries, errors or public schemas. Owner/platform administrator enrollment
and active persisted actor checks precede secret use. The internal callback API
does not return a token to HTTP callers.

Cloudflare requests use a fixed HTTPS origin, no redirects, 8-second request
timeouts, a 1 MiB response cap and bounded read retries. Writes fix TTL=60 and
`proxied=false`. Ambiguous create/update requests are not blindly retried. Replay
lists the exact name and uses a tenant/group ownership comment; foreign records
fail before writes. A 20-second external-operation deadline fits the 30-second
task lease. A failed/lost result retains unknown state for reconciliation.

## Controlled live setup

`tools/phase6/cloudflare_integration.sh` is the only live-test entry. It requires
`CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ZONE_ID`, `PHASE6_DATABASE_URL` and reviewed
`CLOUDFLARE_TOKEN_POLICY_FILE` (private regular file, mode 0600, at most 64 KiB).
Provide the local configuration path to the executor; do not paste a token into
chat, source, shell history, logs or Git. PostgreSQL must be a disposable Phase 6
test service. The test generates its encryption key in memory and cleans up its
encrypted access row and only its unique `vortex-phase6-<uuid>.<zone>` records.
It fails on missing configuration, mismatched responses or cleanup failure.

Use a dedicated test zone. Create a user-owned token with only DNS Edit/Write on
that exact zone and retain the exact policy metadata at creation for operator
review. The file contains no token value. Replace the example IDs and zone name
with the reviewed token's actual metadata:

```json
{
  "id": "0123456789abcdef0123456789abcdef",
  "status": "active",
  "zone_name": "dedicated-test.example",
  "policies": [{
    "effect": "allow",
    "resources": {"com.cloudflare.api.account.zone.abcdef0123456789abcdef0123456789": "*"},
    "permission_groups": [{"id": "4755a26eedb94da69e1066d98aa820be", "name": "DNS Write"}]
  }]
}
```

This is trusted server deployment configuration, never arbitrary client input.
Live `/user/tokens/verify` binds its ID to the supplied token. Exactly one allow
policy, one explicit zone, and one DNS Write permission ID are required. The
dedicated zone DNS-list endpoint must also succeed. The reviewed file is an
operator attestation of the exact token creation policy, not independent live
policy introspection. Revise/review it whenever token policy changes. A stale or
fabricated attestation cannot establish current least privilege; a live run must
retain that review provenance with its evidence.

An optional independent `CLOUDFLARE_POLICY_TOKEN` can instead inspect
`/user/tokens/{id}` and read the dedicated zone metadata. It needs API Tokens
Read and Zone Read; it is never persisted. This is not required and the DNS token
must not gain these permissions. Official docs confirm token metadata reads need
[API Tokens Read/Write](https://developers.cloudflare.com/api/resources/user/subresources/tokens/methods/get/).
Cloudflare documents [single-zone resource keys and permission-ID policies](https://developers.cloudflare.com/fundamentals/api/how-to/create-via-api/)
and [DNS Write for record creation](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/).
Token verification alone and listing visible zones do not prove least write scope.

## Evidence boundary and UX handoff

Local real-PostgreSQL tests cover transactional rollback, encrypted storage,
scope/key recovery, stale actors and serving state; HTTP transport fixtures cover
policy rejection, bounds, idempotent reconciliation and fail-closed responses.
They are not real Cloudflare CRUD evidence. The build-tagged live test executes
create/read/update/delete against `api.cloudflare.com` and verifies TTL, proxy,
encrypted storage, identity-bound policy and exact-name cleanup. Until that
command succeeds, PRT-015 and 06-10 acceptance remain pending.

Phase 7 displays eligible addresses separately from retained allocation and
operation state. An accepted DNS operation is pending, never an immediate success
toast. Unknown outcomes expose reconciliation status. Enrollment uses a safe
access reference; plaintext or internal provider targets do not enter public
business schemas. Phase 6 HTTP transport remains assigned to 06-07.
