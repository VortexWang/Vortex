#!/usr/bin/env python3
"""Fail-closed checks for draft contract inventory, traceability and SemVer changes."""

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ID_RE = re.compile(r"\*\*([A-Z][A-Z0-9]*-[A-Z0-9-]+)\*\*")
CONTRACT_IDS = {"vortex.database", "vortex.public-api", "vortex.control.v1", "vortex.runtime-config", "vortex.vlink.v1"}
SEMVER_RE = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
CHANGELOG_RE = re.compile(r"```json\s*(\{.*?\})\s*```", re.DOTALL)


def load(path):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def requirement_ids(markdown):
    return set(ID_RE.findall(markdown))


def check_map(requirements, mapping):
    expected = requirement_ids(requirements)
    rows = mapping.get("requirements", [])
    errors, seen = [], set()
    for row in rows:
        req = row.get("id")
        if not req or req in seen:
            errors.append(f"missing or duplicate requirement ID: {req}")
        seen.add(req)
        refs = row.get("contracts", [])
        if any(ref not in CONTRACT_IDS for ref in refs):
            errors.append(f"{req}: invalid contract reference")
        if not refs and (row.get("decision") != "not_applicable" or not row.get("reason", "").strip()):
            errors.append(f"{req}: empty mapping requires a reasoned not_applicable decision")
        if refs and row.get("decision") == "not_applicable":
            errors.append(f"{req}: cannot combine contract references with not_applicable")
    for req in sorted(expected - seen):
        errors.append(f"missing requirement: {req}")
    for req in sorted(seen - expected):
        errors.append(f"unknown requirement: {req}")
    return errors


def semver_tuple(value):
    match = SEMVER_RE.fullmatch(value or "")
    return tuple(map(int, match.groups())) if match else None


def check_change(changed_ids, before, after, changelog):
    errors = []
    log = {entry.get("contract_id"): entry for entry in changelog.get("changes", [])}
    for contract_id in changed_ids:
        old, new = semver_tuple(before.get(contract_id)), semver_tuple(after.get(contract_id))
        entry = log.get(contract_id)
        if old is None or new is None or entry is None:
            errors.append(f"{contract_id}: changed content requires valid old/new versions and a change record")
            continue
        if entry.get("version") != after[contract_id]:
            errors.append(f"{contract_id}: changelog version does not match current version")
            continue
        kind = entry.get("kind")
        valid = new[0] > old[0] if kind == "breaking" else (
            new[0] == old[0] and new[1] > old[1] if kind == "compatible" else
            new[:2] == old[:2] and new[2] > old[2] if kind == "fix" else False
        )
        if not valid:
            errors.append(f"{contract_id}: version increment does not match change kind {kind!r}")
    return errors


def check_drafts(versions, review, root=ROOT):
    errors = []
    contracts = versions.get("contracts", [])
    if {c.get("id") for c in contracts} != CONTRACT_IDS or len(contracts) != len(CONTRACT_IDS):
        errors.append("contract catalog must contain each of the five unique contract IDs")
    reviews = {c.get("id"): c for c in review.get("contracts", [])}
    for contract in contracts:
        path = root / contract.get("path", "")
        if not path.is_file():
            errors.append(f"missing contract artifact: {contract.get('path')}")
            continue
        if contract.get("version") != "1.0.0" or contract.get("status") != "draft":
            errors.append(f"{contract.get('id')}: initial contract must remain draft 1.0.0 before approval")
        expected = reviews.get(contract.get("id"), {}).get("sha256")
        if expected:
            import hashlib
            actual = hashlib.sha256(path.read_bytes()).hexdigest()
            if expected != actual:
                errors.append(f"{contract.get('id')}: review hash is stale")
    if review.get("decision") != "pending" and review.get("status") != "approved":
        errors.append("review record has inconsistent decision/status")
    return errors


def check_frozen(versions, review, root=ROOT):
    """Validate approved frozen contracts and their content hashes."""
    errors = []
    contracts = versions.get("contracts", [])
    if {c.get("id") for c in contracts} != CONTRACT_IDS or len(contracts) != len(CONTRACT_IDS):
        errors.append("contract catalog must contain each of the five unique contract IDs")
    if review.get("status") != "approved" or review.get("decision") != "freeze-contracts":
        errors.append("contract review is not approved for freeze-contracts")
    reviews = {c.get("id"): c for c in review.get("contracts", [])}
    for contract in contracts:
        contract_id = contract.get("id")
        path = root / contract.get("path", "")
        if contract.get("status") != "frozen" or contract.get("version") != "1.0.0":
            errors.append(f"{contract_id}: frozen baseline must be version 1.0.0 with status frozen")
        if not path.is_file():
            errors.append(f"missing contract artifact: {contract.get('path')}")
            continue
        expected = reviews.get(contract_id, {}).get("sha256")
        actual = __import__("hashlib").sha256(path.read_bytes()).hexdigest()
        if not expected or expected != actual:
            errors.append(f"{contract_id}: approved review hash is missing or stale")
        if reviews.get(contract_id, {}).get("decision") != "approved":
            errors.append(f"{contract_id}: individual contract decision is not approved")
    return errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("check-drafts", "check-frozen", "check-map", "check-change"))
    parser.add_argument("--versions", default=ROOT / "contracts/versions.yaml")
    parser.add_argument("--review", default=ROOT / "docs/governance/contract-review.yaml")
    parser.add_argument("--requirements", default=ROOT / ".planning/REQUIREMENTS.md")
    parser.add_argument("--map", dest="mapping", default=ROOT / "contracts/requirements-map.yaml")
    parser.add_argument("--base", help="Git commit to compare for check-change")
    args = parser.parse_args()
    try:
        if args.command == "check-drafts":
            errors = check_drafts(load(args.versions), load(args.review))
        elif args.command == "check-frozen":
            errors = check_frozen(load(args.versions), load(args.review))
        elif args.command == "check-map":
            errors = check_map(Path(args.requirements).read_text(encoding="utf-8"), load(args.mapping))
        else:
            if not args.base:
                raise ValueError("check-change requires --base")
            before_text = subprocess.check_output(["git", "show", f"{args.base}:contracts/versions.yaml"], cwd=ROOT, text=True)
            before = {c["id"]: c["version"] for c in json.loads(before_text)["contracts"]}
            after_data = load(args.versions)
            after = {c["id"]: c["version"] for c in after_data["contracts"]}
            changed = subprocess.check_output(["git", "diff", "--name-only", f"{args.base}...HEAD", "--", "contracts"], cwd=ROOT, text=True).splitlines()
            changed_ids = {c["id"] for c in after_data["contracts"] if c["path"] in changed}
            changelog_text = (ROOT / "contracts/CHANGELOG.md").read_text(encoding="utf-8")
            changelog_match = CHANGELOG_RE.search(changelog_text)
            if not changelog_match:
                raise ValueError("contracts/CHANGELOG.md is missing its machine-readable JSON change record")
            changelog = json.loads(changelog_match.group(1))
            errors = check_change(changed_ids, before, after, changelog)
            if changed_ids and "contracts/requirements-map.yaml" not in subprocess.check_output(["git", "diff", "--name-only", f"{args.base}...HEAD"], cwd=ROOT, text=True).splitlines():
                errors.append("contract content changed without a requirement-map update")
        print(json.dumps({"valid": not errors, "errors": errors}, ensure_ascii=False, indent=2))
        return 0 if not errors else 1
    except (OSError, json.JSONDecodeError, subprocess.CalledProcessError, TypeError, ValueError) as exc:
        print(json.dumps({"valid": False, "errors": [str(exc)]}, ensure_ascii=False), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
