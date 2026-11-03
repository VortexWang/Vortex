#!/usr/bin/env python3
"""Fail-closed checks for the Phase 1 supply-chain and evidence registers."""

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ALLOWED = {"MIT", "BSD-2-Clause", "BSD-3-Clause", "BSD-4-Clause", "Apache-2.0", "ISC", "PostgreSQL", "Public-Domain"}
INTEGRITY_RE = re.compile(r"^(sha256:[0-9a-f]{64}|h1:[A-Za-z0-9+/]{43}=|sha256-[A-Za-z0-9+/]{43}={0,2}|sha512-[A-Za-z0-9+/]{86}==|git:[0-9a-f]{40})$")


def load(path):
    # JSON is a strict subset of YAML 1.2, keeping these registers dependency-free.
    return json.loads(Path(path).read_text(encoding="utf-8"))


def check_candidates(manifest):
    errors = []
    seen = set()
    for item in manifest.get("candidates", []):
        name = item.get("name", "<unnamed>")
        if name in seen:
            errors.append(f"duplicate candidate: {name}")
        seen.add(name)
        for field in ("version", "source", "license", "license_evidence", "integrity"):
            if not item.get(field) or item[field] == "TBD":
                errors.append(f"{name}: missing {field}")
        project_only = item.get("class") in {"project-test-image", "network-test-tool"} and item.get("license") == "proprietary-project-code-only" and "No third-party" in item.get("license_evidence", "")
        if item.get("license") not in ALLOWED and not project_only:
            errors.append(f"{name}: license is unknown or disallowed: {item.get('license')}")
        integrity = item.get("integrity", "")
        if not INTEGRITY_RE.fullmatch(integrity):
            errors.append(f"{name}: integrity is not an immutable checksum: {integrity}")
        for platform, digest in item.get("integrity_by_platform", {}).items():
            if not re.fullmatch(r"[0-9a-f]{64}", digest):
                errors.append(f"{name}: invalid SHA-256 for {platform}")
        if item.get("status") != "verified-candidate":
            errors.append(f"{name}: candidate is not source/integrity/license verified")
    if manifest.get("blocking_gaps"):
        errors.append("manifest contains blocking_gaps")
    return errors


def check_evidence(register, root=ROOT):
    errors = []
    for item in register.get("items", []):
        status = item.get("status")
        evidence = item.get("evidence_link")
        path = root / item.get("artifact", "")
        if status == "verified":
            if not path.is_file() or not evidence:
                errors.append(f"{item.get('artifact')}: verified requires original file and evidence_link")
            elif not item.get("reviewer") or not item.get("review_date") or not item.get("review_result"):
                errors.append(f"{item.get('artifact')}: verified requires reviewer, review_date and review_result")
        elif status not in {"missing", "found-unverified"}:
            errors.append(f"{item.get('artifact')}: invalid evidence status {status!r}")
    if register.get("status") == "out-of-scope":
        decision = register.get("decision")
        if not decision or not (root / decision).is_file():
            errors.append("out-of-scope disposition requires a project decision record")
        if any(item.get("status") != "missing" for item in register.get("items", [])):
            errors.append("out-of-scope disposition must preserve all unrecovered original rows as missing")
        return ("out-of-scope" if not errors else "blocked"), errors
    if errors or any(item.get("status") != "verified" for item in register.get("items", [])):
        return "blocked", errors
    return "passed", errors


def check_lock(lock, manifest, policy=None):
    """Validate the immutable subset of the approved candidate snapshot."""
    errors = []
    policy = policy or {"allowed_spdx": sorted(ALLOWED), "denied_spdx": []}
    allowed = set(policy.get("allowed_spdx", []))
    candidates = {item.get("name"): item for item in manifest.get("candidates", [])}
    expected_digest = manifest.get("approval", {}).get("manifest_sha256")
    if not expected_digest or expected_digest == "pending-until-complete":
        errors.append("candidate manifest has no approved digest")
    if lock.get("candidate_manifest_sha256") != expected_digest:
        errors.append("lock candidate digest does not match approved candidate digest")
    if lock.get("user_decision") != "approve-candidates":
        errors.append("lock lacks approve-candidates decision")

    locked_names = set()
    for item in lock.get("locked_candidates", []):
        name = item.get("name", "<unnamed>")
        if name in locked_names:
            errors.append(f"duplicate locked candidate: {name}")
        locked_names.add(name)
        candidate = candidates.get(name)
        if not candidate:
            errors.append(f"{name}: not present in candidate manifest")
            continue
        for field in ("version", "source", "license", "integrity"):
            if item.get(field) != candidate.get(field):
                errors.append(f"{name}: locked {field} differs from candidate")
        if candidate.get("status") != "verified-candidate":
            errors.append(f"{name}: candidate was not verified at approval")
        if item.get("license") not in allowed:
            errors.append(f"{name}: locked license is not allowlisted: {item.get('license')}")
        if not INTEGRITY_RE.fullmatch(item.get("integrity", "")):
            errors.append(f"{name}: locked integrity is not immutable")
        if item.get("status") != "locked":
            errors.append(f"{name}: locked entry must have status locked")
        source = str(item.get("source", ""))
        if "latest" in source or ":latest" in source:
            errors.append(f"{name}: floating source reference is forbidden")

    for item in lock.get("not_locked", []):
        name = item.get("name")
        if name in locked_names:
            errors.append(f"{name}: appears in both locked and not_locked")
        if name not in candidates:
            errors.append(f"{name}: not_locked entry absent from candidate manifest")
    return errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("candidates", "evidence", "check-lock"))
    parser.add_argument("--manifest", default=ROOT / "docs/governance/version-candidates.yaml")
    parser.add_argument("--register", default=ROOT / "docs/governance/evidence-register.yaml")
    parser.add_argument("--lock", default=ROOT / "docs/governance/version-lock.yaml")
    parser.add_argument("--policy", default=ROOT / "docs/governance/license-policy.yaml")
    args = parser.parse_args()
    try:
        if args.command == "candidates":
            data = load(args.manifest)
            errors = check_candidates(data)
            result = {"status": "ready-for-review" if not errors else "blocked", "candidate_count": len(data.get("candidates", [])), "errors": errors}
        elif args.command == "evidence":
            if Path(args.register).is_file():
                status, errors = check_evidence(load(args.register))
            else:
                status, errors = "blocked", [f"evidence register is missing: {args.register}"]
            result = {"requirement": "NFR-038", "status": status, "errors": errors}
        else:
            manifest = load(args.manifest)
            lock = load(args.lock)
            policy = load(args.policy) if Path(args.policy).is_file() else None
            errors = check_lock(lock, manifest, policy)
            result = {"status": "passed" if not errors else "blocked", "locked_count": len(lock.get("locked_candidates", [])), "not_locked_count": len(lock.get("not_locked", [])), "errors": errors}
        print(json.dumps(result, ensure_ascii=False, indent=2))
        expected = "ready-for-review" if args.command == "candidates" else ("out-of-scope" if args.command == "evidence" and result["status"] == "out-of-scope" else "passed")
        return 0 if result["status"] == expected else 1
    except (OSError, json.JSONDecodeError, TypeError) as exc:
        print(json.dumps({"status": "error", "error": str(exc)}, ensure_ascii=False), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
