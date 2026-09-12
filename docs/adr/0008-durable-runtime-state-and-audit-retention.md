# ADR 0008: Durable Runtime State and Audit Retention

- Status: Accepted
- Date: 2026-08-19
- Related: backend hardening B5

## Context

Session messages already had a SQLite store, but conversation traces, ordinary
memory items, SAMG access metadata, and several process-wide services could
silently fall back to empty in-memory instances. Audit files used a hash chain,
but startup only warned on a broken chain and retention deleted the chain
prefix without preserving verification context.

## Decision

- Production opens Session/message and Agent conversation/trace state from the
  same `sessions.db` boundary. Agent runtime snapshots are committed through a
  single SQLite transaction and validated during startup.
- Memory items, Atomic Memory, vectors, Raw Archive, and SAMG use explicit
  SQLite implementations selected by bootstrap. SAMG persists node activation
  and access history as well as triples, entities, and pointers.
- Compatibility `Get*` functions never construct an in-memory service. Tests
  inject the in-memory implementations explicitly; production readiness treats
  Agent, Memory, SAMG, and Audit as required.
- SQLite stores use WAL, a busy timeout, deterministic pagination ordering,
  schema checks, and `PRAGMA quick_check`. Serialized rows are decoded during
  startup where applicable so malformed state fails closed.
- Every authenticated non-read API request emits a baseline audit entry after
  completion, including failures. Domain services may add richer records.
- Audit startup rejects malformed JSON or an invalid hash chain. Rotation saves
  the last removed hash as a retention anchor in a sidecar manifest, so the
  retained suffix has explicit and testable verification semantics. Shutdown
  flushes and syncs buffered entries.

## Consequences

A missing durable dependency is visible as not-ready or 503 instead of an empty
data set. Restart recovery, pagination, and concurrent writes are governed by
one production store per domain. SQLite runtime tests still require the
CGO-capable toolchain defined by ADR 0003; `CGO_ENABLED=0` remains useful only
for compile checks and non-SQLite tests.

Audit retention proves continuity of the retained suffix from its persisted
anchor. It does not claim that removed content remains recoverable; deletion of
sensitive payloads must still leave a new immutable audit event.
