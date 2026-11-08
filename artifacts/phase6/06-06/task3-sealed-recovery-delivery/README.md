# Seal before first runtime recovery delivery — development evidence

Actual RuntimeServer/Agent and independent Hub/backend mTLS cover both first
materialization after seal and an immutable recovery outbox created before seal.
Both stop the original credit before first network delivery. Each connects twice
without hidden retries, preserves revoked original credit and37 known used bytes,
checks exact replay bytes and then runs the inherited child final/parent final/
one fresh successor funding assertions. The existing open/stopped variants also
pass in the same native and Linux focused race runs.

Only tests and candidate documentation changed. Production/root/binary sources,
SQL/protobuf/generators/dependencies and approved snapshots are unchanged from
f1cc01c0, whose full native/Linux backend/root race and six builds are inherited.
No redundant full/build/generator rerun is claimed. This is Darwin arm64 and
Docker Linux arm64 development evidence, not native amd64 or clean same-SHA
Docker/Actions acceptance. Other delivery orderings, automatic transferred-credit
renewal and Engine/pending deployment convergence remain;Phase6 stays8/11,
06-06 incomplete and CR-008 unprepared/unapproved.
