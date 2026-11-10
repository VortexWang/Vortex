"""Exact official sqlc artifact and linked-source license verification.

CR-003 and CR-004 are tuple-scoped exceptions, never changes to allowed_spdx. Review
includes nested notices and embedded-source licenses, not just module roots.
"""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
USAGE = "development-ci-sql-to-go"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def metadata(binary):
    raw = subprocess.check_output(["go", "version", "-m", str(binary)], text=True)
    dependencies = []
    main = None
    for line in raw.splitlines():
        fields = line.strip().split()
        if fields and fields[0] == "mod":
            main = fields[1:3]
        elif fields and fields[0] == "dep":
            if len(fields) != 4:
                raise ValueError("unreviewed dependency metadata")
            dependencies.append(dict(module=fields[1], version=fields[2], sum=fields[3]))
        elif fields and fields[0] == "=>":
            raise ValueError("replaced sqlc dependency")
    if main != ["github.com/sqlc-dev/sqlc", "v1.31.1"] or not dependencies:
        raise ValueError("unreviewed sqlc identity")
    return sorted(dependencies, key=lambda row: row["module"])


def source_inventory(module, version, expected_sum=None):
    result = json.loads(subprocess.check_output(
        ["go", "mod", "download", "-json", f"{module}@{version}"], text=True, cwd="/tmp"))
    if expected_sum and result["Sum"] != expected_sum:
        raise ValueError(f"source checksum mismatch: {module}")
    directory = Path(result["Dir"])
    files = {}
    for path in sorted(directory.rglob("*")):
        if path.is_file() and path.name.lower().startswith(("license", "notice", "copying", "copyright")):
            files[path.relative_to(directory).as_posix()] = digest(path)
    if not files:
        raise ValueError(f"missing source license: {module}")
    return dict(module=module, version=version, sum=result["Sum"], license_files=files)


def exception_matches(approval, artifact, usage, dependency, license_id):
    """Every field is part of the authorization, including release origin."""
    return (approval.get("status") == "approved" and approval.get("decision") == "A"
            and usage == approval.get("usage")
            and artifact in approval.get("artifacts", [])
            and dependency == approval.get("dependency")
            and license_id == approval.get("license"))


def embedded_exception_matches(approval, artifact, usage, source, license_id):
    """CR-004 covers exact source/assets, including the Bison output exception."""
    identity = {key: source.get(key) for key in
                ("module", "version", "sum", "component", "files", "binary_asset")}
    return (approval.get("id") == "CR-004"
            and approval.get("status") == "approved" and approval.get("decision") == "A"
            and usage == USAGE == approval.get("usage")
            and artifact in approval.get("artifacts", [])
            and any(identity == item.get("source") and license_id == item.get("license")
                    for item in approval.get("embedded_sources", [])))


def verify(archive, binary, platform_key, pinned):
    review = json.loads((ROOT / "tools/phase6/sqlc-dependencies.json").read_text())
    approvals = json.loads((ROOT / "docs/governance/tool-license-exceptions.json").read_text())["exceptions"]
    artifact = next(row for row in review["artifacts"] if row["platform"] == platform_key)
    if digest(archive) != artifact["archive_sha256"] or digest(binary) != artifact["binary_sha256"]:
        raise ValueError("sqlc artifact bytes changed")
    if artifact["archive_sha256"] != pinned["integrity_by_platform"][platform_key]:
        raise ValueError("sqlc version lock changed")
    actual = metadata(binary)
    expected = [{k: row[k] for k in ("module", "version", "sum")} for row in review["compiled_modules"]]
    if actual != expected:
        raise ValueError("sqlc linked closure changed")
    allowed = set(json.loads((ROOT / "docs/governance/license-policy.yaml").read_text())["allowed_spdx"])
    blocked, matched = [], []
    for row in [review["main_module"], *review["compiled_modules"]]:
        if source_inventory(row["module"], row["version"], row["sum"]) != {k: row[k] for k in ("module", "version", "sum", "license_files")}:
            raise ValueError(f"sqlc source/license bytes changed: {row['module']}")
        dependency = {k: row[k] for k in ("module", "version", "sum")}
        for license_id in row["licenses"]:
            if license_id not in allowed:
                approval = next((item for item in approvals
                                 if item.get("id") == "CR-003"
                                 and exception_matches(item, artifact, USAGE, dependency, license_id)), None)
                if approval:
                    matched.append(dict(dependency=dependency, license=license_id, exception=approval["id"]))
                else:
                    blocked.append(dict(dependency=dependency, license=license_id))
    binary_bytes = binary.read_bytes()
    for row in review["embedded_sources"]:
        source = json.loads(subprocess.check_output(
            ["go", "mod", "download", "-json", row["module"] + "@" + row["version"]], text=True, cwd="/tmp"))
        if source["Sum"] != row["sum"]:
            raise ValueError("embedded source module changed")
        directory = Path(source["Dir"])
        for name, expected_digest in row["files"].items():
            if digest(directory / name) != expected_digest:
                raise ValueError(f"embedded source/license changed: {name}")
        if row.get("binary_asset") and (directory / row["binary_asset"]).read_bytes() not in binary_bytes:
            raise ValueError("reviewed embedded asset not found in sqlc binary")
        for license_id in row["licenses"]:
            if license_id not in allowed:
                approval = next((item for item in approvals
                                 if embedded_exception_matches(item, artifact, USAGE, row, license_id)), None)
                finding = dict(component=row["component"], module=row["module"],
                               version=row["version"], sum=row["sum"], license=license_id,
                               files=row["files"])
                if approval:
                    matched.append(dict(finding, exception=approval["id"]))
                else:
                    blocked.append(finding)
    auxiliary = review["auxiliary_source_review"]
    for name, expected_digest in auxiliary["evidence_files"].items():
        if digest(ROOT / name) != expected_digest:
            raise ValueError(f"auxiliary linked-source evidence changed: {name}")
    if not set(auxiliary["licenses"]) <= allowed:
        raise ValueError("unapproved auxiliary linked-source license")
    gaps = review.get("provenance_gaps", [])
    return dict(status="blocked" if blocked or gaps else "passed", linked_modules=len(actual), artifact=artifact,
                approved_exceptions=matched, license_findings=blocked, provenance_gaps=gaps,
                auxiliary_source_status=auxiliary["status"], usage=USAGE, scope=review["scope"])
