# Node lifecycle

Build `CGO_ENABLED=0 go build -trimpath -o vortex-node ./cmd/vortex-node` on the
approved Go toolchain. Linux amd64 and arm64 are supported; cross compilation
is a build check only, never target-runtime or performance evidence.

`vortex-node enroll` generates the private key locally. Supply a mode0600 token
file with `--token-file` or `VORTEX_ENROLLMENT_TOKEN`, plus `--node-id`,
`--enrollment-hub`, `--hub-name`, `--tls-ca`, `--db` and `--credential-dir`.
Do not put enrollment tokens in command-line arguments. Enrollment is a separate
one-time operation; production token/CA provisioning belongs to the backend.

`vortex-node run` owns one process lock before opening SQLite, recovers its
identity/ledger and starts Engine/control. Legacy invocation without `run`
still works with a deprecation message. Configure the existing runtime/control
files and private identity directory. `--admin-socket` must name a private Unix
socket in a mode0700 directory; socket mode is0600. The default is a short
per-user/per-database path under `/tmp`; services explicitly set a runtime path.

`vortex-node status --admin-socket ...` returns bounded JSON observations from
the running Agent: node/boot, applied version, current Hub mode, active workers
and drain state. It does not open another live SQLite writer.

`vortex-node drain --admin-socket ... --drain-timeout 30s` permanently stops
new stream admission, including on existing VLink connections. Existing streams
retain their original accounting bounds during grace. At timeout residual links
are closed and worker/accounting cleanup must complete before success. Unresolved
writes remain funded reservations/unknown debt; timeout is not release proof.
Repeated drain is idempotent. SIGTERM runs the same bounded drain before shutdown.

Install `deploy/systemd/vortex-node.service` with an installed binary at
`/usr/local/bin/vortex-node`. `/etc/vortex-node/node.env` contains nonsecret
VORTEX_NODE_ID, VORTEX_HUB_NAME and VORTEX_ENROLLMENT_ENDPOINT. Configure the
trust bundle, engine.json and control.json there. Enroll using a one-shot service
with the same StateDirectory identity/permissions before starting normal service.
The supplied unit uses DynamicUser, private state/runtime directories, no new
privileges and Restart=on-failure. It never restores authoritative DB/WAL during
binary replacement. systemd must allow50s stop time for the30s drain plus sync.

Build the container from repository root:
`docker build -f deploy/node/Dockerfile -t vortex-node:local .`.
It runs the actual binary as PID1/nonroot on scratch with the already approved
build image. Mount local persistent state at `/state`, owned by65532, and trusted
configuration/CA read-only; bind the same flags used by systemd. Set a stop grace
period of at least50s. Do not place SQLite/WAL on NFS or shared cross-host storage.
A kill/restart creates a new boot and preserves prior ledger/unknown debt; it
cannot revive old authorization. No deployment mode puts traffic in Hub/backend.
