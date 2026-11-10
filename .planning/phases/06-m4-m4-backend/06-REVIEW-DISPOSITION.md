---
phase: 06-m4-m4-backend
review_source: 65a5335894732b7d632e870acc3d8283be224cd3
status: resolved
---

# 审查处置

当前复核计数：Critical0、Warning0、Info0。

| Finding | Severity | Disposition | Evidence |
| --- | --- | --- | --- |
| WR-06-01 conditional shell swallowed credential guard failure | warning | fixed | 3fb8e25a; producer-secret-guard/negative.json; synthetic leak exit1 / redacted exit0 |
| WR-06-02 root-owned private artifact blocked runner read | warning | fixed | d852c99d; producer-evidence-owner/negative.json; uid1001 real Linux volume |
| WR-06-03 Ruby unavailable in pinned Go image | warning | fixed | 49b64e03; producer-host-preflight logs; checks moved before long runtime suites |
| WR-06-04 task/tenant deadlock and redundant recovery scans | warning | fixed | 4420e625/30653e9b; worker-cadence-lock-order RED/GREEN, superseded denial, real process replay; new full pair required |
| WR-06-05 host Python fractional timestamp and private policy path | warning | fixed | 4420e625; RFC3339Nano integer parser and21 semantic groups; absolute private-policy preflight |
| WR-06-06 fixture preparation consumed failover/proof context | warning | fixed | a2097430; regional-fixture-context native5/Linux3 race; unchanged5s runtime bound, absent heartbeat requires sql.ErrNoRows |
| WR-06-07 operation-event tenant FK lock inversion | warning | fixed | f54c8017; worker-event-lock-order six old-source RED55P03, eight paths + expiry-event rollback GREEN; real process/socket/replay regression |

| WR-06-08 serialized renewal burst starved heartbeat/result | warning | fixed | f466bfab; renewal-control-fairness RED9vs4, native67.255s/Linux79.891s repeated3race; unchanged watchdog/convergence/no-reconnect |

| WR-06-09 status clock sampled before newer concurrent heartbeat | warning | fixed | 65a53358; deterministic preclock RED; native3race/Linux actual recovery10race171.349s; unchanged strict counters/deadline |
| WR-06-10 official release gateway504 | warning | fixed | 65a53358; same official asset query/API, original locked checksums retained;26 engineering groups/real Linuxprotoc36.2 |

本记录保留已修复发现，当前REVIEW只统计未解决项。
