// Realtime WebSocket client for the workbench shell (T1.12.b).
//
// One socket per project, opened lazily on that project's first subscription
// and closed when its last one goes away:
//
//     ws://{base}/api/v1/projects/{projectId}/stream
//
// The random-session stream this module used to open is gone. Its topic
// allowlist was empty (T0.08 fixed every route to an exact topic set), so each
// topic frame the workbench sent on it was silently dropped, and a random
// client id can never carry a project's authorization anyway: the project
// stream is the only route that proves which project a connection belongs to —
// which is exactly what both protocols below are scoped by.
//
// Two protocols share one project socket:
//
//   - The legacy topic protocol (subscribeTopic / WsHandler): topics such as
//     flow:project:<id> and workspace:root:<hash>. The subscription table is
//     replayed after every reconnect.
//
//   - The ordered-subscription protocol (subscribeScope / ScopeListener, §20.3):
//
//         subscribe {resource_type, resource_id, after, client_request_id}
//           → subscribed → replay_started → events → replay_finished
//           → live events
//
//     This module owns one cursor per scope — the last *applied* sequence on
//     that scope's own counter, so a project subscription (project_seq) and a
//     run subscription (run_seq) on one connection never mix (§27.4). It also
//     owns the recovery rules that make the protocol safe:
//
//       * an event at or below the cursor is a duplicate (§27.4 allows the
//         server to re-send after a resubscribe) and is dropped;
//       * an event above cursor+1 is a gap and is NEVER applied — the client
//         resubscribes from its own cursor and lets the server replay in order;
//       * an event is applied only after listener.onEvent returned, and the
//         cursor moves only then: "应用事件与保存最后已应用序号同批提交"
//         forbids persisting a cursor ahead of the render it belongs to;
//       * order is the sequence number, never occurred_at or arrival time;
//       * a reconnect resumes from the last applied cursor — never from the
//         high watermark, and never from the newest event seen;
//       * cursor_expired / invalid_cursor stop the scope and ask the listener
//         for a new cursor (a snapshot); the client never invents one.
//
// Auth (T0.08.b) is unchanged in substance and now applied per project socket:
// the browser cannot set an Authorization header on the WebSocket handshake, so
// credentials ride as subprotocols — the client offers
// [`codeflow.v1`, `codeflow.token.<token>`] (token only when the origin gate
// allows; legacy/browser mode offers just `codeflow.v1`), and the backend echoes
// back only the stable protocol. A dropped credentialed socket may be a 401
// (stale token): the connection model re-pairs once per drop (single-flight) so
// the next attempt uses fresh credentials — except for a 1013 close, which is
// the server telling a slow client to come back, not an auth rejection. A
// latched 403 stops the reconnect loop entirely until the pairing identity
// changes. In DEV+mock mode no socket is opened and the connection state reads
// as online.
import { getWsBase } from '../../api';
import { refreshBackendConnection } from './connection';
import { getAuthFailure, getTokenForUrl, onAuthFailureChange } from './authProvider';
import { useShellStore } from '../stores/shell';
import { isDevMockActive } from '../lib/devMock';
import type { ExecutionEvent } from '../../generated/openapi-types';

export interface WsFrame {
  type: string;
  session_id?: string;
  agent_id?: string;
  content?: string;
  data?: Record<string, unknown>;
  timestamp?: number;
}

export type WsHandler = (frame: WsFrame) => void;
export type WsStatusHandler = (connected: boolean) => void;

/** The resource an ordered subscription follows (§20.3's closed enum). */
export type ScopeRef = { resourceType: 'project' | 'run'; resourceId: string };

/**
 * Where one ordered subscription is. 'subscribing' covers registration, the
 * wait for `subscribed`, and every reconnect until the server acknowledges
 * again; 'resync_required' means the server refused the cursor and only the
 * listener can supply a new one.
 */
export type ScopePhase =
  | 'subscribing'
  | 'replaying'
  | 'live'
  | 'unavailable'
  | 'resync_required'
  | 'forbidden'
  | 'failed';

export interface ScopeListener {
  /** Apply one event synchronously. The cursor advances only after this returns. */
  onEvent(event: ExecutionEvent): void;
  /**
   * The cursor this scope pointed at is unusable: ask the owner of the cache
   * for the cursor a fresh snapshot carries. Returning a rejected promise (or
   * throwing) leaves the scope parked in 'resync_required' — the client never
   * guesses a cursor, and in particular never adopts high_watermark, which
   * belongs to a different timeline than the local cache.
   */
  onResync(info: {
    reason: 'cursor_expired' | 'invalid_cursor';
    retentionFloor?: number;
    highWatermark?: number;
  }): number | Promise<number>;
  onPhase?(phase: ScopePhase): void;
}

const BACKOFF_MIN_MS = 1000;
const BACKOFF_MAX_MS = 16000;
/** Stable subprotocol, mirrored from backend middleware.WebSocketProtocolV1. */
const WS_PROTOCOL_V1 = 'codeflow.v1';
/** Token-bearing subprotocol tag, mirrored from backend middleware. */
const WS_TOKEN_PROTOCOL_TAG = 'codeflow.token.';
/** The close code a server uses to shed a slow consumer (ws.CloseTryAgainLater). */
const SLOW_CONSUMER_CLOSE_CODE = 1013;

const FRAME_SUBSCRIBED = 'subscribed';
const FRAME_REPLAY_STARTED = 'replay_started';
const FRAME_EVENT = 'event';
const FRAME_REPLAY_FINISHED = 'replay_finished';
const FRAME_CURSOR_EXPIRED = 'cursor_expired';
const FRAME_INVALID_CURSOR = 'invalid_cursor';
const FRAME_FORBIDDEN = 'forbidden';
const FRAME_UNAVAILABLE = 'unavailable';
const FRAME_INVALID_REQUEST = 'invalid_request';

/**
 * Server frame types of the ordered-subscription protocol. They are never
 * dispatched to legacy topic handlers: a topic literally named "event" would
 * otherwise receive every ExecutionEvent of the connection.
 */
const ORDERED_FRAME_TYPES = new Set<string>([
  FRAME_SUBSCRIBED,
  FRAME_REPLAY_STARTED,
  FRAME_EVENT,
  FRAME_REPLAY_FINISHED,
  FRAME_CURSOR_EXPIRED,
  FRAME_INVALID_CURSOR,
  FRAME_FORBIDDEN,
  FRAME_UNAVAILABLE,
  FRAME_INVALID_REQUEST,
]);

/** One ordered subscription of one project connection. */
interface ScopeState {
  /** "project:<id>" / "run:<id>" — the wire scope the frames carry. */
  key: string;
  scope: ScopeRef;
  listener: ScopeListener;
  /** Last applied sequence; every gate in this file compares against it. */
  cursor: number;
  phase: ScopePhase;
  /** Correlation id of the subscribe frame currently outstanding, if any. */
  requestId: string | null;
  /** Retry backoff of this scope alone (unavailable / a listener that threw). */
  backoff: number;
  retryTimer: ReturnType<typeof setTimeout> | null;
}

/** One project's socket and everything it carries. */
interface ProjectConn {
  projectId: string;
  socket: WebSocket | null;
  backoff: number;
  reconnectTimer: ReturnType<typeof setTimeout> | null;
  topics: Map<string, Set<WsHandler>>;
  scopes: Map<string, ScopeState>;
}

const conns = new Map<string, ProjectConn>();
const statusHandlers = new Set<WsStatusHandler>();
let connected = false;
let requestSeq = 0;
let rebindInFlight: Promise<void> | null = null;

function nextRequestId(): string {
  requestSeq += 1;
  return `sub-${requestSeq}`;
}

function scopeKey(scope: ScopeRef): string {
  return `${scope.resourceType}:${scope.resourceId}`;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function projectStreamUrl(projectId: string): string {
  return `${getWsBase()}/api/v1/projects/${encodeURIComponent(projectId)}/stream`;
}

/**
 * The hub broadcasts the same frame on every matching topic without a topic
 * field, so delivery is re-derived from frame content: global topics match by
 * type; per-project flow topics additionally match data.project_id; per-root
 * workspace topics accept any workspace_event (handlers filter by data.root
 * when they watch several roots — the workbench only ever watches one).
 */
function topicMatches(topic: string, frame: WsFrame): boolean {
  if (topic === frame.type) return true;
  if (topic.startsWith('flow:project:')) {
    return frame.type === 'flow_event' && frame.data?.project_id === topic.slice('flow:project:'.length);
  }
  if (topic.startsWith('workspace:root:')) {
    return frame.type === 'workspace_event';
  }
  return false;
}

function setConnected(v: boolean): void {
  if (connected === v) return;
  connected = v;
  useShellStore.getState().setWsConnected(v);
  for (const cb of statusHandlers) cb(v);
}

/**
 * `wsConnected` is an aggregate now: it reads true while at least one project
 * socket is OPEN. Mock mode has no socket at all and still reads online, which
 * is what the chrome did before this module owned several connections.
 */
function updateStatus(): void {
  let next = false;
  if (conns.size > 0) {
    if (isDevMockActive()) {
      next = true;
    } else {
      for (const pc of conns.values()) {
        if (pc.socket?.readyState === WebSocket.OPEN) {
          next = true;
          break;
        }
      }
    }
  }
  setConnected(next);
}

function sendFrame(pc: ProjectConn, frame: Record<string, unknown>): void {
  if (pc.socket?.readyState === WebSocket.OPEN) {
    pc.socket.send(JSON.stringify(frame));
  }
}

function clearTimer(timer: ReturnType<typeof setTimeout> | null): null {
  if (timer != null) clearTimeout(timer);
  return null;
}

function emitPhase(st: ScopeState, phase: ScopePhase): void {
  try {
    st.listener.onPhase?.(phase);
  } catch {
    /* a diagnostic callback must not take the socket down */
  }
}

function setPhase(st: ScopeState, phase: ScopePhase): void {
  if (st.phase === phase) return;
  st.phase = phase;
  emitPhase(st, phase);
}

/** Scope states that must not be re-subscribed automatically, whatever happens. */
function isTerminalPhase(phase: ScopePhase): boolean {
  return phase === 'forbidden' || phase === 'failed';
}

/**
 * Send one subscribe frame for this scope from its current cursor, with a fresh
 * client_request_id. Everything else about the scope is untouched, so a retry
 * after `unavailable` keeps its cursor and its phase until the server answers.
 */
function sendScopeSubscribe(pc: ProjectConn, st: ScopeState): void {
  if (pc.socket?.readyState !== WebSocket.OPEN) return;
  st.requestId = nextRequestId();
  sendFrame(pc, {
    type: 'subscribe',
    data: {
      resource_type: st.scope.resourceType,
      resource_id: st.scope.resourceId,
      after: st.cursor,
      client_request_id: st.requestId,
    },
  });
}

/**
 * Re-subscribe now (a gap, a replay that fell short, or a resync that just
 * produced a cursor). Cancelling a pending retry first is what keeps the
 * immediate path immediate.
 */
function resubscribeScopeNow(pc: ProjectConn, st: ScopeState): void {
  if (pc.scopes.get(st.key) !== st) return; // unsubscribed meanwhile
  st.retryTimer = clearTimer(st.retryTimer);
  setPhase(st, 'subscribing');
  sendScopeSubscribe(pc, st);
}

/**
 * Re-subscribe this scope after a delay, backing off 1s → 16s on its own
 * counter (reset when the scope catches up and goes live). A pending retry is
 * left alone: two timers would only double the subscribe frames the server has
 * to answer.
 */
function scheduleScopeRetry(pc: ProjectConn, st: ScopeState, delayMs: number): void {
  if (st.retryTimer != null) return;
  if (pc.scopes.get(st.key) !== st) return;
  if (getAuthFailure() === 'forbidden') return;
  const wait = delayMs > 0 ? delayMs : st.backoff;
  if (delayMs <= 0) st.backoff = Math.min(st.backoff * 2, BACKOFF_MAX_MS);
  st.retryTimer = setTimeout(() => {
    st.retryTimer = null;
    if (pc.scopes.get(st.key) !== st) return;
    sendScopeSubscribe(pc, st);
  }, wait);
}

/** Find the scope a control frame's client_request_id belongs to, if any. */
function matchScope(pc: ProjectConn, data: Record<string, unknown>): ScopeState | null {
  const requestId = data.client_request_id;
  if (typeof requestId !== 'string' || requestId === '') return null;
  for (const st of pc.scopes.values()) {
    if (st.requestId === requestId) {
      const scope = data.scope;
      if (typeof scope === 'string' && scope !== st.key) return null; // acked another scope
      return st;
    }
  }
  return null;
}

/** Minimal shape check: an event is applied only if its identity is usable. */
function asExecutionEvent(data: Record<string, unknown>): ExecutionEvent | null {
  if (typeof data.id !== 'string' || data.id === '') return null;
  if (typeof data.scope !== 'string' || data.scope === '') return null;
  if (typeof data.sequence !== 'number' || !Number.isInteger(data.sequence)) return null;
  return data as unknown as ExecutionEvent;
}

/**
 * Apply one event of one scope, or refuse to.
 *
 * The three-way split is §27.4 in code: at or below the cursor is a duplicate
 * (drop, the cursor already covers it), cursor+1 is the next event (apply, then
 * advance), anything above is a gap (never apply — resubscribe and let the
 * server replay the missing ones in order).
 */
function applyEventFrame(pc: ProjectConn, st: ScopeState, event: ExecutionEvent): void {
  // Not consuming: the cursor is unusable until the listener supplies a new one
  // (resync_required), or the server will not serve this scope again. Late
  // frames of an earlier subscription are moot either way.
  if (st.phase === 'resync_required' || isTerminalPhase(st.phase)) return;
  const sequence = event.sequence;
  if (sequence <= st.cursor) return; // duplicate: already applied
  // The listener refused an earlier event and a retry is pending: everything
  // else this subscription delivers lies beyond the refused sequence. Chasing it
  // as a gap would cancel the backoff and replay at network speed.
  if (st.retryTimer != null) return;
  if (sequence > st.cursor + 1) {
    // A resubscribe is already outstanding: its replay covers this gap too.
    if (st.phase === 'subscribing' && st.requestId !== null) return;
    // A gap. The missing sequences are not recoverable from what is in hand, and
    // applying out of order would hand the listener a stream with a hole in it.
    logWarn(`scope ${st.key} gap at ${sequence} (cursor ${st.cursor}); replaying from the cursor`);
    resubscribeScopeNow(pc, st);
    return;
  }
  try {
    st.listener.onEvent(event);
  } catch (err) {
    // The listener refused the event, so the cursor must not cover it: keep the
    // cursor where it is and ask the server for the same sequence again.
    logWarn(`scope ${st.key} listener threw on sequence ${sequence}: ${errText(err)}`);
    scheduleScopeRetry(pc, st, 0);
    return;
  }
  // Only now, after the listener returned, is the sequence "applied".
  st.cursor = sequence;
}

/**
 * The server refused the cursor (410 cursor_expired / 422 invalid_cursor). Both
 * end the subscription, so the scope stops until a new cursor arrives from the
 * listener; the frame's numbers are handed over untouched for the cache owner
 * to act on (retention_floor for "how much history is left", high_watermark for
 * "how far this timeline goes").
 */
function handleCursorFrame(
  pc: ProjectConn,
  st: ScopeState,
  reason: 'cursor_expired' | 'invalid_cursor',
  info: { retentionFloor?: number; highWatermark?: number },
): void {
  setPhase(st, 'resync_required');
  let result: number | Promise<number>;
  try {
    result = st.listener.onResync({ reason, ...info });
  } catch (err) {
    logWarn(`scope ${st.key} resync for ${reason} failed: ${errText(err)}`);
    return; // parked in resync_required: no retry loop on a cursor we may not invent
  }
  const adopt = (next: number): void => {
    if (pc.scopes.get(st.key) !== st) return;
    if (!Number.isInteger(next) || next < 0) {
      logWarn(`scope ${st.key} resync returned a cursor that is not a non-negative integer`);
      return;
    }
    st.cursor = next;
    resubscribeScopeNow(pc, st);
  };
  if (typeof result === 'number') {
    adopt(result);
    return;
  }
  Promise.resolve(result).then(adopt, (err: unknown) => {
    logWarn(`scope ${st.key} resync for ${reason} rejected: ${errText(err)}`);
  });
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Never log a payload or an event body — only the fixed words of the protocol. */
function logWarn(message: string): void {
  console.warn(`[ws] ${message}`);
}

/** Route one server frame of the ordered-subscription protocol. */
function handleOrderedFrame(pc: ProjectConn, frame: WsFrame): void {
  const data = isObject(frame.data) ? frame.data : {};
  switch (frame.type) {
    case FRAME_EVENT: {
      const event = asExecutionEvent(data);
      if (!event) return;
      const st = pc.scopes.get(event.scope);
      if (!st) return; // no such subscription on this connection: drop
      applyEventFrame(pc, st, event);
      return;
    }
    case FRAME_SUBSCRIBED: {
      const st = matchScope(pc, data);
      if (!st) return;
      // An acknowledged subscribe is not progress yet: resetting the backoff here
      // would retry a listener that keeps refusing one event at the shortest wait
      // forever. It resets once the replay caught up (replay_finished).
      setPhase(st, 'subscribing');
      return;
    }
    case FRAME_REPLAY_STARTED: {
      const st = matchScope(pc, data);
      if (!st) return;
      setPhase(st, 'replaying');
      return;
    }
    case FRAME_REPLAY_FINISHED: {
      const st = matchScope(pc, data);
      if (!st) return;
      // The listener refused an event of this replay and its retry is pending:
      // falling short is expected, and the retry resumes from the cursor on its
      // own schedule.
      if (st.retryTimer != null) return;
      const nextAfter = data.next_after;
      if (typeof nextAfter === 'number' && st.cursor < nextAfter) {
        // The replay claims to have delivered events this client never applied:
        // a silent gap in the making. Replay it again rather than switch to live.
        logWarn(`scope ${st.key} replay ended at ${nextAfter} but cursor is ${st.cursor}; replaying`);
        resubscribeScopeNow(pc, st);
        return;
      }
      st.backoff = BACKOFF_MIN_MS; // caught up: the next trouble starts from the shortest wait
      setPhase(st, 'live');
      return;
    }
    case FRAME_CURSOR_EXPIRED: {
      const st = matchScope(pc, data);
      if (!st) return;
      handleCursorFrame(pc, st, 'cursor_expired', {
        retentionFloor: typeof data.retention_floor === 'number' ? data.retention_floor : undefined,
      });
      return;
    }
    case FRAME_INVALID_CURSOR: {
      const st = matchScope(pc, data);
      if (!st) return;
      // high_watermark is reported, never adopted: the local cache sits on a
      // different timeline and only a snapshot can move the cursor.
      handleCursorFrame(pc, st, 'invalid_cursor', {
        highWatermark: typeof data.high_watermark === 'number' ? data.high_watermark : undefined,
      });
      return;
    }
    case FRAME_UNAVAILABLE: {
      const st = matchScope(pc, data);
      if (!st) return;
      setPhase(st, 'unavailable');
      // The socket is healthy — the server cannot serve this subscription yet
      // (T1.04 wires the store). Keep the cursor and retry on this scope's own
      // schedule, without touching the socket or the other scopes.
      scheduleScopeRetry(pc, st, 0);
      return;
    }
    case FRAME_FORBIDDEN: {
      const st = matchScope(pc, data);
      if (!st) return;
      setPhase(st, 'forbidden');
      return; // never retried: the resource is not this connection's to follow
    }
    case FRAME_INVALID_REQUEST: {
      const st = matchScope(pc, data);
      if (!st) return;
      setPhase(st, 'failed');
      const reason = typeof data.reason === 'string' ? data.reason : 'unknown';
      logWarn(`scope ${st.key} subscribe frame rejected: ${reason}`);
      return;
    }
    default:
      return;
  }
}

function handleMessage(pc: ProjectConn, raw: unknown): void {
  let frame: WsFrame;
  try {
    frame = JSON.parse(String(raw)) as WsFrame;
  } catch {
    return;
  }
  if (!frame || typeof frame.type !== 'string') return;
  if (frame.type === 'ping') {
    sendFrame(pc, { type: 'pong', timestamp: Date.now() });
    return;
  }
  if (ORDERED_FRAME_TYPES.has(frame.type)) {
    handleOrderedFrame(pc, frame);
    return;
  }
  for (const [topic, set] of pc.topics) {
    if (topicMatches(topic, frame)) {
      for (const handler of set) handler(frame);
    }
  }
}

/**
 * A dropped credentialed socket may be an auth rejection (the browser cannot
 * see the handshake status): re-pair once so a rotated sidecar token is picked
 * up before the next attempt. Single-flight; no-op outside Tauri mode.
 */
function rebindAfterDrop(): void {
  if (rebindInFlight) return;
  rebindInFlight = Promise.resolve(refreshBackendConnection())
    .then(
      () => undefined,
      () => undefined,
    )
    .finally(() => {
      rebindInFlight = null;
    });
}

/** The subprotocol offer for one connection attempt: stable + token when gated in. */
function wsProtocols(url: string): string[] {
  const token = getTokenForUrl(url);
  return token ? [WS_PROTOCOL_V1, `${WS_TOKEN_PROTOCOL_TAG}${token}`] : [WS_PROTOCOL_V1];
}

function isEmpty(pc: ProjectConn): boolean {
  return pc.topics.size === 0 && pc.scopes.size === 0;
}

function scheduleReconnect(pc: ProjectConn): void {
  if (pc.reconnectTimer != null) return;
  if (isEmpty(pc)) return; // the last subscription left on purpose
  if (getAuthFailure() === 'forbidden') return; // permission lock: no storm
  const wait = pc.backoff;
  pc.backoff = Math.min(pc.backoff * 2, BACKOFF_MAX_MS);
  pc.reconnectTimer = setTimeout(() => {
    pc.reconnectTimer = null;
    connectProject(pc);
  }, wait);
}

/** Replay the whole subscription table so a reconnect is transparent. */
function replaySubscriptions(pc: ProjectConn): void {
  for (const topic of pc.topics.keys()) {
    sendFrame(pc, { type: 'subscribe', data: { topic } });
  }
  for (const st of pc.scopes.values()) {
    // A scope on its own retry timer (unavailable, a listener that threw) is
    // waiting for a moment, not for a socket: its timer re-subscribes it. A
    // scope parked in resync_required has no valid position to resume from
    // until the listener supplies one.
    if (isTerminalPhase(st.phase)) continue;
    if (st.retryTimer != null) continue;
    if (st.phase === 'resync_required') continue;
    setPhase(st, 'subscribing');
    sendScopeSubscribe(pc, st);
  }
}

function connectProject(pc: ProjectConn): void {
  if (getAuthFailure() === 'forbidden') return; // permission lock: stay down
  if (isEmpty(pc)) return;
  if (pc.socket && (pc.socket.readyState === WebSocket.OPEN || pc.socket.readyState === WebSocket.CONNECTING)) {
    return;
  }
  if (isDevMockActive()) {
    // Mock mode: no socket. Report online so the chrome reflects dev reality.
    updateStatus();
    return;
  }
  let ws: WebSocket;
  let offeredToken: boolean;
  try {
    const url = projectStreamUrl(pc.projectId);
    offeredToken = getTokenForUrl(url) !== null;
    ws = new WebSocket(url, wsProtocols(url));
  } catch {
    scheduleReconnect(pc);
    return;
  }
  pc.socket = ws;

  ws.onopen = () => {
    pc.backoff = BACKOFF_MIN_MS;
    updateStatus();
    replaySubscriptions(pc);
  };

  // The event argument is optional on purpose: a close may arrive without one
  // (the test fake and, per spec, some implementations omit it), and a missing
  // code must not be read as "not a slow consumer".
  ws.onclose = (ev?: { code?: number }) => {
    if (pc.socket === ws) pc.socket = null;
    updateStatus();
    // Every scope falls back to 'subscribing': after the reconnect each one is
    // re-subscribed from its own last applied cursor. Terminal scopes stay put,
    // and a scope waiting for a resync cursor keeps waiting for it.
    for (const st of pc.scopes.values()) {
      st.retryTimer = clearTimer(st.retryTimer);
      if (isTerminalPhase(st.phase) || st.phase === 'resync_required') continue;
      setPhase(st, 'subscribing');
    }
    if (isEmpty(pc)) return; // an intentional close
    if (getAuthFailure() === 'forbidden') return; // permission lock: stay down
    const code = ev && typeof ev.code === 'number' ? ev.code : undefined;
    if (code === SLOW_CONSUMER_CLOSE_CODE) {
      // 1013 (Try Again Later) is the server shedding a slow client, not an auth
      // rejection: come back promptly and do not re-pair the connection.
      pc.backoff = BACKOFF_MIN_MS;
    }
    scheduleReconnect(pc);
    if (code !== SLOW_CONSUMER_CLOSE_CODE && offeredToken) rebindAfterDrop();
  };

  ws.onmessage = (ev) => {
    handleMessage(pc, ev.data);
  };

  ws.onerror = () => {
    ws.close();
  };
}

function ensureConn(projectId: string): ProjectConn {
  let pc = conns.get(projectId);
  if (!pc) {
    pc = {
      projectId,
      socket: null,
      backoff: BACKOFF_MIN_MS,
      reconnectTimer: null,
      topics: new Map(),
      scopes: new Map(),
    };
    conns.set(projectId, pc);
  }
  return pc;
}

/** Shut a project socket down for good once nothing is subscribed on it. */
function teardownIfIdle(pc: ProjectConn): void {
  if (!isEmpty(pc)) return;
  pc.reconnectTimer = clearTimer(pc.reconnectTimer);
  for (const st of pc.scopes.values()) st.retryTimer = clearTimer(st.retryTimer);
  const current = pc.socket;
  pc.socket = null;
  if (current) {
    current.onopen = null;
    current.onclose = null; // its close is intentional, not a drop
    current.onerror = null;
    current.onmessage = null;
    if (current.readyState === WebSocket.OPEN || current.readyState === WebSocket.CONNECTING) {
      current.close();
    }
  }
  conns.delete(pc.projectId);
  updateStatus();
}

function requireProjectId(projectId: string): void {
  if (typeof projectId !== 'string' || projectId === '') {
    throw new Error('ws: a project id is required for every subscription');
  }
}

/**
 * Subscribe a handler to a legacy hub topic of projectId's stream; returns the
 * matching unsubscribe function. The project's socket opens lazily on its first
 * subscription and closes when the last one on that project is removed.
 */
export function subscribeTopic(projectId: string, topic: string, handler: WsHandler): () => void {
  requireProjectId(projectId);
  const pc = ensureConn(projectId);
  const set = pc.topics.get(topic) ?? new Set<WsHandler>();
  const isNewTopic = set.size === 0;
  set.add(handler);
  pc.topics.set(topic, set);

  if (isDevMockActive()) {
    updateStatus();
  } else {
    connectProject(pc);
    if (isNewTopic) sendFrame(pc, { type: 'subscribe', data: { topic } });
  }

  return () => {
    const current = conns.get(projectId);
    if (!current) return;
    const handlers = current.topics.get(topic);
    if (!handlers) return;
    handlers.delete(handler);
    if (handlers.size > 0) return;
    current.topics.delete(topic);
    sendFrame(current, { type: 'unsubscribe', data: { topic } });
    teardownIfIdle(current);
  };
}

/**
 * Follow one scope of projectId's stream with the ordered-subscription protocol
 * (§20.3). `after` is the last sequence already applied locally; the listener
 * receives every event above it, in sequence order, exactly once per scope.
 * Returns the unsubscribe function, which also stops any pending retry.
 *
 * One listener per scope per project connection: a second registration for a
 * live scope is a bug in the caller, not a subscription to merge. Once the
 * returned function ran, the scope may be subscribed again.
 */
export function subscribeScope(
  projectId: string,
  scope: ScopeRef,
  after: number,
  listener: ScopeListener,
): () => void {
  requireProjectId(projectId);
  if (scope.resourceType !== 'project' && scope.resourceType !== 'run') {
    throw new Error(`ws: unknown resource type ${String(scope.resourceType)}`);
  }
  if (typeof scope.resourceId !== 'string' || scope.resourceId === '') {
    throw new Error('ws: a scope needs a resource id');
  }
  if (!Number.isInteger(after) || after < 0) {
    throw new Error('ws: `after` must be a non-negative integer cursor');
  }
  const pc = ensureConn(projectId);
  const key = scopeKey(scope);
  if (pc.scopes.has(key)) {
    throw new Error(`ws: scope ${key} already has a listener on project ${projectId}`);
  }

  const st: ScopeState = {
    key,
    scope: { resourceType: scope.resourceType, resourceId: scope.resourceId },
    listener,
    cursor: after,
    phase: 'subscribing',
    requestId: null,
    backoff: BACKOFF_MIN_MS,
    retryTimer: null,
  };
  pc.scopes.set(key, st);
  emitPhase(st, st.phase);

  if (isDevMockActive()) {
    updateStatus();
  } else {
    connectProject(pc);
    sendScopeSubscribe(pc, st);
  }

  return () => {
    const current = conns.get(projectId);
    if (!current) return;
    if (current.scopes.get(key) !== st) return;
    current.scopes.delete(key);
    st.retryTimer = clearTimer(st.retryTimer);
    st.requestId = null; // frames still in flight for it are dropped from here on
    sendFrame(current, {
      type: 'unsubscribe',
      data: {
        resource_type: st.scope.resourceType,
        resource_id: st.scope.resourceId,
        client_request_id: nextRequestId(),
      },
    });
    teardownIfIdle(current);
  };
}

/** Read-only diagnostics: the last applied sequence of a scope, or null. */
export function getScopeCursor(projectId: string, scope: ScopeRef): number | null {
  const pc = conns.get(projectId);
  if (!pc) return null;
  const st = pc.scopes.get(scopeKey(scope));
  return st ? st.cursor : null;
}

/** Read-only diagnostics: the phase of a scope, or null when it is not followed. */
export function getScopePhase(projectId: string, scope: ScopeRef): ScopePhase | null {
  const pc = conns.get(projectId);
  if (!pc) return null;
  const st = pc.scopes.get(scopeKey(scope));
  return st ? st.phase : null;
}

/** Bring every project that still has subscriptions back up. */
function reconnectAll(): void {
  for (const pc of conns.values()) {
    if (isEmpty(pc) || pc.socket) continue;
    connectProject(pc);
  }
}

// Permission lock (403 latched anywhere on the pairing origin): drop every
// project socket, cancel every pending retry, and stop reconnecting. When the
// latch clears (the pairing identity changed), resume the projects that still
// have subscriptions.
onAuthFailureChange((failure) => {
  if (failure === 'forbidden') {
    for (const pc of conns.values()) {
      pc.backoff = BACKOFF_MIN_MS;
      pc.reconnectTimer = clearTimer(pc.reconnectTimer);
      for (const st of pc.scopes.values()) {
        st.backoff = BACKOFF_MIN_MS;
        st.retryTimer = clearTimer(st.retryTimer);
      }
      const current = pc.socket;
      pc.socket = null;
      if (current) {
        current.onclose = null; // its close is intentional, not a drop
        if (current.readyState === WebSocket.OPEN || current.readyState === WebSocket.CONNECTING) {
          current.close();
        }
      }
    }
    updateStatus();
    return;
  }
  reconnectAll();
});

/** Observe connection state; the callback fires immediately with the current value. */
export function onStatus(cb: WsStatusHandler): () => void {
  statusHandlers.add(cb);
  cb(connected);
  return () => {
    statusHandlers.delete(cb);
  };
}
