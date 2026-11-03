# Contract Freeze Questions

These are source gaps, not approved design choices. Resolve every item before marking the corresponding artifact frozen.

| Contract | Open question | Source boundary |
|---|---|---|
| Database | Complete entity/column types, foreign keys, uniqueness, tenant row policies, source-specific ledger and Outbox schemas, indexes and retention | V1.11 specifies behavior and key fields, not a complete DDL catalog |
| OpenAPI | Endpoint paths, operations, request/response schemas, auth/session flow, pagination, status/error codes and SSE event schemas | GEN-007 / BE-005 specify REST/SSE and public/internal boundary only |
| Protobuf | Complete RPC surface, message encoding, idempotency keys, deadlines, sequence rules, authentication metadata and lifecycle-specific messages | V1.11 specifies gRPC/Protobuf transport but no complete .proto |
| Runtime config | Complete listener/rule/target/lease/grant schemas and exact per-key defaults/ranges for all Appendix A parameters | ENG-029 and Appendix A define fields/values at mixed levels of precision |
| VLink | Numeric type/flag assignments, canonical payload encoding, HELLO exporter signature format, stream ID lifecycle, unknown type and malformed-frame behavior | ENG-010..023 define semantic fields and fixed header, not all wire codepoints |

Contract hashes remain pending and all five versions remain `1.0.0` draft until the user approves the exact content.
