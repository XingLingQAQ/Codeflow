# ADR 0006: Authoritative Project Workspace and Runtime Bindings

## Status

Accepted

## Context

Workspace requests that accepted an arbitrary filesystem root could operate on a
directory unrelated to the selected Project. Project rows also did not retain
the authoritative workspace, default Flow, or default Session, leaving restart
and retry windows where those resources could diverge.

## Decision

- Project stores a canonical `workspace_root`, `default_flow_id`,
  `default_session_id`, and `binding_state`.
- Roots are resolved with `Abs`/symlink canonicalization and checked against
  configured allowed roots before persistence.
- Workspace APIs prefer `project_id + relative_path`; legacy `root` remains a
  migration input and is never used to override an explicit Project binding.
- Project creation provisions a default Session and Flow with the same binding
  IDs. `Idempotency-Key` retries return the original result; SQLite services
  persist a create-operation journal.
- Delete is recoverable: the Project is archived, active Flows are aborted, and
  process-local workspace watchers/dev-servers are stopped. Archive and restore
  progress is persisted in a lifecycle journal; physical purge is deferred to
  retention-aware cleanup. Restore reuses the retained Session and creates a new
  runnable default Flow when the archived Flow is terminal.
- New directory bindings fail closed when no server allow-list is configured.
  `CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1` is an explicit, temporary
  desktop migration switch.

## Consequences

The server is the authority for project-to-filesystem and project-to-runtime
relationships. Clients no longer need to persist or invent absolute roots, and
retries can safely replay a create request. A separate janitor is still needed
for final retention cleanup of archived resources.
