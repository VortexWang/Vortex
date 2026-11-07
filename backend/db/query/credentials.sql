-- name: InsertEncryptedCredential :one
INSERT INTO encrypted_credentials(id,tenant_id,zone_id,ciphertext,nonce,key_version,scope)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: GetEncryptedCredential :one
SELECT * FROM encrypted_credentials WHERE tenant_id=$1 AND id=$2;

-- name: LockEncryptedCredential :one
SELECT * FROM encrypted_credentials WHERE tenant_id=$1 AND id=$2 FOR UPDATE;

-- name: DeleteEncryptedCredential :execrows
DELETE FROM encrypted_credentials WHERE tenant_id=$1 AND id=$2;

-- name: LeaseDNSOperation :execrows
UPDATE tasks SET state='leased',lease_token=$3,lease_until=now()+interval '30 seconds',attempts=attempts+1
WHERE tenant_id=$1 AND operation_id=$2 AND task_kind='resource_change'
AND (state IN ('pending','unknown') OR (state='leased' AND lease_until<now()));

-- name: FinishDNSOperation :execrows
UPDATE tasks SET state=$4,lease_until=NULL WHERE tenant_id=$1 AND operation_id=$2 AND lease_token=$3 AND state='leased' AND lease_until>now();
