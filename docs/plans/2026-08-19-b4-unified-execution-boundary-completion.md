# B4 Unified Privacy, Outbound, and Isolation Boundary

Status: Complete on 2026-08-19

## Delivered

- A shared, versioned policy decision contract now governs outbound request and
  response handling, workspace writes, dev-server process starts, hooks, plugin
  contributions, and integration/plugin invocation.
- Decisions include allow/deny, reason, rule version, project/agent/plugin
  identity, resource, operation, and audit ID. Denials are non-retryable and
  auditable through the existing audit query service.
- Production bootstrap enables fail-closed enforcement and initializes privacy
  and isolation explicitly. `CODEFLOW_PRIVACY_MASTER_KEY` is required;
  `CODEFLOW_ALLOW_LOCAL_EXECUTION=1` is the explicit local allow mode.
- Standard and chain encryption now write AES-256-GCM. Legacy CBC is read-only,
  and document decrypt migrates a CBC record to GCM exactly once.
- Outbound URL query strings are removed from policy/audit resources so API
  credentials are not recorded.

## Verification

- Focused tests cover adapter send/stream/tool-turn bypasses, response denial,
  direct workspace writes, direct process starts, hook execution, plugin
  contribution/invocation, identity-rich audit records, GCM authentication,
  and CBC compatibility migration.
- Affected non-SQLite package suites pass with `CGO_ENABLED=0`.
- Full SQLite runtime validation remains assigned to the CGO-capable B11 gate;
  the current Windows host has no `gcc`.

See ADR 0007 for the boundary and migration decision.
