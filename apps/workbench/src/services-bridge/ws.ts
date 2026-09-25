// Realtime WebSocket client for the hub entry
// ws://{base}/api/v1/conversations/{clientId}/stream.
//
// The legacy services/websocket.ts targets /ws, which no longer exists in the
// backend router — this module replaces it for the new shell. One singleton
// socket carries every topic subscription; the subscription table is replayed
// after each reconnect (exponential backoff 1s → 16s). In DEV+mock mode no
// socket is opened and the connection state reads as online.
//
// Auth (T0.08.b): the browser cannot set an Authorization header on the
// WebSocket handshake, so credentials ride as subprotocols — the client
// offers [`codeflow.v1`, `codeflow.token.<token>`] (token only when the origin
// gate allows; legacy/browser mode offers just `codeflow.v1`), and the backend
// echoes back only the stable protocol. A dropped credentialed socket may be a
// 401 (stale token): the connection model re-pairs once per drop
// (single-flight) so the next attempt uses fresh credentials. A latched 403
// stops the reconnect loop entirely until the pairing identity changes.
import { getWsBase } from '../../api';
import { refreshBackendConnection } from './connection';
import { getAuthFailure, getTokenForUrl, onAuthFailureChange } from './authProvider';
import { useShellStore } from '../stores/shell';
import { isDevMockActive } from '../lib/devMock';

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

const BACKOFF_MIN_MS = 1000;
const BACKOFF_MAX_MS = 16000;
/** Stable subprotocol, mirrored from backend middleware.WebSocketProtocolV1. */
const WS_PROTOCOL_V1 = 'codeflow.v1';
/** Token-bearing subprotocol tag, mirrored from backend middleware. */
const WS_TOKEN_PROTOCOL_TAG = 'codeflow.token.';

const handlers = new Map<string, Set<WsHandler>>();
const statusHandlers = new Set<WsStatusHandler>();
const clientId = `wb-${Math.random().toString(36).slice(2, 10)}`;

let socket: WebSocket | null = null;
let connected = false;
let backoff = BACKOFF_MIN_MS;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let rebindInFlight: Promise<void> | null = null;

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

function sendFrame(frame: Record<string, unknown>): void {
  if (socket?.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(frame));
  }
}

function scheduleReconnect(): void {
  if (reconnectTimer != null || handlers.size === 0) return;
  if (getAuthFailure() === 'forbidden') return; // permission lock: no storm
  const wait = backoff;
  backoff = Math.min(backoff * 2, BACKOFF_MAX_MS);
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, wait);
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

function connect(): void {
  if (getAuthFailure() === 'forbidden') return; // permission lock: stay down
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
    return;
  }
  let ws: WebSocket;
  let offeredToken: boolean;
  try {
    const url = `${getWsBase()}/api/v1/conversations/${clientId}/stream`;
    offeredToken = getTokenForUrl(url) !== null;
    ws = new WebSocket(url, wsProtocols(url));
  } catch {
    scheduleReconnect();
    return;
  }
  socket = ws;

  ws.onopen = () => {
    backoff = BACKOFF_MIN_MS;
    setConnected(true);
    // Replay the whole subscription table so a reconnect is transparent.
    for (const topic of handlers.keys()) {
      sendFrame({ type: 'subscribe', data: { topic } });
    }
  };

  ws.onmessage = (ev) => {
    let frame: WsFrame;
    try {
      frame = JSON.parse(String(ev.data)) as WsFrame;
    } catch {
      return;
    }
    if (!frame || typeof frame.type !== 'string') return;
    if (frame.type === 'ping') {
      sendFrame({ type: 'pong', timestamp: Date.now() });
      return;
    }
    for (const [topic, set] of handlers) {
      if (topicMatches(topic, frame)) {
        for (const handler of set) handler(frame);
      }
    }
  };

  ws.onclose = () => {
    if (socket === ws) socket = null;
    setConnected(false);
    if (getAuthFailure() === 'forbidden') return; // permission lock: stay down
    scheduleReconnect();
    if (offeredToken) rebindAfterDrop();
  };

  ws.onerror = () => {
    ws.close();
  };
}

// Permission lock (403 latched anywhere on the pairing origin): drop the
// socket, cancel any pending retry, and stop reconnecting. When the latch
// clears (the pairing identity changed), resume if topics are still watched.
onAuthFailureChange((failure) => {
  if (failure === 'forbidden') {
    backoff = BACKOFF_MIN_MS;
    if (reconnectTimer != null) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    const current = socket;
    socket = null;
    if (current) {
      current.onclose = null; // its close is intentional, not a drop
      if (current.readyState === WebSocket.OPEN || current.readyState === WebSocket.CONNECTING) {
        current.close();
      }
    }
    setConnected(false);
    return;
  }
  if (!isDevMockActive() && handlers.size > 0 && !socket) connect();
});

/**
 * Subscribe a handler to a hub topic; returns the matching unsubscribe
 * function. The socket opens lazily on the first subscription.
 */
export function subscribe(topic: string, handler: WsHandler): () => void {
  const set = handlers.get(topic) ?? new Set<WsHandler>();
  const isNewTopic = set.size === 0;
  set.add(handler);
  handlers.set(topic, set);

  if (isDevMockActive()) {
    // Mock mode: no socket. Report online so the chrome reflects dev reality.
    setConnectedMock();
  } else {
    connect();
    if (isNewTopic) sendFrame({ type: 'subscribe', data: { topic } });
  }

  return () => {
    const current = handlers.get(topic);
    if (!current) return;
    current.delete(handler);
    if (current.size === 0) {
      handlers.delete(topic);
      sendFrame({ type: 'unsubscribe', data: { topic } });
    }
  };
}

function setConnectedMock(): void {
  connected = true;
  useShellStore.getState().setWsConnected(true);
  for (const cb of statusHandlers) cb(true);
}

/** Observe connection state; the callback fires immediately with the current value. */
export function onStatus(cb: WsStatusHandler): () => void {
  statusHandlers.add(cb);
  cb(connected);
  return () => {
    statusHandlers.delete(cb);
  };
}
