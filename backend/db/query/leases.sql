-- name: FindLeaseNonce :one
SELECT * FROM lease_expectations WHERE tenant_id=$1 AND node_id=$2 AND kind=$3 AND nonce=$4;
-- name: LatestLeaseRevision :one
SELECT COALESCE(max(revision),0)::bigint FROM lease_expectations WHERE tenant_id=$1 AND node_id=$2 AND kind=$3;
-- name: AppendLeaseExpectation :one
INSERT INTO lease_expectations(tenant_id,id,node_id,kind,share_kind,generation,boot_id,revision,nonce,connection_id,request_digest,requested_at,issued_at,ttl_seconds,renewal_seconds,relative_deadline,ownership_until,policy_until,autonomy_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,300,60,$14,$15,$16,$17) RETURNING *;
