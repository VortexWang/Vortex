#!/bin/sh
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

mode=${1:-native}
out=${2:-"$(pwd)/artifacts/phase4/qualification/$mode"}
formal=${PHASE4_FORMAL:-0}
policy_version=${PHASE4_POLICY_VERSION:-1}
case "$policy_version" in 1|2) ;; *) echo "PHASE4_POLICY_VERSION must be 1 or 2" >&2; exit 2 ;; esac
credit_budget=${PHASE4_SOURCE_CREDIT_BUDGET:-1099511627776}
credit_initial=${PHASE4_CREDIT_INITIAL_BLOCK:-4194304}
credit_max=${PHASE4_CREDIT_MAX_BLOCK:-4194304}
credit_target=${PHASE4_CREDIT_GRANT_TARGET:-134217728}
credit_renew=${PHASE4_CREDIT_RENEW_PERCENT:-30}
credit_observe=${PHASE4_CREDIT_OBSERVE:-false}
for value in "$credit_initial" "$credit_max" "$credit_target" "$credit_renew"; do
  case "$value" in ''|*[!0-9]*) echo "credit parameters must be positive integers" >&2; exit 2 ;; esac
done
case "$credit_observe" in true|false) ;; *) echo "invalid credit observation flag" >&2; exit 2 ;; esac
if [ "$credit_initial" -lt 1 ] || [ "$credit_max" -lt "$credit_initial" ] || [ "$credit_max" -gt 4194304 ] || [ "$credit_target" -lt "$credit_max" ] || [ "$credit_target" -gt 1099511627776 ] || [ "$credit_renew" -lt 1 ] || [ "$credit_renew" -gt 99 ]; then
  echo "invalid bounded credit parameters" >&2; exit 2
fi
case "$formal" in 0|1) ;; *) echo "PHASE4_FORMAL must be 0 or 1" >&2; exit 2 ;; esac
case "$credit_budget" in ''|*[!0-9]*) echo "PHASE4_SOURCE_CREDIT_BUDGET must be a positive integer" >&2; exit 2 ;; esac
if [ "$credit_budget" -lt 1 ] || [ "$credit_budget" -gt 1125899906842624 ]; then
  echo "PHASE4_SOURCE_CREDIT_BUDGET must be within the source credit bound" >&2
  exit 2
fi
mkdir -p "$out/config" "$out/certs"
rm -f "$out/topology-ready" "$out/qualification.txt" "$out/result.txt" "$out/mixed-workload.json"
rm -f "$out/flow-accounting.json"
rm -f "$out/report.json" "$out/drop-summary.json" "$out/target-start.json" "$out/target-final.json"
rm -f "$out"/kernel-*-start.json "$out"/kernel-*-final.json

case "$mode" in
  native) host=127.0.0.1; cert_root="$out/certs"; cert_ref="$cert_root"; ;;
  docker) host=0.0.0.0; cert_root="$out/certs"; cert_ref=/certs; ;;
  *) echo "usage: $0 native|docker [output]" >&2; exit 2 ;;
esac

cd "$out"
openssl genpkey -algorithm ED25519 -out certs/ca-key.pem
openssl req -x509 -new -key certs/ca-key.pem -out certs/ca.pem -days 2 -subj '/CN=VORTEX qualification CA'

ids='source=00000000-0000-0000-0000-000000000001 relay-1=00000000-0000-0000-0000-000000000002 relay-2=00000000-0000-0000-0000-000000000003 relay-3=00000000-0000-0000-0000-000000000004 destination=00000000-0000-0000-0000-000000000005'
for item in $ids; do
  name=${item%%=*}; id=${item#*=}
  openssl genpkey -algorithm ED25519 -out "certs/$name-key.pem"
  openssl req -new -key "certs/$name-key.pem" -out "certs/$name.csr" -subj "/CN=$name"
  printf 'subjectAltName=DNS:vortex,URI:urn:vortex:node:%s\nextendedKeyUsage=clientAuth,serverAuth\n' "$id" > "certs/$name.ext"
  openssl x509 -req -in "certs/$name.csr" -CA certs/ca.pem -CAkey certs/ca-key.pem -CAcreateserial -out "certs/$name.pem" -days 2 -extfile "certs/$name.ext"
done

json() {
  name=$1 role=$2 node_id=$3 listen=$4 peer=$5 peer_id=$6 next_id=$7 hop=$8 service=$9 target=${10}
  udp_service=
	transport=
	if [ -n "${TRANSPORT_MODE:-}" ]; then
		case "$TRANSPORT_MODE" in pool|mux|auto) ;; *) echo "invalid TRANSPORT_MODE" >&2; exit 1 ;; esac
		transport=",\"transport_mode\":\"$TRANSPORT_MODE\",\"pool_min_links\":1,\"pool_max_links\":4"
	fi
  if [ "$formal" = 1 ] && [ -z "${TRANSPORT_MODE:-}" ]; then
	transport=',"transport_mode":"mux","pool_min_links":1,"pool_max_links":4'
  fi
  if [ "$role" = source ]; then udp_service=",\"udp_service_listen\":\"$host:18082\""; fi
  auth='"qualification_only":true,'
  runtime_authority=
  authority=
  if [ "$formal" = 1 ]; then
    auth=
    runtime_authority='"runtime_authority":true,'
    if [ "$role" = source ]; then
      authority=",\"source_credit\":{\"snapshot_id\":\"phase4-formal/source/path-$policy_version/policy-$policy_version\",\"budget_bytes\":$credit_budget,\"initial_block_bytes\":$credit_initial,\"max_block_bytes\":$credit_max,\"grant_target_bytes\":$credit_target,\"renew_percent\":$credit_renew,\"observe\":$credit_observe}"
    fi
  fi
  : > "$out/events-$name.jsonl"
  cat > "config/$name.json" <<EOF
{"version":1,"node_id":"$node_id","role":"$role",$auth$runtime_authority"certificate":"$cert_ref/$name.pem","private_key":"$cert_ref/$name-key.pem","trust_bundle":"$cert_ref/ca.pem","listen":"$listen","peer":"$peer","peer_node_id":"$peer_id","next_hop_node_id":"$next_id","service_listen":"$service"$udp_service,"rule_id":"qualification-rule","rule_revision":$policy_version,"path_generation":$policy_version,"hop_index":$hop,"target":"$target","evidence_file":"$out/events-$name.jsonl","execution_point":$( if [ "$formal" = 1 ] && [ "$role" = source ]; then echo true; elif [ "$role" = destination ]; then echo true; else echo false; fi )$transport$authority}
EOF
}

if [ "$mode" = native ]; then
  json source source 00000000-0000-0000-0000-000000000001 "$host:19441" "$host:19442" 00000000-0000-0000-0000-000000000002 00000000-0000-0000-0000-000000000002 0 "$host:18080" ""
  json relay-1 relay 00000000-0000-0000-0000-000000000002 "$host:19442" "$host:19443" 00000000-0000-0000-0000-000000000001 00000000-0000-0000-0000-000000000003 0 "" ""
  json relay-2 relay 00000000-0000-0000-0000-000000000003 "$host:19443" "$host:19444" 00000000-0000-0000-0000-000000000002 00000000-0000-0000-0000-000000000004 1 "" ""
  json relay-3 relay 00000000-0000-0000-0000-000000000004 "$host:19444" "$host:19445" 00000000-0000-0000-0000-000000000003 00000000-0000-0000-0000-000000000005 2 "" ""
  json destination destination 00000000-0000-0000-0000-000000000005 "$host:19445" "" 00000000-0000-0000-0000-000000000004 "" 3 "" "$host:18081"
else
  json source source 00000000-0000-0000-0000-000000000001 "$host:9441" relay-1:9442 00000000-0000-0000-0000-000000000002 00000000-0000-0000-0000-000000000002 0 "$host:18080" ""
  json relay-1 relay 00000000-0000-0000-0000-000000000002 "$host:9442" relay-2:9443 00000000-0000-0000-0000-000000000001 00000000-0000-0000-0000-000000000003 0 "" ""
  json relay-2 relay 00000000-0000-0000-0000-000000000003 "$host:9443" relay-3:9444 00000000-0000-0000-0000-000000000002 00000000-0000-0000-0000-000000000004 1 "" ""
  json relay-3 relay 00000000-0000-0000-0000-000000000004 "$host:9444" destination:9445 00000000-0000-0000-0000-000000000003 00000000-0000-0000-0000-000000000005 2 "" ""
  json destination destination 00000000-0000-0000-0000-000000000005 "$host:9445" "" 00000000-0000-0000-0000-000000000004 "" 3 "" "172.30.0.10:18081"
fi
printf '%s\n' "$mode" > mode
python3 "$script_dir/record_evidence.py" "$out" "$mode"
