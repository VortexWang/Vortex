# Public contract ownership

`contracts/openapi/v1/openapi.yaml` is the sole editable source. It uses JSON
syntax, a valid YAML subset, so Go contract tests need no new YAML dependency.
Copy it verbatim to `backend/openapi/v1/openapi.yaml`; never edit the copy alone.
The gate copies twice to temporary paths and compares all byte streams.

Phase6 installs no OpenAPI generator. Gin adapters are handwritten. The06-07
candidate enumerates41paths/45registered operations, checked in both directions
against Gin routes. Actual HTTP integration responses are checked against its
safe schema projections. Ruby lint checks authentication, errors, cursors,
durable SSE/resync and secret/runtime exclusions; the refresh body-token
exception is restricted to its exact route and cannot be reused elsewhere.

Candidate contract SemVer3.0.0 is independent of the retained/api/v1 URL prefix.
CR-009 contains breaking changes from the old unimplemented2.3.0 public boundary;
implementation or local tests do not grant contract freeze or phase acceptance.
Current approval status is in contracts/versions.yaml and the exact CR records.
Production/session/recovery/SSE behavior is documented in
`docs/operations/public-api.md`; final same-SHA Docker/Actions pairing remains06-11.

Phase7 owns TypeScript generation from the same approved source SHA, including
exact generator version, checksum, license review and deterministic output checks.
