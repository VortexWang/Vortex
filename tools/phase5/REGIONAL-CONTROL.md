# Phase 5 regional control

`vortex-node --control-config runtime.json --hub-name <enrollment-Hub-DNS> --enrollment-hub <enrollment-address> ...` reads the owner-approved runtime envelope (`contracts/runtime/config.schema.json`). The optional `--region` must match its lifecycle region. TLS credentials/CA retain existing flags; each configured endpoint pins its own certificate DNS name and Hub UUID. Only explicitly configured same-region endpoints are considered.

Minimal control envelope (replace IDs, addresses and trust material):

```json
{
  "version": 1,
  "node_id": "550e8400-e29b-41d4-a716-446655440100",
  "listeners": [],
  "rules": [],
  "revocations": [],
  "lifecycle": {
    "region": {
      "region_id": "east",
      "hubs": [
        {
          "hub_id": "550e8400-e29b-41d4-a716-446655440101",
          "endpoint": "127.0.0.1:19090",
          "server_name": "550e8400-e29b-41d4-a716-446655440101"
        }
      ]
    },
    "dial_timeout_seconds": 5,
    "watchdog_seconds": 15,
    "failover_budget_seconds": 60,
    "retry_min_ms": 1000,
    "retry_max_ms": 30000
  }
}
```

These timing values are configurable development defaults. This control envelope does not start an Engine listener; existing `--engine-config` supplies the Engine configuration. `--hub ADDRESS --hub-name DNS --hub-id UUID --region REGION` becomes a single-entry list using the same loop. It requires negotiated control1.3.0 status support rather than assuming readiness from an old heartbeat-only peer. `--hub-name` remains the separate enrollment/credential-renewal TLS identity when a list is used.

The node sends legacy Heartbeat then fresh LifecycleHello, requires status bound to the selected certificate identity/UUID/region/session, and enforces independent readiness and status watchdog timers. Losing attempts are canceled and joined. The mutation boundary cancels an old sender before waiting for its lock; late messages cannot apply config, grants or clock updates. Sequence numbers persist and stop before signed SQLite overflow.

Online peers are preferred. Restricted peers may carry existing accounting/receipt traffic; new config/grants remain denied while restricted in this increment. Original grants and existing TCP flows keep their original bounds during a control switch. Status never refreshes authority time, funding or autonomy deadline. A disconnected or stale peer is not reported as current. Recovery/catchup and full restricted-mode capability enforcement remain 05-12 work.

`cmd/vortex-test-hub` is explicitly a protocol fixture. Lifecycle status reads existing durable issuer state. An empty database reports blocked and cannot create authority from a node heartbeat. Its `--test-delegation-template` and `--test-config-update` accept trusted offline fixture inputs for process tests, not production backend allocation. `--stall-readiness` / `--stall-after-readiness` are deliberate fault injections. Do not deploy this fixture as a production backend or unreviewed issuer.

Verification entry: `make phase5-check` in clean Linux source with an isolated small full-disk filesystem. It requires five named actual-process scenarios, ten race repetitions of regional safety tests, the root/Engine tests and raw recovery audit; missing/skipped cases fail the gate. The TCP fixture retains the historical separate Engine qualification identity, so it does not claim the shared control/VLink rotation planned in 05-09.
