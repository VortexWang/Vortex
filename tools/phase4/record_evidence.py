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
manifest = {"roles": canonical, "runtime_source_sha256": hashlib.sha256((root / "engine/runtime/link/supervisor.go").read_bytes()).hexdigest(),
            "workload": {"duration_seconds": float(os.getenv("MIXED_DURATION_SECONDS", "5")),
                         "tcp_streams": int(os.getenv("MIXED_TCP_STREAMS", "4")),
                         "udp_mbps": float(os.getenv("MIXED_UDP_MBPS", "100")), "udp_payload_bytes": 1200}}
(out / "semantic-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
metadata = {"commit": command("git", "rev-parse", "HEAD"), "dirty": bool(command("git", "status", "--porcelain")),
            "backend": sys.argv[2], "system": platform.system(), "release": platform.release(),
            "machine": platform.machine(), "cpu_count": os.cpu_count(), "go": command("go", "version"),
            "semantic_manifest_sha256": digest(manifest), "exact_generated_config_sha256": digest(configs),
            "qualification": "not-qualified"}
if Path("/proc/meminfo").exists():
    metadata["mem_total"] = Path("/proc/meminfo").read_text().splitlines()[0]
if Path("/etc/os-release").exists():
    metadata["os_release"] = Path("/etc/os-release").read_text()
(out / "host.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n")
