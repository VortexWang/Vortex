-- name: CreateEntitlementCode :exec
INSERT INTO entitlement_codes(code_hash,kind,package_id,package_version,duration_days,amount,remaining_uses,valid_until,applicable_tenant) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: LockEntitlementCode :one
SELECT * FROM entitlement_codes WHERE code_hash=$1 FOR UPDATE;
-- name: GetEntitlementCode :one
SELECT * FROM entitlement_codes WHERE code_hash=$1;
-- name: ConsumeEntitlementCode :execrows
UPDATE entitlement_codes SET remaining_uses=remaining_uses-1 WHERE code_hash=$1 AND remaining_uses>0 AND NOT revoked AND valid_until>now();
-- name: RevokeEntitlementCode :exec
UPDATE entitlement_codes SET revoked=true WHERE code_hash=$1;
-- name: AppendEntitlementRedemption :exec
INSERT INTO entitlement_redemptions(request_key,tenant_id,code_hash,result) VALUES($1,$2,$3,$4);
