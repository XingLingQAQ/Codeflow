// Origin-scoped auth provider for the backend connection token (T0.08.a).
//
// The process token from the sidecar handshake lives only in the connection
// module's memory. This provider is the single decision point for whether a
// credential may be attached to an outgoing request: every call re-derives
// the answer from the live connection model (nothing is cached, so a token
// rotation or sidecar re-pair takes effect immediately), and the token is
// never logged, persisted, or embedded in URLs here.
//
// Attachment rule: a credential may only go to the connection base origin.
// Scheme family (http/ws vs https/wss), hostname, and effective port must all
// match — a CDN, a model provider, or the same host on another port/protocol
// gets nothing. Relative URLs resolve against the connection base. Legacy
// pairings (CODEFLOW_PORT) carry no token by design; browser mode has no
// token at all; pending/unavailable connections attach nothing.
//
// T0.08.b wires this into the HTTP helpers (Authorization: Bearer via
// authHeadersFor) and the WebSocket subprotocols (codeflow.v1 +
// codeflow.token.<token>); on top of the per-origin decision and the gated
// token accessor, this module also owns the 401→re-pair / 403→permission-latch
// state machine (handleAuthHttpStatus / getAuthFailure).

import { getBackendConnection, getBackendConnectionSnapshot, onBackendConnectionChange, refreshBackendConnection } from './connection';

export interface AuthContext {
  /** True when the target URL resolves to the connection base origin. */
  sameOrigin: boolean;
  /** Canonical origin of the target URL, or null when unparseable. */
  targetOrigin: string | null;
  /** Canonical connection base origin, or null while pending/unavailable. */
  baseOrigin: string | null;
  /** The live connection carries a handshake token (never true in legacy/browser mode). */
  tokenAvailable: boolean;
  /** Legacy pairing — no token exists by design; attachment stays off even if one appears. */
  legacy: boolean;
  /** Final decision: a credential may be attached to this URL right now. */
  attachToken: boolean;
}

type SchemeFamily = 'http' | 'https';

interface OriginKey {
  family: SchemeFamily;
  host: string;
  /** Effective port: explicit, else the family default (80/443). */
  port: string;
}

/** ws/wss are scheme twins of http/https on the same server; any other scheme never matches. */
function familyOf(protocol: string): SchemeFamily | null {
  switch (protocol) {
    case 'http:':
    case 'ws:':
      return 'http';
    case 'https:':
    case 'wss:':
      return 'https';
    default:
      return null;
  }
}

function keyOf(url: URL): OriginKey | null {
  const family = familyOf(url.protocol);
  if (!family || !url.hostname) return null;
  return {
    family,
    host: url.hostname,
    port: url.port || (family === 'https' ? '443' : '80'),
  };
}

function sameKey(a: OriginKey, b: OriginKey): boolean {
  return a.family === b.family && a.host === b.host && a.port === b.port;
}

function renderOrigin(key: OriginKey): string {
  const defaultPort = key.family === 'https' ? '443' : '80';
  return `${key.family}://${key.host}${key.port === defaultPort ? '' : `:${key.port}`}`;
}

/** Absolute URLs parse directly; anything else resolves against the connection base. */
function resolveTarget(target: string, baseUrl: string | null): OriginKey | null {
  let url: URL | null = null;
  try {
    url = new URL(target);
  } catch {
    if (baseUrl) {
      try {
        url = new URL(target, baseUrl);
      } catch {
        url = null;
      }
    }
  }
  return url ? keyOf(url) : null;
}

/**
 * Per-request auth decision, derived fresh from the live connection model.
 * The returned context never contains the token itself.
 */
export function getAuthContext(target: string): AuthContext {
  const snapshot = getBackendConnectionSnapshot();
  const connection = getBackendConnection();

  let baseKey: OriginKey | null = null;
  if (snapshot.baseUrl) {
    try {
      baseKey = keyOf(new URL(snapshot.baseUrl));
    } catch {
      baseKey = null;
    }
  }
  const targetKey = resolveTarget(target, snapshot.baseUrl);

  const sameOrigin = baseKey !== null && targetKey !== null && sameKey(targetKey, baseKey);
  const token = connection?.token;
  const tokenAvailable = typeof token === 'string' && token.length > 0;
  const legacy = connection?.legacy === true;
  const attachToken =
    snapshot.status === 'ready' && sameOrigin && tokenAvailable && !legacy;

  return {
    sameOrigin,
    targetOrigin: targetKey ? renderOrigin(targetKey) : null,
    baseOrigin: baseKey ? renderOrigin(baseKey) : null,
    tokenAvailable,
    legacy,
    attachToken,
  };
}

/**
 * The token for a request to `target`, or null when the origin does not match
 * the connection base (or no token exists). This is the only accessor step b
 * may use to build Bearer headers / WS token subprotocols — never read
 * `connection.token` directly outside the provider.
 */
export function getTokenForUrl(target: string): string | null {
  if (!getAuthContext(target).attachToken) return null;
  return getBackendConnection()?.token ?? null;
}

// --- T0.08.b wiring surface: header builder + 401/403 state machine ---

/** Permission-failure kinds the wiring layer latches. Only 'forbidden' exists today. */
export type AuthFailureKind = 'forbidden';

export type AuthFailureListener = (failure: AuthFailureKind | null) => void;

let authFailure: AuthFailureKind | null = null;
const failureListeners = new Set<AuthFailureListener>();
let rebindPromise: Promise<void> | null = null;

function setAuthFailure(next: AuthFailureKind | null): void {
  if (authFailure === next) return;
  authFailure = next;
  for (const cb of failureListeners) cb(authFailure);
}

/**
 * The latched permission failure, or null. A 403 on the pairing origin latches
 * 'forbidden' until the pairing identity actually changes (re-pair with a new
 * token, or the connection dropping) — reconnect loops must consult this and
 * stop instead of storming the backend with doomed attempts.
 */
export function getAuthFailure(): AuthFailureKind | null {
  return authFailure;
}

/** Observe latch changes; the callback fires immediately with the current value. */
export function onAuthFailureChange(cb: AuthFailureListener): () => void {
  failureListeners.add(cb);
  cb(authFailure);
  return () => {
    failureListeners.delete(cb);
  };
}

// The latch belongs to one pairing: any emitted connection change means
// applyReady/applyUnavailable replaced or cleared it (a no-op refresh that
// returns the identical connection emits nothing, so the lock survives it).
let firstConnectionEcho = true;
onBackendConnectionChange(() => {
  if (firstConnectionEcho) {
    firstConnectionEcho = false;
    return;
  }
  setAuthFailure(null);
});

/**
 * Request headers for `target`: `Authorization: Bearer <token>` exactly when
 * the origin gate allows attachment, otherwise empty. Never carries the token
 * anywhere else (no URL, no storage, no logs).
 */
export function authHeadersFor(target: string): Record<string, string> {
  const token = getTokenForUrl(target);
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/**
 * Report the HTTP status of a response from `target` after a request. Only
 * same-origin responses can move the auth state — a CDN/model-provider 401/403
 * is not our pairing's business.
 *
 * - 401: the connection's token is stale. Clear + re-pair once via
 *   refreshBackendConnection (single-flight across concurrent requests);
 *   resolves when the re-pair settled so callers can act on fresh state.
 * - 403: permission failure. Latch 'forbidden' until the pairing identity
 *   changes; the caller should surface it, not retry.
 */
export async function handleAuthHttpStatus(target: string, status: number): Promise<void> {
  if (status !== 401 && status !== 403) return;
  if (!getAuthContext(target).sameOrigin) return;
  if (status === 403) {
    setAuthFailure('forbidden');
    return;
  }
  // Single-flight only coalesces concurrent 401s: once the re-pair settles the
  // slot is released, so a later stale token (another sidecar restart) re-pairs
  // again instead of awaiting the already-settled first rebind forever.
  if (!rebindPromise) {
    rebindPromise = refreshBackendConnection()
      .then(
        () => undefined,
        () => undefined,
      )
      .finally(() => {
        rebindPromise = null;
      });
  }
  await rebindPromise;
}
