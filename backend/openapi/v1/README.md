# Public contract ownership

`contracts/openapi/v1/openapi.yaml` is the sole editable source. After a change,
copy it verbatim to `backend/openapi/v1/openapi.yaml`; never edit the copy alone.
The Phase 6 gate copies twice to temporary paths and compares all byte streams.
`tools/phase6/openapi_validate.rb` checks paths, authentication, errors, cursors,
durable SSE resynchronization and the exclusion of secrets/runtime wire types.
Phase 6 installs no OpenAPI generator. Backend HTTP adapters are handwritten.

Routes marked `x-vortex-implementation-plan: 06-07` describe the adapter contract;
they do not claim implemented REST/SSE or completed BE-005 acceptance. Later
domain plans extend the source with safe public business models.

Phase 7 owns TypeScript generation from the same source SHA, including exact
generator version, checksum, license review and deterministic-output checks.
