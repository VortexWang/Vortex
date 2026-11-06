"""Verify the current backend compiled closure against reviewed exact bytes.

The inventory includes production and test packages on both supported Linux
architectures plus the executing host. Module-graph-only dependencies are not
represented as linked code. This is not a distributed-image license audit.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import zipfile
from sqlc_license_check import verify as verify_sqlc

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "tools/phase6/backend-dependencies.json"


def command(args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def inventory():
    modules = {}
    for goos, goarch in {("linux", "amd64"), ("linux", "arm64"),
                           (command(["go", "env", "GOOS"]), command(["go", "env", "GOARCH"]))}:
        env = dict(os.environ, GOOS=goos, GOARCH=goarch)
        raw = command(["go", "-C", "backend", "list", "-deps", "-test", "-f",
                       "{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}", "./..."], env=env)
        for row in raw.splitlines():
            if not row or row.startswith("vortex"):
                continue
            name, version, directory = row.split("|")
            modules[name] = (version, Path(directory))
    sums = {tuple(parts[:2]): parts[2] for line in (ROOT / "backend/go.sum").read_text().splitlines()
            if len(parts := line.split()) == 3}
    result = []
    for name, (version, directory) in sorted(modules.items()):
        files = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(directory.iterdir())
                 if p.is_file() and p.name.lower().startswith(("license", "notice", "copying"))}
        if not files:
            raise RuntimeError(f"missing license files: {name}")
        result.append(dict(module=name, version=version, sum=sums[(name, version)], license_files=files))
    return result


def main():
    work = Path(sys.argv[1])
    lock = json.loads((ROOT / "docs/governance/version-lock.yaml").read_text())
    pinned = {item["name"]: item for item in lock["locked_candidates"]}
    assert pinned["Go"]["version"] == "1.27.3"
    assert pinned["Goose"]["version"] == "v3.29.0"
    assert pinned["sqlc"]["version"] == "v1.31.1"
    assert pinned["protoc"]["version"] == "36.2"
    actual = inventory()
    reviewed = json.loads(MANIFEST.read_text())
    assert actual == reviewed["compiled_modules"], "dependency/license bytes changed; review the new closure"
    allowed = set(json.loads((ROOT / "docs/governance/license-policy.yaml").read_text())["allowed_spdx"])
    assert all(set(row["licenses"]) <= allowed for row in reviewed["license_review"])
    assert {row["module"] for row in reviewed["license_review"]} == {row["module"] for row in actual}
    goose = next(row for row in actual if row["module"] == "github.com/pressly/goose/v3")
    assert goose["sum"] == pinned["Goose"]["integrity"], "Goose source checksum mismatch"
    # Local replacements are exact repository source, verified by the clean-SHA
    # wrapper. go mod verify must operate on the external graph separately.
    verify = work / "verify"
    verify.mkdir()
    for name in ("go.mod", "go.sum"):
        (verify / name).write_bytes((ROOT / "backend" / name).read_bytes())
    command(["go", "-C", str(verify), "mod", "edit", "-droprequire=vortex", "-droprequire=vortex/engine"])
    print(command(["go", "-C", str(verify), "mod", "verify"]))
    system = platform.system().lower()
    arch = "arm64" if platform.machine() in {"arm64", "aarch64"} else "amd64"
    platform_key = f"{system}-{arch}"
    assert hashlib.sha256((work / "sqlc.tar.gz").read_bytes()).hexdigest() == pinned["sqlc"]["integrity_by_platform"][platform_key]
    archive_platform = {"darwin-arm64": "osx-aarch_64", "linux-amd64": "linux-x86_64", "linux-arm64": "linux-aarch_64"}[platform_key]
    download = f"https://github.com/protocolbuffers/protobuf/releases/download/v36.2/protoc-36.2-{archive_platform}.zip"
    archive = work / "protoc.zip"
    # Use curl to honor the environment's proxy/CA configuration consistently.
    subprocess.run(["curl", "--fail", "--location", "--silent", "--show-error", download, "-o", str(archive)], check=True)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    assert digest == pinned["protoc"]["integrity_by_platform"][platform_key], "protoc archive checksum mismatch"
    with zipfile.ZipFile(archive) as source:
        source.extract("bin/protoc", work)
    binary = work / "bin/protoc"
    binary.chmod(0o755)
    assert command([str(binary), "--version"]) == "libprotoc 36.2"
    generator = verify_sqlc(work / "sqlc.tar.gz", work / "sqlc", platform_key, pinned["sqlc"])
    blocked = generator["license_findings"]
    print(json.dumps(dict(status=generator["status"], go="1.27.3", goose=goose["version"], goose_sum=goose["sum"],
                          sqlc="v1.31.1", protoc="36.2", protoc_archive_sha256=digest,
                          compiled_modules=len(actual), targets=["linux/amd64", "linux/arm64", "host"],
                          scope="backend production/test and exact sqlc generator linked and embedded licenses; protoc checksum; external services excluded",
                          generator_closure_status=generator["status"], generator=generator,
                          license_findings=blocked)))
    if generator["status"] != "passed":
        raise SystemExit("sqlc generator contains an unapproved license or unresolved source provenance")


if __name__ == "__main__":
    main()
