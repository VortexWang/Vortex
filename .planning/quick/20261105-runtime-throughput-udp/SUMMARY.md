---
quick_id: 261105-rudp
status: complete
completed: 2026-11-05
---

	Increased gated TCP copy chunks to 128 KiB, expanded VLink stream/aggregate
windows and bounded UDP queues, and raised UDP socket buffer requests to 32 MiB
for source, target, and relay sockets. Follow-up native evidence identified
the remaining short-term loss as source flow queue overflow, so the source
queue was raised to 1 MiB/1024 packets. Existing relay/link/runtime tests and
race tests pass. Native performance and loss qualification remain pending until
the controlled host retest completes.
