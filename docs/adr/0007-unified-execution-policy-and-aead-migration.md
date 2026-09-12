# ADR 0007: Unified Execution Policy and AEAD Migration

- Status: Accepted
- Date: 2026-08-19
- Related: backend hardening B4

## Context

Outbound model calls, workspace writes, dev-server process launches, hooks, and
plugin execution previously relied on separate checks. Several checks existed
only in HTTP handlers, so direct service calls could bypass them. Privacy data
was also written with unauthenticated AES-CBC.

## Decision

- `internal/policy` is the shared decision and audit boundary. Decisions carry
  the operation, resource, reason, rule version, project ID, agent ID, plugin
  ID, and audit ID.
- Enforcement occurs at the lowest shared execution paths: the base HTTP
  adapter, filesystem write service, dev-server manager, hook manager, plugin
  contribution registry, and integration invocation service.
- Server bootstrap requires enforcement before services are exposed. A missing
  evaluator denies operations. `CODEFLOW_ALLOW_LOCAL_EXECUTION=1` is the only
  explicit local-development allow mode.
- Server bootstrap requires `CODEFLOW_PRIVACY_MASTER_KEY`; it never creates an
  ephemeral encryption key that would make data unreadable after restart.
- New standard and chain-key ciphertext use AES-256-GCM. AES-CBC remains a
  read-only compatibility format. A successfully decrypted legacy document is
  rewritten to GCM once; later reads do not rewrite it again.
- Outbound URL query strings are removed before decisions and audit records are
  stored, preventing provider credentials such as Gemini `?key=` values from
  entering the audit log.
- Process command lines are represented only as the `process` resource in
  policy/audit records; commit messages and shell arguments are never logged.

## Consequences

Policy denials are non-retryable and queryable through the audit service. A
handler cannot grant authority that the underlying execution service denies.
Tests that construct isolated in-memory services can run without process-wide
enforcement, or install a fake/local evaluator explicitly; production startup
always enables enforcement.

Existing CBC data remains readable during migration, but no production write
path produces CBC. Removing legacy CBC reads requires a later migration audit
showing that no CBC records remain.
