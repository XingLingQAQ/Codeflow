# B3 Project / Workspace / Flow / Session Binding Completion

Status: implemented and focused tests passing on 2026-08-19.

Implemented scope:

- Project SQLite rows persist canonical `workspace_root`, `default_flow_id`,
  `default_session_id`, and `binding_state`, including startup migration for
  existing databases.
- Project creation provisions and returns the actual default Flow and Session;
  the Flow carries the default Session ID.
- `Idempotency-Key` retries reuse the original bindings, with completed create
  operations recorded in the Project SQLite journal. Creation is a forward-only
  saga (`pending -> session_created -> flow_created -> completed`) and startup
  recovery reuses durable resources without destructive guessing.
- Workspace read/list/stat/write paths accept `project_id + relative_path` and
  resolve the authoritative root from Project. Legacy root inputs remain for
  migration and cannot override an explicit Project binding.
- Existing directories can be bound through the Project API. New bindings fail
  closed without `CODEFLOW_WORKSPACE_ROOTS`; the explicit
  `CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1` switch is migration-only.
- Deletion records a lifecycle journal, archives the Project, aborts active
  owned Flows, and stops process-local watchers/dev-servers. Restore reuses the
  retained Session and creates a new runnable Flow when the old default Flow is
  terminal. Sessions/Flows are retained for a later janitor.

Verification:

```text
CGO_ENABLED=0 go test ./internal/project ./internal/api/handlers ./internal/workspace \
  -run 'Test(CreateProjectWithDefaultFlow|CanonicalizeWorkspaceRoot|ArchiveProject|RecoverIncompleteProject|Workspace|DevServer)' -count=1
```

SQLite runtime tests remain blocked on this Windows host because `gcc` is not
installed; compilation-only checks pass with `CGO_ENABLED=0`.
