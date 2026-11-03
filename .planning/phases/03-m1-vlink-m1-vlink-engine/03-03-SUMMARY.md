---
phase: 03-m1-vlink-m1-vlink-engine
plan: 03
status: complete
requirements: [ENG-020, ENG-021, ENG-022, ENG-023, ENG-024]
commits: [2bae560]
---

# Plan 03-03 Summary: Local OPEN Authorization and UDP Admission

Implemented local OPEN policy evaluation against the current rule snapshot. It checks rule revision and path generation, hop, network, peer membership, visited nodes, runtime/segment permission, execution-point rate and connection shares, billing snapshot and coefficient, and the self-hosted exemption. Rejections use stable error classes and correlation IDs; audit events contain no destination details. The policy performs no control-plane calls.

TCP and UDP admission now select the target only from local runtime configuration, resolve names once, reject private/reserved and mixed DNS answer sets, apply configured address prefixes, and dial only the validated numeric endpoint. Both admission paths open the accounting gate before remote OPEN_OK can be sent. Remote OPEN confirmation now requires an authorizer callback; resources are released when OPEN is rejected, reset, or the link closes. UDP DGRAM writes also check OPEN_OK at the stream boundary.

UDP forwarding owns the authorized stream and connected target socket, accounts each direction through pre-write reservations, returns reservations for window drops, reports dropped responses through the supplied callback, and closes both forwarding directions after cancellation or link loss. It does not replay datagrams. The tests cover repeated datagrams, OPEN_OK gating, over-window drop and recovery after WINDOW credit, and resource cleanup on link loss.

## Verification

- `cd engine && go test -race ./vlink -run 'Test(Open|Target|UDP)'` passed.
- `cd engine && go test -race ./...` passed on local macOS arm64.
- `cd engine && go vet ./...` passed.
- `git diff --check` passed.
- The UDP integration fixture uses a local loopback echo endpoint; it verifies behavior, not throughput or external-address routing.

## Scope Notes

- Pool management, reverse link establishment, abuse thresholds, protocol metrics, Docker, and GitHub Actions evidence remain in plans 03-04 through 03-06.
- This local run does not claim Linux-specific or dual-host validation. Phase 3 final acceptance remains pending the Docker and Actions plan.
