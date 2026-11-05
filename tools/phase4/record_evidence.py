#!/usr/bin/env python3
"""Record host identity and separate semantic and exact generated config digests."""
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

out = Path(sys.argv[1])
root = Path(__file__).resolve().parents[2]

def command(*args):
    return subprocess.check_output(args, cwd=root, text=True).strip()

def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()

configs = {path.stem: json.loads(path.read_text()) for path in sorted((out / "config").glob("*.json"))}
canonical = {}
for name, config in configs.items():
    semantic = {key: value for key, value in config.items()
                if key not in {"certificate", "private_key", "trust_bundle", "evidence_file", "listen", "peer", "service_listen", "udp_service_listen", "target"}}
    semantic["tcp_service"] = bool(config.get("service_listen"))
    semantic["udp_service"] = bool(config.get("udp_service_listen"))
    semantic["target_service"] = "echo-target:18081" if config.get("target") else ""
    canonical[name] = semantic
manifest = {"runtime_environment": {key: os.getenv(key, "default") for key in ("GOMAXPROCS", "GOGC", "CPU_PROFILE", "VORTEX_QUALIFICATION_EVENT_LIMIT")}, "roles": canonical, "runtime_source_sha256": hashlib.sha256((root / "engine/runtime/link/supervisor.go").read_bytes()).hexdigest(),
            "workload": {"duration_seconds": float(os.getenv("MIXED_DURATION_SECONDS", "5")),
                         "tcp_streams": int(os.getenv("MIXED_TCP_STREAMS", "4")),
                         "udp_mbps": float(os.getenv("MIXED_UDP_MBPS", "100")), "udp_payload_bytes": 1200}}
(out / "semantic-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
metadata = {"commit": command("git", "rev-parse", "HEAD"), "dirty": bool(command("git", "status", "--porcelain")),
            "backend": sys.argv[2], "driver_placement": "dedicated-linux-client-container" if sys.argv[2] == "docker" else "native-host", "system": platform.system(), "release": platform.release(),
            "machine": platform.machine(), "cpu_count": os.cpu_count(), "go": command("go", "version"),
            "semantic_manifest_sha256": digest(manifest), "exact_generated_config_sha256": digest(configs),
            "authorization_mode": "runtime-authority" if (
                configs.get("source", {}).get("source_credit") and
                all(config.get("runtime_authority") and not config.get("qualification_only")
                    for config in configs.values())
            ) else "qualification-only",
            "qualification": "not-qualified",
            "runtime_environment": {key: os.getenv(key, "default") for key in ("GOMAXPROCS", "GOGC", "CPU_PROFILE", "VORTEX_QUALIFICATION_EVENT_LIMIT")}}
if Path("/etc/os-release").exists():
    release_fields = {}
    for line in Path("/etc/os-release").read_text().splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            release_fields[key] = value.strip().strip('"')
    metadata["os_id"] = release_fields.get("ID", "")
    metadata["os_version"] = release_fields.get("VERSION_ID", "")
if platform.system() == "Linux":
    try:
        routes = subprocess.check_output(["ip", "route", "show", "default"], text=True).splitlines()
        fields = routes[0].split() if routes else []
        metadata["qualification_interface"] = fields[fields.index("dev") + 1] if "dev" in fields else ""
    except (OSError, subprocess.CalledProcessError, ValueError):
        metadata["qualification_interface"] = ""
if Path("/proc/meminfo").exists():
    metadata["mem_total"] = Path("/proc/meminfo").read_text().splitlines()[0]
if Path("/etc/os-release").exists():
    metadata["os_release"] = Path("/etc/os-release").read_text()
(out / "host.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n")
