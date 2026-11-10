#!/bin/sh
set -eu
mkdir -p /runtime/evidence/profiles
exec /evidence/bin/vortex-node-real --diagnostics-dir "/runtime/evidence/profiles/node-$$" "$@"
