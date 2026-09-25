// Identity-scoped cache registry (T0.08.c).
//
// §27.4: switching account / project / sidecar must drop every cross-identity
// sensitive cache. A sidecar re-pair mints a new process token and a new
// process_start_id, so every server-derived cache from the previous pairing is
// stale by construction — while anything the user authored locally (drafts,
// layout, editor buffers, chat transcripts, theme) must survive untouched.
//
// This module is the single place that decides when that reset happens. A
// cache owner registers a name + clear function once; the registry subscribes
// to the connection model and fires every clearer on a pairing-identity
// change. Pairing identity is host + port + process_start_id + legacy flag
// (the token rotates with the process, which process_start_id already covers).
//
//   first echo (subscription seed)                -> no reset
//   pending -> first ready(A)                     -> no reset
//   ready(A) -> identical refresh                 -> no reset
//   ready(A) -> unavailable                       -> no reset
//   ready(A) -> unavailable -> ready(A)           -> no reset (same process, same owner)
//   ready(A) -> ready(B), any identity difference -> reset
//   ready(A) -> unavailable -> ready(B)           -> reset
//
// One failing clearer never blocks the others. Nothing here reads, logs, or
// copies the token: only host/port/process_start_id/legacy are ever touched.

import {
  onBackendConnectionChange,
  type BackendConnection,
  type BackendConnectionSnapshot,
} from './connection';

export type IdentityScopedCacheClear = () => void;

interface IdentityScopedCache {
  name: string;
  clear: IdentityScopedCacheClear;
}

const registrations = new Map<string, IdentityScopedCache>();

let subscribed = false;
/** False until the subscription's seed echo has been consumed. */
let observed = false;
/** Identity of the last *paired* (ready) connection; null while unpaired. */
let lastIdentity: string | null = null;

/**
 * Stable key for one pairing. Null when there is no connection (pending or
 * unavailable) — "no pairing" is not an identity, so losing it is not by
 * itself a rebind. The key is opaque and carries no token.
 */
export function pairingIdentityKey(connection: BackendConnection | null): string | null {
  if (!connection) return null;
  return [
    connection.host,
    String(connection.port),
    connection.process_start_id ?? '',
    connection.legacy ? '1' : '0',
  ].join('\u0000');
}

function clearAll(): void {
  for (const { name, clear } of registrations.values()) {
    try {
      clear();
    } catch (err) {
      // Isolate failures: a broken clearer must not leave the remaining caches
      // holding the previous pairing's data. Only the registration name and
      // the error message are surfaced — never the connection payload.
      const detail = err instanceof Error ? err.message : String(err);
      console.warn(`[CodeFlow] identity-scoped cache reset failed: ${name}: ${detail}`);
    }
  }
}

function evaluate(snapshot: BackendConnectionSnapshot): void {
  const wasObserved = observed;
  observed = true;

  // Unpaired snapshots (pending / unavailable) never carry an identity: the
  // model clears its connection wholesale, so there is nothing to compare —
  // and losing the pairing is not itself a rebind. The identity of the last
  // real pairing is kept so a recovery back to the same sidecar process is
  // recognised as "same identity" below.
  if (snapshot.status !== 'ready' || !snapshot.connection) return;

  const identity = pairingIdentityKey(snapshot.connection);
  const previousIdentity = lastIdentity;
  lastIdentity = identity;

  if (!wasObserved) return; // seed echo: only records the starting state
  // Never paired before: this is the process's first ready, not a rebind. In
  // production the registrations happen at module load (connection still
  // pending), so this branch is the normal startup path — the caches are
  // empty anyway and the ticket says the first ready must not reset.
  if (previousIdentity === null) return;
  if (identity !== previousIdentity) clearAll();
}

function ensureSubscribed(): void {
  if (subscribed) return;
  subscribed = true;
  onBackendConnectionChange(evaluate);
}

/**
 * Register a server-derived cache to be cleared whenever the pairing identity
 * changes. Re-registering the same name replaces the previous entry (HMR
 * safety). Returns an unregister function; caches live for the session.
 */
export function registerIdentityScopedCache(
  name: string,
  clear: IdentityScopedCacheClear,
): () => void {
  if (typeof name !== 'string' || name.length === 0) {
    throw new Error('identity-scoped cache requires a non-empty name');
  }
  if (typeof clear !== 'function') {
    throw new Error(`identity-scoped cache ${name} requires a clear function`);
  }
  ensureSubscribed();
  registrations.set(name, { name, clear });
  let active = true;
  return () => {
    if (!active) return;
    active = false;
    registrations.delete(name);
  };
}

/**
 * Force one full reset (tests and explicit "reload from backend" actions).
 * The identity transition path above is the only automatic caller.
 */
export function clearIdentityScopedCaches(): void {
  clearAll();
}

/** Registered cache names, in registration order — for diagnostics and FU. */
export function registeredIdentityScopedCaches(): string[] {
  return [...registrations.keys()];
}

/** Structural subset of QueryClient this helper needs (no runtime import). */
export interface IdentityQueryClient {
  getMutationCache(): { clear(): void };
  removeQueries(filters: { type: 'inactive' }): void;
  resetQueries(filters: { type: 'active' }): Promise<void>;
}

/**
 * Drop every server-derived React Query entry when the pairing changes.
 *
 * `client.clear()` alone is not enough: it empties the store but leaves
 * mounted observers holding their last result — the component keeps rendering
 * the previous backend's data and never refetches. So the reset is staged:
 *
 *   1. mutation cache clear — in-flight/recorded mutations of the old pairing;
 *   2. removeQueries({type:'inactive'}) — no observer depends on them;
 *   3. resetQueries({type:'active'}) — mounted observers are notified with
 *      `data: undefined` and refetch against the new pairing.
 *
 * The reset promise is intentionally not awaited: callers run on the
 * synchronous connection-change path, and rejection is swallowed (a failed
 * refetch already surfaces through each query's own error state).
 */
export function resetQueryClientForNewPairing(client: IdentityQueryClient): void {
  client.getMutationCache().clear();
  client.removeQueries({ type: 'inactive' });
  void client.resetQueries({ type: 'active' }).catch(() => undefined);
}
