# 05-04 Incremental Evidence

Status: executing; no SUMMARY or completion claimed.

## Direct reconcile safety prerequisite

- Direct `ApplyConfig` previously omitted the immutable request/fencing transaction used by the control decoder. It could start another activation while an earlier commit remained unknown.
- Both entry paths now use `SaveConfigRequest` before Engine preparation. Direct calls persist their distinct request identity and fencing floor. Reusing a transaction with changed generation is rejected. Existing rows without matching fencing evidence cannot silently acquire it.
- Tests now require an unresolved commit to block subsequent activation. Reopen coverage proves the stored fence and uncertainty survive a new coordinator instance.
- Remaining: durable execution-query proof and resource-hold resolution, fresh-boot activation orchestration, atomic signer S/R transfer and prefix/tail checkpoint handling, process-fault and paired Docker/Actions final acceptance.
