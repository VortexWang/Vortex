# Stopped original credit before recovery — development evidence

A direct credit stop can arrive before the Hub recovery update. The original
consumer rejected the revoked original family, even with an exact independent
backend recovery fence. The actual mTLS RED and the exact store RED are retained.

A pure recovery envelope may now retain only that handoff history if there is
both a local permanent stop for the exact root and the immutable backend fence.
All original parent/source/Node/boot/Hub/adjacent epoch/R/sealed sequence/hash
checks still apply. The transaction contains no new grants or policy transition.
The original root remains revoked and every later source spend still requires
an active root. A general revoked root without this independent fence remains
rejected, including the existing legacy revocation regression.

Store checks cover missing/changed fence, new-grant/policy refusal, FULL rollback,
concurrent exact replay, reopen, unchanged balances, outstanding ticket retention
and refusal to finalize an unresolved child or spend from the stopped source.
The actual production RuntimeServer/Agent plus Hub/backend role-mTLS and PG usage
ACK test now also covers direct stop before first recovery delivery. The original
known37-byte debt remains, and inherited parent final/fresh funding checks follow.

Native runs are Darwin arm64; Linux runs are Docker arm64. Linux amd64/arm64
cross-builds are compilation evidence. SQL/protobuf/generators/dependencies and
approved snapshots are unchanged; lifecycle.md is candidate documentation. No
clean-source same-SHA Docker/Actions pair, native amd64 execution, complete Engine
recovery or complete06-06 acceptance is claimed. Phase6 stays8/11;CR-008 is not
prepared or approved. Seal-before-first-delivery and other message orderings,
older-path revocation-only updates, automatic transferred-credit renewal and
Engine/pending deployment convergence remain ongoing work.
