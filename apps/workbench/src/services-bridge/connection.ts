// Backend connection model (T0.07.c).
//
// In Tauri the sidecar handshake is exposed through the `get_backend_connection`
// invoke (src-tauri/src/lib.rs). The Rust side only publishes a connection once
// a complete `CODEFLOW_HANDSHAKE:` line has been parsed — split or delayed
// stdout chunks surface here as `null` (still pending). We poll until a full
// connection appears and stop at `unavailable` on timeout; there is no default
// port fallback and no probing of localhost services.
//
// In a plain browser there is no pairing: the base URL comes from
// VITE_API_BASE or the page origin, again without probing.
//
// The process token lives in module memory only. It is never logged,
// persisted, or attached to anything by this module — T0.08 wires it into
// HTTP/WS credentials.

export interface BackendConnection {
  host: string;
  port: number;
  /** Process token from the handshake; absent only in legacy mode. */
  token?: string;
  expires_at?: string;
  process_start_id?: string;
  /** True when discovered via the legacy `CODEFLOW_PORT:` protocol (no token). */
  legacy: boolean;
}

export type BackendConnectionStatus = 'pending' | 'ready' | 'unavailable';

export interface BackendConnectionSnapshot {
  status: BackendConnectionStatus;
  connection: BackendConnection | null;
  /** HTTP base URL the workbench may talk to; null while pending/unavailable. */
  baseUrl: string | null;
}

export type BackendConnectionListener = (snapshot: BackendConnectionSnapshot) => void;

type InvokeFn = (command: string) => Promise<unknown>;

export interface InitBackendConnectionOptions {
  /** Poll attempts before giving up (default 20 × 500ms = 10s budget). */
  maxAttempts?: number;
  intervalMs?: number;
  /** Test seam: replaces the Tauri invoke channel. */
  invoke?: InvokeFn;
  /** Test seam: replaces Tauri runtime detection. */
  isTauri?: () => boolean;
}

const DEFAULT_MAX_ATTEMPTS = 20;
const DEFAULT_INTERVAL_MS = 500;

let status: BackendConnectionStatus = 'pending';
let connection: BackendConnection | null = null;
let baseUrl: string | null = null;
let initPromise: Promise<BackendConnectionStatus> | null = null;
let invokeFn: InvokeFn | null = null;
const listeners = new Set<BackendConnectionListener>();

function snapshot(): BackendConnectionSnapshot {
  return { status, connection: getBackendConnection(), baseUrl };
}

export function getBackendConnectionSnapshot(): BackendConnectionSnapshot {
  return snapshot();
}

/** The live connection (shallow copy), or null. T0.08 reads `token` from here. */
export function getBackendConnection(): BackendConnection | null {
  return connection ? { ...connection } : null;
}

/** Observe connection changes; the callback fires immediately with the current snapshot. */
export function onBackendConnectionChange(cb: BackendConnectionListener): () => void {
  listeners.add(cb);
  cb(snapshot());
  return () => {
    listeners.delete(cb);
  };
}

function emit(): void {
  const snap = snapshot();
  for (const cb of listeners) cb(snap);
}

function sameConnection(a: BackendConnection | null, b: BackendConnection | null): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.host === b.host &&
    a.port === b.port &&
    a.legacy === b.legacy &&
    a.token === b.token &&
    a.expires_at === b.expires_at &&
    a.process_start_id === b.process_start_id
  );
}

function applyReady(conn: BackendConnection): void {
  const changed = status !== 'ready' || !sameConnection(connection, conn);
  connection = conn;
  baseUrl = `http://${conn.host}:${conn.port}`;
  status = 'ready';
  if (changed) {
    // Never log token/expires_at — host/port/legacy are not sensitive.
    console.log(
      `[CodeFlow] Backend connection ready on ${conn.host}:${conn.port}` +
        (conn.legacy ? ' (legacy protocol, no token)' : '')
    );
    emit();
  }
}

function applyUnavailable(): void {
  const changed = status !== 'unavailable';
  connection = null;
  baseUrl = null;
  status = 'unavailable';
  if (changed) emit();
}

function isValidConnection(value: unknown): value is BackendConnection {
  if (!value || typeof value !== 'object') return false;
  const c = value as Partial<BackendConnection>;
  return (
    typeof c.host === 'string' &&
    c.host.length > 0 &&
    typeof c.port === 'number' &&
    Number.isInteger(c.port) &&
    c.port > 0
  );
}

function normalizeBase(raw: string): string {
  return raw.trim().replace(/\/+$/, '');
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function defaultIsTauri(): boolean {
  return (
    typeof window !== 'undefined' &&
    Boolean((window as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__)
  );
}

async function defaultInvoke(): Promise<InvokeFn> {
  const { invoke } = await import('@tauri-apps/api/core');
  return (command: string) => invoke<unknown>(command);
}

/**
 * Resolve the backend connection once. Subsequent calls share the first
 * resolution; use refreshBackendConnection() to re-pair after a sidecar exit.
 */
export function initBackendConnection(
  options: InitBackendConnectionOptions = {}
): Promise<BackendConnectionStatus> {
  if (initPromise) return initPromise;
  const tauri = (options.isTauri ?? defaultIsTauri)();
  initPromise = tauri ? initFromSidecar(options) : Promise.resolve(initFromBrowser());
  return initPromise;
}

async function initFromSidecar(
  options: InitBackendConnectionOptions
): Promise<BackendConnectionStatus> {
  const maxAttempts = options.maxAttempts ?? DEFAULT_MAX_ATTEMPTS;
  const intervalMs = options.intervalMs ?? DEFAULT_INTERVAL_MS;
  try {
    invokeFn = options.invoke ?? (await defaultInvoke());
  } catch {
    console.warn('[CodeFlow] Tauri invoke channel unavailable; backend unavailable');
    applyUnavailable();
    return status;
  }

  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const conn = await fetchConnectionOnce();
    if (conn === 'error') return status; // invoke failed; already unavailable
    if (conn) {
      applyReady(conn);
      return status;
    }
    if (attempt < maxAttempts) await sleep(intervalMs);
  }

  console.warn(
    `[CodeFlow] Backend handshake not ready after ${maxAttempts} attempts; backend unavailable`
  );
  applyUnavailable();
  return status;
}

/**
 * Single invoke round. Returns the connection, null while the sidecar has not
 * completed a handshake (pending), or 'error' when the invoke itself failed —
 * the payload is never logged because it may carry the process token.
 */
async function fetchConnectionOnce(): Promise<BackendConnection | null | 'error'> {
  if (!invokeFn) return 'error';
  let raw: unknown;
  try {
    raw = await invokeFn('get_backend_connection');
  } catch {
    console.warn('[CodeFlow] get_backend_connection invoke failed; backend unavailable');
    applyUnavailable();
    return 'error';
  }
  if (raw == null) return null;
  if (!isValidConnection(raw)) {
    console.warn('[CodeFlow] Malformed backend connection payload; backend unavailable');
    applyUnavailable();
    return 'error';
  }
  return raw;
}

/**
 * Re-check the pairing once (e.g. after /ready became unreachable). When the
 * sidecar exited, the Rust side has cleared the connection and this flips the
 * model to unavailable; a restarted sidecar yields its new connection.
 */
export async function refreshBackendConnection(): Promise<BackendConnectionStatus> {
  if (!invokeFn) return status; // browser mode or never initialized: nothing to re-pair
  const conn = await fetchConnectionOnce();
  if (conn === 'error') return status;
  if (conn == null) {
    if (status !== 'unavailable') {
      console.log('[CodeFlow] Backend connection cleared (sidecar exited); backend unavailable');
    }
    applyUnavailable();
    return status;
  }
  applyReady(conn);
  return status;
}

function initFromBrowser(): BackendConnectionStatus {
  const envBase = (import.meta as unknown as { env?: { VITE_API_BASE?: string } }).env
    ?.VITE_API_BASE;
  const origin =
    typeof window !== 'undefined' && /^https?:\/\//.test(window.location.origin)
      ? window.location.origin
      : '';
  const base = envBase?.trim() ? normalizeBase(envBase) : origin ? normalizeBase(origin) : null;
  if (!base) {
    applyUnavailable();
    return status;
  }
  connection = null;
  baseUrl = base;
  status = 'ready';
  console.log(`[CodeFlow] Browser mode; backend base ${base}`);
  emit();
  return status;
}
