#!/usr/bin/env python3
"""Fail-closed Phase 5 evidence manifest and raw snapshot auditor."""
import argparse
import gzip
import hashlib
import json
import platform
import re
import sqlite3
import subprocess
import tempfile
from pathlib import Path


PROCESS_CASES = {
    "regional-process.log": (
        "TestRegionalProcessFailover/silent-stream",
        "TestRegionalProcessFailover/silent-established",
        "TestRegionalProcessFailover/autonomous",
        "TestRegionalProcessFailover/recovering",
        "TestRegionalProcessFailover/tcp-original-authorization",
    ),
    "renewal-process.log": (
        "TestSchedulerProcessRenewal/low-water-tcp-udp",
        "TestSchedulerProcessRenewal/lost-result",
        "TestSchedulerProcessRenewal/hub-restart",
        "TestSchedulerProcessRenewal/node-restart",
        "TestSchedulerProcessRenewal/quota-exhausted",
        "TestSchedulerProcessRenewal/write-failure",
        "TestSchedulerProcessRenewal/expiry-with-pending-result",
    ),
    "identity-process.log": ("TestIdentityProcessRotation",),
    "node-commands.log": (
        "TestNodeCommandsDrain/commands-grace",
        "TestNodeCommandsDrain/commands-force",
        "TestNodeCommandsDrain/commands-restart",
    ),
    "upgrade-process.log": (
        "TestUpgradeProcessRollback/upgrade-healthy",
        "TestUpgradeProcessRollback/upgrade-rollback",
    ),
    "recovering-process.log": (
        "TestRecoveringProcessCatchup/recovering-lost-ack",
        "TestRecoveringProcessCatchup/recovering-restart",
        "TestRecoveringProcessCatchup/recovering-before-handoff",
        "TestRecoveringProcessCatchup/recovering-after-catchup",
        "TestRecoveringProcessCatchup/recovering-wrong-hash",
    ),
    "policy-process.log": (
        "TestSharePolicyProcess/policy-cutover",
        "TestSharePolicyProcess/policy-future-revoked",
        "TestSharePolicyProcess/policy-hub-restart",
        "TestSharePolicyProcess/share-invalid-parent",
        "TestSharePolicyProcess/share-invalid-issuer",
        "TestSharePolicyProcess/share-invalid-cycle",
        "TestSharePolicyProcess/share-invalid-capacity",
        "TestSharePolicyProcess/share-invalid-revision",
    ),
}
AUDIT_FILES = (
    "audit.json",
    "policy-audit.json",
    "renewal-safety-audit.json",
    "retention-audit.json",
    "recovery/audit.json",
)
SNAPSHOT_DIRS = ("recovery", "policy", "renewal-safety", "retention")


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def inspect_snapshot(path, expected_hash):
    raw = gzip.decompress(path.read_bytes())
    if hashlib.sha256(raw).hexdigest() != expected_hash:
        raise ValueError(f"snapshot raw hash mismatch: {path}")
    with tempfile.TemporaryDirectory() as directory:
        db_path = Path(directory) / "ledger.sqlite"
        db_path.write_bytes(raw)
        # Immutable mode keeps audit reads read-only while allowing SQLite to
        # validate snapshots whose original connection used WAL on macOS.
        db = sqlite3.connect(f"file:{db_path}?mode=ro&immutable=1", uri=True)
        try:
            if db.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
                raise ValueError(f"SQLite integrity failure: {path}")
        finally:
            db.close()


def validate_manifest(root, manifest, expected_sha):
    if manifest.get("schema") != 1 or manifest.get("status") != "passed":
        raise ValueError("evidence manifest is absent or not passed")
    if manifest.get("source_sha") != expected_sha or not re.fullmatch(r"[0-9a-f]{40}", expected_sha):
        raise ValueError("source SHA mismatch")
    if manifest.get("source_clean") is not True:
        raise ValueError("source checkout was not clean")

    entries = manifest.get("files", [])
    files = {entry["path"]: entry["sha256"] for entry in entries}
    if len(files) != len(entries):
        raise ValueError("duplicate evidence manifest paths")
    if not files:
        raise ValueError("evidence file manifest is empty")
    for required_file in ("report.json", "recovery/host.txt", "recovery/recovery-hub.json"):
        if required_file not in files:
            raise ValueError(f"mandatory evidence file unhashed: {required_file}")
    for name, digest in files.items():
        if Path(name).is_absolute() or ".." in Path(name).parts:
            raise ValueError("unsafe evidence path")
        path = root / name
        if not path.is_file() or path.stat().st_size == 0 or sha256(path) != digest:
            raise ValueError(f"evidence file missing, empty or changed: {name}")
    report = json.loads((root / "report.json").read_text())
    if report.get("commit") != expected_sha or report.get("status") != "passed":
        raise ValueError("phase report is not passed for the requested source")

    cases = {entry["name"]: entry for entry in manifest.get("process_cases", [])}
    required = {case for group in PROCESS_CASES.values() for case in group}
    if set(cases) != required:
        raise ValueError("process case set is incomplete or contains unexpected entries")
    for log_name, names in PROCESS_CASES.items():
        if log_name not in files:
            raise ValueError(f"unhashed process log: {log_name}")
        log_path = root / log_name
        if not log_path.is_file() or log_path.stat().st_size == 0:
            raise ValueError(f"process log missing or empty: {log_name}")
        log = log_path.read_text(errors="replace")
        for name in names:
            line = f"--- PASS: {name} "
            if line not in log or f"--- SKIP: {name}" in log:
                raise ValueError(f"required process case did not pass: {name}")
            if cases[name].get("log") != log_name or cases[name].get("status") != "passed":
                raise ValueError(f"invalid process case record: {name}")

    audits = manifest.get("audits", {})
    if set(audits) != set(AUDIT_FILES):
        raise ValueError("audit set is incomplete")
    for name, relative in audits.items():
        path = root / relative
        if relative not in files or not path.is_file():
            raise ValueError(f"audit evidence missing: {name}")
        report = json.loads(path.read_text())
        if report.get("status") != "passed":
            raise ValueError(f"audit not passed: {name}")

    snapshots = manifest.get("snapshots", [])
    if not snapshots:
        raise ValueError("raw ledger snapshot set is empty")
    actual_snapshots = {str(path.relative_to(root)) for directory in SNAPSHOT_DIRS for path in (root / directory).rglob("*.sqlite.gz")}
    if {item["path"] for item in snapshots} != actual_snapshots:
        raise ValueError("snapshot manifest is incomplete")
    for snapshot in snapshots:
        relative = snapshot["path"]
        if relative not in files or snapshot.get("sha256") != files[relative]:
            raise ValueError(f"snapshot manifest mismatch: {relative}")
        raw_hash = snapshot.get("raw_sha256")
        if not raw_hash:
            raise ValueError(f"snapshot raw hash missing: {relative}")
        inspect_snapshot(root / relative, raw_hash)

    recovery = json.loads((root / "recovery/recovery-hub.json").read_text())
    info = recovery.get("Info", {})
    if info.get("JournalMode") != "wal" or info.get("Synchronous") != "2":
        raise ValueError("recovery Hub is not WAL/FULL")
    if manifest.get("host", {}).get("go_version") != "go1.27.3":
        raise ValueError("unexpected Go toolchain")
    if manifest.get("backend") != "protocol-test-only" or manifest.get("production_backend_authority") is not False:
        raise ValueError("fixture evidence cannot assert production backend authority")


def collect(root, expected_sha):
    report = json.loads((root / "report.json").read_text())
    if report.get("status") != "passed" or report.get("commit") != expected_sha:
        raise ValueError("phase5 report missing, failed or for another source SHA")
    host_text = (root / "recovery/host.txt").read_text()
    if f"commit: {expected_sha}" not in host_text:
        raise ValueError("host report SHA mismatch")
    host = {
        "go_version": next((line.split()[2] for line in host_text.splitlines() if line.startswith("go version ")), ""),
        "platform": next((line for line in host_text.splitlines() if line.startswith("Linux ")), ""),
        "uname_machine": platform.machine(),
    }
    source = Path(__file__).resolve().parents[2]
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True).strip()
    if head != expected_sha:
        raise ValueError("collector source differs from requested SHA")
    subprocess.run(["git", "diff", "--quiet", "HEAD", "--", ".", ":(exclude)artifacts/**"], cwd=source, check=True)
    untracked = subprocess.check_output(["git", "ls-files", "--others", "--exclude-standard", "--", "agent", "hub", "cmd", "contracts", "engine", "tools", "test", "tests"], cwd=source, text=True)
    if untracked.strip():
        raise ValueError("untracked source prevents clean-source evidence")
    files = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.stat().st_size == 0 or path.name in ("gap-evidence.json", "gap-check.log"):
            continue
        files.append({"path": str(path.relative_to(root)), "sha256": sha256(path), "size": path.stat().st_size})
    cases = []
    for log_name, names in PROCESS_CASES.items():
        log = (root / log_name).read_text(errors="replace")
        for name in names:
            if f"--- PASS: {name} " not in log or f"--- SKIP: {name}" in log:
                raise ValueError(f"required process case did not pass: {name}")
            cases.append({"name": name, "log": log_name, "status": "passed"})
    audits = {name: name for name in AUDIT_FILES}
    snapshots = []
    for directory in SNAPSHOT_DIRS:
        for path in sorted((root / directory).rglob("*.sqlite.gz")):
            raw = gzip.decompress(path.read_bytes())
            snapshots.append({"path": str(path.relative_to(root)), "sha256": sha256(path), "raw_sha256": hashlib.sha256(raw).hexdigest()})
    manifest = {
        "schema": 1,
        "status": "passed",
        "source_sha": expected_sha,
        "source_clean": True,
        "host": host,
        "backend": "protocol-test-only",
        "production_backend_authority": False,
        "process_cases": cases,
        "audits": audits,
        "snapshots": snapshots,
        "files": files,
        "contract_digests": {
            name: sha256(source / name) for name in (
                "contracts/authorization/v1/payload.schema.json",
                "contracts/protobuf/v1/vortex.proto",
                "contracts/versions.yaml",
                "docs/governance/contract-review.yaml",
            )
        },
    }
    validate_manifest(root, manifest, expected_sha)
    return manifest


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--collect", action="store_true")
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--sha", required=True)
    args = parser.parse_args()
    if not args.collect:
        raise SystemExit("--collect is required")
    manifest = collect(args.out, args.sha)
    path = args.out / "gap-evidence.json"
    path.write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n")
    print(json.dumps({"status": "passed", "source_sha": args.sha, "process_cases": len(manifest["process_cases"]), "snapshots": len(manifest["snapshots"]), "files": len(manifest["files"])}))


if __name__ == "__main__":
    main()
