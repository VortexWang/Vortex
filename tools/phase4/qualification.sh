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
import collections, json, os, re, sys

events_file, workload_file, start_file, final_file, out_dir = sys.argv[1:]
totals = collections.Counter()
count = 0
with open(events_file, encoding="utf-8") as events:
    for line in events:
        event = json.loads(line)
        if event["outcome"] != "committed" or event["reserved"] != event["used"]:
            raise SystemExit("accounting contains non-committed or unresolved usage")
        totals[event["direction"]] += event["used"]
        count += 1
with open(workload_file, encoding="utf-8") as source:
    workload = json.load(source)
with open(start_file, encoding="utf-8") as source:
    start = json.load(source)
with open(final_file, encoding="utf-8") as source:
    final = json.load(source)
target = {key: final[key] - start[key] for key in final}
smoke_tcp = len(b"vortex-three-relay-smoke")
smoke_udp = len(b"vortex-three-relay-udp-smoke")
expected_left = target["tcp_bytes_received"] + target["udp_bytes_received"]
expected_right = (smoke_tcp + smoke_udp + workload["baseline"]["received_bytes"] +
                  workload["mixed"]["tcp"]["received_bytes"] + workload["mixed"]["udp_bytes_received"])
expected = {"left_to_right": expected_left, "right_to_left": expected_right}
if count < 4 or dict(totals) != expected:
    raise SystemExit(f"accounting mismatch: events={count} totals={dict(totals)} expected={expected}")

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
