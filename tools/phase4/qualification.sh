#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mode=${BACKEND:-native}
qual=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
export BACKEND="$mode" QUALIFICATION_DIR="$qual"

"$root/tools/phase4/topology-smoke.sh"

for name in source relay-1 relay-2 relay-3 destination; do
  test -s "$qual/logs/$name.log"
  grep -q 'OPEN authorized' "$qual/logs/$name.log"
  if grep -q 'sustained control frame rate exceeded\|repeated .* violations' "$qual/logs/$name.log"; then
    echo "qualification blocked: protocol violation threshold reached in $name" >&2
    exit 1
  fi
done
test "$(cat "$qual/client.out")" = "vortex-three-relay-smoke"
test "$(cat "$qual/udp-client.out")" = "vortex-three-relay-udp-smoke"

python3 - "$qual/events-destination.jsonl" "$qual/mixed-workload.json" "$qual/target-start.json" "$qual/target-final.json" "$qual" <<'PY'
import collections, hashlib, json, os, re, sys
from pathlib import Path

events_file, workload_file, start_file, final_file, out_dir = sys.argv[1:]
totals = collections.Counter()
returned = collections.Counter()
count = 0
flows = collections.defaultdict(collections.Counter)
with open(events_file, encoding="utf-8") as events:
    for line in events:
        event = json.loads(line)
        if event["outcome"] == "returned" and event["used"] == 0:
            returned[event["direction"]] += event["reserved"]
            continue
        if event["outcome"] != "committed" or event["reserved"] != event["used"]:
            raise SystemExit("accounting contains non-committed or unresolved usage")
        totals[event["direction"]] += event["used"]
        key = event["flow"]["Key"]
        if not re.fullmatch(r"[0-9a-f]{32}/[1-9][0-9]*/[12]", key):
            raise SystemExit("missing unique link/stream/network accounting key")
        flows[key][event["direction"]] += event["used"]
        count += 1
with open(workload_file, encoding="utf-8") as source:
    workload = json.load(source)
with open(start_file, encoding="utf-8") as source:
    start = json.load(source)
with open(final_file, encoding="utf-8") as source:
    final = json.load(source)
target = {key: final[key] - start[key] for key in final}
container_kernel_drops = {}
for name in ("source", "destination", "target"):
    first = Path(out_dir, "kernel-" + name + "-start.json")
    last = Path(out_dir, "kernel-" + name + "-final.json")
    if first.exists() and last.exists():
        before, after = json.loads(first.read_text()), json.loads(last.read_text())
        delta = after["RcvbufErrors"] - before["RcvbufErrors"]
        if delta < 0:
            raise SystemExit("container UDP counter reset during workload")
        container_kernel_drops[name] = delta
smoke_tcp = len(b"vortex-three-relay-smoke")
smoke_udp = len(b"vortex-three-relay-udp-smoke")
expected_left = target["tcp_bytes_received"] + target["udp_bytes_received"] + 1200 * container_kernel_drops.get("target", 0)
expected_right = target["tcp_bytes_sent"] + target["udp_bytes_sent"] - returned["right_to_left"] - 1200 * container_kernel_drops.get("destination", 0)
expected = {"left_to_right": expected_left, "right_to_left": expected_right}
if count < 4 or dict(totals) != expected:
    raise SystemExit(f"accounting mismatch: events={count} totals={dict(totals)} expected={expected}")

tcp_flows = {k: dict(v) for k, v in flows.items() if k.endswith("/1")}
udp_flows = {k: dict(v) for k, v in flows.items() if k.endswith("/2")}
if len(tcp_flows) != 1 + 2 * workload["tcp_streams"] or len(udp_flows) != 2:
    raise SystemExit("unexpected or colliding per-flow accounting identities")
for key, flow in tcp_flows.items():
    if flow.get("left_to_right", 0) != flow.get("right_to_left", 0):
        raise SystemExit("TCP per-flow echo accounting mismatch: " + key)
Path(out_dir, "flow-accounting.json").write_text(json.dumps({"tcp": tcp_flows, "udp": udp_flows}, indent=2, sort_keys=True) + "\n")

drop_counts = collections.Counter()
drop_line = re.compile(r"udp (?:source queue dropped datagram|source datagram dropped|forwarding dropped datagram|target response dropped).*reason=([a-zA-Z0-9_]+)")
summary_line = re.compile(r"udp link drop summary.*window_drops=(\d+).*queue_drops=(\d+).*receive_queue_drops=(\d+)")
source_summary = re.compile(r"udp source drop summary.*queue_drops=(\d+).*oversized_drops=(\d+).*capacity_drops=(\d+)")
for name in ("source", "relay-1", "relay-2", "relay-3", "destination"):
    with open(os.path.join(out_dir, "logs", name + ".log"), encoding="utf-8", errors="replace") as log:
        for line in log:
            match = drop_line.search(line)
            summary = summary_line.search(line)
            source = source_summary.search(line)
            if match:
                drop_counts[match.group(1)] += 1
            if summary:
                drop_counts["window_full"] += int(summary.group(1))
                drop_counts["queue_full"] += int(summary.group(2))
                drop_counts["receive_queue_full"] += int(summary.group(3))
            if source:
                drop_counts["source_queue_full"] += int(source.group(1))
                drop_counts["oversized"] += int(source.group(2))
                drop_counts["source_capacity"] += int(source.group(3))
drop_counts["kernel_rcvbuf_driver_scope"] += workload.get("kernel_udp_driver_delta", {}).get("RcvbufErrors", 0)
for name, lost in container_kernel_drops.items():
    drop_counts["kernel_rcvbuf_container_" + name] += lost
with open(os.path.join(out_dir, "drop-summary.json"), "w", encoding="utf-8") as output:
    json.dump(dict(sorted(drop_counts.items())), output, indent=2, sort_keys=True)
    output.write("\n")
print(json.dumps({"events": count, "accounting": dict(totals), "expected": expected,
                  "target": target, "classified_drops": dict(drop_counts)}, sort_keys=True))
lost = workload["mixed"]["udp_sent"] - workload["mixed"]["udp_unique_received"]
classified = sum(drop_counts.values())
if lost != classified:
    raise SystemExit(f"UDP loss is not fully classified: lost={lost} classified={classified}")
report = {
    "schema": 1, "runtime_correctness": "pass", "performance_qualification": "not-qualified",
    "accounting": dict(totals), "classified_drops": dict(drop_counts),
    "returned_reservations": dict(returned),
    "kernel_counter_scope": "driver and dedicated fixture container network namespaces; diagnostic attribution only, not per-flow qualification",
    "end_to_end_received_bytes": (smoke_tcp + smoke_udp + workload["baseline"]["received_bytes"] +
                                  workload["mixed"]["tcp"]["received_bytes"] + workload["mixed"]["udp_bytes_received"]),
    "nfr_003": {"status": "not-qualified", "mixed_tcp_mbps": workload["mixed"]["tcp"]["throughput_mbps"],
                "tcp_only_retention": workload["tcp_retention"], "reason": "TCP-only is a contention baseline, not an independent same-path minimum TLS baseline; host eligibility must be reviewed"},
    "nfr_005": {"status": "not-qualified", "udp_loss": workload["mixed"]["udp_loss"],
                "loss_threshold_met": workload["mixed"]["udp_loss"] <= 0.001,
                "reason": "Short diagnostic run; host and duration qualification remain open"},
    "nfr_006": {"status": "not-qualified", "reason": "mixed workload does not establish routing fault/forged OPEN coverage"},
    "nfr_007": {"status": "not-qualified", "reason": "independent path latency baseline is missing"},
    "d_04": {"status": "pending-review"},
}
with open(os.path.join(out_dir, "report.json"), "w", encoding="utf-8") as output:
    json.dump(report, output, indent=2, sort_keys=True)
    output.write("\n")
paths = sorted(set(Path(out_dir).glob("*.json")) | set(Path(out_dir).glob("events-*.jsonl")) | set(Path(out_dir, "logs").glob("*.log")))
with open(os.path.join(out_dir, "checksums.sha256"), "w", encoding="utf-8") as output:
    for path in paths:
        checksum = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                checksum.update(chunk)
        output.write(checksum.hexdigest() + "  " + str(path.relative_to(out_dir)) + "\n")
PY

events=0
for file in "$qual"/events-*.jsonl; do
  if test -s "$file"; then events=$((events + 1)); fi
done
if test "$events" -lt 1; then
  echo "qualification blocked: accounting evidence is missing" >&2
  exit 1
fi
printf 'qualification=tcp-udp-mixed-runtime-and-accounting-pass\nperformance_qualification=not-qualified\nevent_files=%s\n' "$events" >"$qual/qualification.txt"
