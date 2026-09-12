# B5 Durable Runtime State and Audit Foundation

Status: Complete on 2026-08-19

## Delivered

- Production bootstrap explicitly injects durable Session/message,
  conversation/trace, Memory, Atomic/vector, Raw Archive, and SAMG services.
- Agent, Memory, Raw Archive, MemoryAgent, Preflight, and SAMG compatibility
  getters no longer silently create empty runtime state.
- Session/message writes are transactional; list ordering is deterministic.
  Memory and SAMG include durable pagination/access metadata and integrity
  checks. All owned stores close during normal server shutdown.
- Audit now fails startup on corruption, syncs shutdown writes, retains a chain
  anchor across file rotation, and records a baseline event for every
  authenticated mutation success or failure.

## Verification

- New specifications cover restart recovery, shutdown flush, concurrent Session
  writes, Memory pagination, SAMG access recovery, Audit rotation continuity,
  and corrupt Audit startup rejection.
- `CGO_ENABLED=0 go test ./... -run '^$'` passes as a repository-wide compile
  check. Audit and other non-SQLite tests run on this host.
- SQLite runtime execution remains assigned to the CGO-capable B11 gate because
  this Windows environment does not provide `gcc`; `go-sqlite3` is a stub when
  compiled with `CGO_ENABLED=0`.

See ADR 0008 for persistence ownership and audit retention semantics.
