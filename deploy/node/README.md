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

## Backend and Hub control (06-06)

生产节点同时运行两个 mTLS 控制连接。现有 `--hub` / `--control-config`
指定同区域 Hub，负责运行授权、续租与计量批次；新增
`--backend host:port --backend-ca backend-role-ca.pem --source-backend-id UUID`
指定后端直接控制连接，负责规则 Prepare/Commit、节点总份额 CAS 和有限来源根。
三个 backend 参数必须同时配置，后端证书的唯一 DNS SAN/CN 必须匹配该固定 UUID。
两个连接各自重连，Hub 切换不会取消后端会话，后端连接不会竞争计量批次确认。

`--tls-ca` 继续指定注册和节点凭据校验信任；Hub 使用独立角色 CA 时显式设置
`--hub-ca hub-role-ca.pem`。各通道共享轮换后的节点凭据，分别验证服务器角色根。
缺少可信时间、资金、份额执行回执或当前 boot 授权时，连接成功也不会允许 Commit。

生产 Hub 入口是 `go -C backend build -o vortex-hub ./cmd/vortex-hub`。
配置 `--hub-id UUID --region REGION --backend-id UUID --backend host:port`
及 `--tls-cert hub.pem --tls-key hub-key.pem --node-ca node-role-ca.pem`
`--backend-ca backend-role-ca.pem --db /state/hub.db --node-listen :19093`。
该进程只承载控制 RPC、FULL/WAL 签发账本和计量转交；节点 CA 与后端 CA 必须分离。
Linux 内核时间未同步或误差过大时停止签发。启动生成新 Hub boot；后端未完成
旧 boot 恢复时保持阻塞，不能通过复用 boot 参数或重启清零旧资金占用。

## External signed service upgrade (05-11)

Use a separate trusted `/usr/local/libexec/vortex-node-upgrader` invocation,
never a goroutine of the process being replaced. The node service executable
`/usr/local/bin/vortex-node` must resolve through `/opt/vortex-node/current`;
that selector uses relative release targets inside the private installer root.
The helper runs `vortex-node upgrade` with fixed argv through the supplied
oneshot unit. The normal node service remains non-root. Root may inspect its
private socket only when directory/socket owner match and modes remain0700/0600.

Provision raw32-byte pinned Ed25519 public key, raw64-byte detached signature,
expected SHA256 and a stable transaction UUID. The signed candidate and old binary
must both support read-only `upgrade-preflight --db ...` for schema16 before any
drain. Automatic schema migration is deliberately not supported in this path;
older releases without preflight require a separately reviewed initial deployment.
No Agent/Hub DB, WAL, credentials, fencing or financial records are snapshotted or
restored. Current implementation snapshots an explicit no-settings-changed marker.

The private upgrade-probe.json specifies an already authorized VLink peer identity,
existing rule/revision/path and bounded application request/expected response
(base64 bytes in JSON). It does not create a rule, target or quota. Its TLS+HELLO
OPEN and TCP exchange must pass the receiving node's ordinary authority/accounting
gate. Only existing controlled local health targets should be used; probes consume
real budget. Fresh boot, executable digest, FULL/WAL, current control and applied
configuration are checked before the traffic exchange.

Service stop requires systemd inactive/failed, zero MainPID/ControlPID and both
original/current cgroups unpopulated. Missing/uncertain proof retains the same
transaction as unknown. A failed candidate is stopped before old binary restart;
old service must also pass current-authority traffic health. `--recover` queries the
existing durable transaction; unresolved activation remains an explicit error.
05-11 actual OS-process upgrade/rollback and paired gates passed; systemd unit
verification is separate from a live systemd boot. See05-11-EVIDENCE.md.
