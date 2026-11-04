#!/bin/sh
# Keep workload generation in Linux for Compose, including on Darwin hosts.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tool=$1; shift
if [ "${BACKEND:-native}" = docker ]; then
  if [ "$tool" = mixed_workload.py ]; then
    set -- "$@" --metrics http://target:18083/stats
  fi
  exec docker compose -f "$root/test/qualification/compose.yaml" exec -T client python3 "/usr/local/bin/$tool" "$@" --host source
fi
exec python3 "$root/tools/phase4/$tool" "$@"
