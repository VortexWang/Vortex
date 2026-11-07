-- name: CreatePackageCatalog :exec
INSERT INTO package_catalog(id,name,current_version) VALUES($1,$2,1);
-- name: LockPackageCatalog :one
SELECT * FROM package_catalog WHERE id=$1 FOR UPDATE;
-- name: AdvancePackageCatalog :exec
UPDATE package_catalog SET name=$2,current_version=current_version+1,enabled=$3 WHERE id=$1;
-- name: AppendPackageVersion :one
INSERT INTO package_versions(package_id,version,terms) VALUES($1,$2,$3) RETURNING *;
-- name: GetPackageVersion :one
SELECT * FROM package_versions WHERE package_id=$1 AND version=$2;
-- name: AppendPackageSnapshot :one
INSERT INTO package_snapshots(id,tenant_id,package_id,package_version,terms) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: GetPackageSnapshot :one
SELECT * FROM package_snapshots WHERE tenant_id=$1 AND id=$2;
-- name: LockPackageRequestKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(request_key)::text,6004));
-- name: GetPackageRequest :one
SELECT * FROM package_requests WHERE request_key=$1 FOR UPDATE;
-- name: AppendPackageRequest :exec
INSERT INTO package_requests(request_key,tenant_id,payload_sha256,result) VALUES($1,$2,$3,$4);
-- name: AppendBillingSnapshot :one
INSERT INTO billing_snapshots(id,tenant_id,package_snapshot_id,rule_id,rule_revision,path_generation,multiplier_milli,path_versions) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;
-- name: GetBillingSnapshot :one
SELECT * FROM billing_snapshots WHERE tenant_id=$1 AND id=$2;
-- name: FindBillingSnapshot :one
SELECT * FROM billing_snapshots WHERE tenant_id=$1 AND rule_id=$2 AND rule_revision=$3 AND path_generation=$4 AND package_snapshot_id=$5 AND version=1;
