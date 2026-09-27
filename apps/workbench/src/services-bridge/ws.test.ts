// FU for the T1.12.b WebSocket client: one socket per project stream, the two
// protocols that share it, and the per-scope cursor state machine.
//
// The fake server below is the protocol, not a recording: it answers subscribe
// frames the way the backend does (subscribed → replay_started → events →
// replay_finished), it can close with a code, and it can deliver frames out of
// order, twice, or with a hole. The event fixtures carry occurred_at values in
// the opposite order of their sequences on purpose — a client that sorted by
// time instead of by sequence would fail every ordering assertion here.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { BackendConnection } from './connection';
import type { ExecutionEvent } from '../../generated/openapi-types';
import type { ScopePhase, ScopeRef, WsFrame } from './ws';

const BASE = 'http://127.0.0.1:41234';
const TOKEN = 'ws-canary-token-1f4c8b70';
const ROTATED_TOKEN = 'ws-canary-token-9a3d2e15';

const HANDSHAKE_CONN: BackendConnection = {
  host: '127.0.0.1',
  port: 41234,
  token: TOKEN,
  expires_at: 'process',
  process_start_id: 'start-123',
  legacy: false,
};

const ROTATED_CONN: BackendConnection = {
  ...HANDSHAKE_CONN,
  token: ROTATED_TOKEN,
  process_start_id: 'start-456',
};

const LEGACY_CONN: BackendConnection = { host: '127.0.0.1', port: 18081, legacy: true };

vi.mock('../mocks', () => ({
  isMockActive: () => false,
  jitter: async () => undefined,
  activateMock: () => undefined,
  showMockNotice: () => undefined,
}));

/** Invoke stub serving queued get_backend_connection responses (null when drained). */
function makeInvoke(queue: unknown[]) {
  const fn = vi.fn(async (command: string) => {
    if (command !== 'get_backend_connection') {
      throw new Error(`unexpected invoke command: ${command}`);
    }
    return queue.length > 0 ? queue.shift() : null;
  });
  return { fn };
}

function stubWindow(origin = 'http://localhost:3000', search = '') {
  vi.stubGlobal('window', { location: { origin, search } });
}

/** Minimal WebSocket stand-in: records constructor args, sends and close calls. */
class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  readonly protocols: string[];
  readyState = FakeWebSocket.CONNECTING;
  closeCalls = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onclose: ((ev?: { code?: number; reason?: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;

  constructor(url: string, protocols?: string | string[]) {
    this.url = url;
    this.protocols = Array.isArray(protocols) ? protocols : protocols ? [protocols] : [];
    FakeWebSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(String(data));
  }

  /** Client-initiated close (teardown or the 403 latch). */
  close(): void {
    this.closeCalls += 1;
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  /** Server-side handshake completion. */
  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  /** Deliver one server frame. */
  message(frame: WsFrame | string): void {
    this.onmessage?.({ data: typeof frame === 'string' ? frame : JSON.stringify(frame) });
  }

  /** Server-side drop with a close code (1013 = slow consumer). */
  closeWith(code: number): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code });
  }

  /** Server-side drop without a close event payload (the shape the fakes used before). */
  drop(): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  /** Every frame this socket sent, parsed. */
  frames(): Array<{ type: string; data: Record<string, unknown> }> {
    return this.sent.map((raw) => JSON.parse(raw) as { type: string; data: Record<string, unknown> });
  }

  /** Ordered-subscription subscribe frames (the topic protocol has no resource_type). */
  orderedSubscribes(): Array<{ type: string; data: Record<string, unknown> }> {
    return this.frames().filter((f) => f.type === 'subscribe' && 'resource_type' in f.data);
  }

  topicSubscribes(): Array<{ type: string; data: Record<string, unknown> }> {
    return this.frames().filter((f) => f.type === 'subscribe' && 'topic' in f.data);
  }
}

function stubWebSocket(): typeof FakeWebSocket {
  FakeWebSocket.instances = [];
  vi.stubGlobal('WebSocket', FakeWebSocket);
  return FakeWebSocket;
}

/** Flush the microtask queue (resync promises are timer-free). */
async function flushMicro(rounds = 20): Promise<void> {
  for (let i = 0; i < rounds; i++) await Promise.resolve();
}

function scopeString(scope: ScopeRef): string {
  return `${scope.resourceType}:${scope.resourceId}`;
}

// --- server frame fixtures (the shapes backend/internal/websocket emits) ---

function subscribedFrame(requestId: unknown, scope: string, projectId = 'p1'): WsFrame {
  const [kind, id] = scope.split(':');
  return {
    type: 'subscribed',
    data: {
      client_request_id: requestId,
      scope,
      resource_type: kind,
      resource_id: id,
      project_id: projectId,
    },
  };
}

function replayStartedFrame(
  requestId: unknown,
  scope: string,
  after: number,
  highWatermark: number,
  retentionFloor = 1,
): WsFrame {
  return {
    type: 'replay_started',
    data: {
      client_request_id: requestId,
      scope,
      after,
      high_watermark: highWatermark,
      retention_floor: retentionFloor,
    },
  };
}

function replayFinishedFrame(
  requestId: unknown,
  scope: string,
  highWatermark: number,
  nextAfter: number,
): WsFrame {
  return {
    type: 'replay_finished',
    data: {
      client_request_id: requestId,
      scope,
      high_watermark: highWatermark,
      next_after: nextAfter,
    },
  };
}

/**
 * One ExecutionEvent as the wire carries it. occurred_at is supplied by the
 * caller so a test can invert it against the sequence.
 */
function eventFrame(
  scope: string,
  sequence: number,
  id: string,
  occurredAt: string,
  payload: Record<string, unknown> = {},
): WsFrame {
  const [kind, resourceId] = scope.split(':');
  const event: ExecutionEvent = {
    id,
    scope,
    project_id: 'p1',
    run_id: kind === 'run' ? resourceId : null,
    sequence,
    type: 'run.completed',
    schema_version: 1,
    occurred_at: occurredAt,
    identity: { project_id: 'p1', actor: { type: 'system', id: 'codeflow' } },
    payload,
  };
  return { type: 'event', data: event as unknown as Record<string, unknown> };
}

/**
 * The server side of one project socket: it answers the ordered subscribe frames
 * the client sent, in order, exactly once each.
 */
class FakeServer {
  private answered = 0;

  constructor(
    private readonly ws: FakeWebSocket,
    private readonly opts: { highWatermark?: number; retentionFloor?: number; projectId?: string } = {},
  ) {}

  get highWatermark(): number {
    return this.opts.highWatermark ?? 100;
  }

  /** Answer every not-yet-answered ordered subscribe frame. */
  answer(): void {
    const subs = this.ws.orderedSubscribes();
    for (; this.answered < subs.length; this.answered++) {
      const { data } = subs[this.answered];
      const scope = `${String(data.resource_type)}:${String(data.resource_id)}`;
      this.ws.message(subscribedFrame(data.client_request_id, scope, this.opts.projectId ?? 'p1'));
      this.ws.message(
        replayStartedFrame(
          data.client_request_id,
          scope,
          Number(data.after),
          this.highWatermark,
          this.opts.retentionFloor ?? 1,
        ),
      );
    }
  }

  /** Acknowledge the newest subscribe and close its replay at nextAfter. */
  finishReplay(nextAfter: number): void {
    const subs = this.ws.orderedSubscribes();
    const last = subs[subs.length - 1];
    const scope = `${String(last.data.resource_type)}:${String(last.data.resource_id)}`;
    this.ws.message(replayFinishedFrame(last.data.client_request_id, scope, this.highWatermark, nextAfter));
  }

  /** The newest ordered subscribe frame, or null. */
  lastSubscribe(): { type: string; data: Record<string, unknown> } | null {
    const subs = this.ws.orderedSubscribes();
    return subs.length > 0 ? subs[subs.length - 1] : null;
  }

  subscribeCount(): number {
    return this.ws.orderedSubscribes().length;
  }
}

/** A listener that records everything it is handed. */
function recorder() {
  const applied: ExecutionEvent[] = [];
  const phases: ScopePhase[] = [];
  const resyncs: Array<{ reason: string; retentionFloor?: number; highWatermark?: number }> = [];
  const state = { throwOn: null as string | null, resyncResult: undefined as number | Promise<number> | undefined };
  return {
    applied,
    phases,
    resyncs,
    state,
    listener: {
      onEvent(event: ExecutionEvent) {
        if (state.throwOn === event.id) throw new Error(`listener refused ${event.id}`);
        applied.push(event);
      },
      onResync(info: { reason: string; retentionFloor?: number; highWatermark?: number }) {
        resyncs.push(info);
        if (state.resyncResult === undefined) throw new Error('no resync cursor configured');
        return state.resyncResult;
      },
      onPhase(phase: ScopePhase) {
        phases.push(phase);
      },
    },
  };
}

async function freshModules() {
  vi.resetModules();
  const connection = await import('./connection');
  const auth = await import('./authProvider');
  const shell = await import('../stores/shell');
  return { connection, auth, shell };
}

/** Fresh module graph, ready Tauri pairing, and an installed WebSocket fake. */
async function readyWs(queue: unknown[] = [HANDSHAKE_CONN]) {
  const mods = await freshModules();
  const invoke = makeInvoke(queue);
  await mods.connection.initBackendConnection({ invoke: invoke.fn, isTauri: () => true, intervalMs: 0 });
  const fakeWs = stubWebSocket();
  const ws = await import('./ws');
  return { ...mods, invoke, fakeWs, ws, queue };
}

const PROJECT_P1: ScopeRef = { resourceType: 'project', resourceId: 'p1' };
const RUN_R1: ScopeRef = { resourceType: 'run', resourceId: 'r1' };

beforeEach(() => {
  stubWindow();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('T1.12.b ws — project streams', () => {
  it('ProjectStreamUrlPerProject: 每个项目一条项目流 socket，绝不出现会话路径', async () => {
    const { ws, fakeWs } = await readyWs();

    const offA = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const offB = ws.subscribeTopic('p2', 'flow:project:p2', () => undefined);

    expect(fakeWs.instances).toHaveLength(2);
    expect(fakeWs.instances[0].url).toMatch(/^ws:\/\/127\.0\.0\.1:41234\/api\/v1\/projects\/p1\/stream$/);
    expect(fakeWs.instances[1].url).toMatch(/^ws:\/\/127\.0\.0\.1:41234\/api\/v1\/projects\/p2\/stream$/);
    for (const inst of fakeWs.instances) {
      expect(inst.url).not.toContain('/conversations/');
      expect(inst.url).not.toContain('wb-');
    }

    // Project ids are URL-encoded, so a project id can never forge a path.
    offA();
    const offC = ws.subscribeTopic('p 3/../x', 't', () => undefined);
    expect(fakeWs.instances[2].url).toBe(
      'ws://127.0.0.1:41234/api/v1/projects/p%203%2F..%2Fx/stream',
    );
    offB();
    offC();
  });

  it('LastSubscriptionClosesSocketWithoutReconnect: 项目最后一个订阅移除即关 socket 且不重连', async () => {
    const { ws, fakeWs } = await readyWs();
    const offTopic = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const offScope = ws.subscribeScope('p1', PROJECT_P1, 0, recorder().listener);

    expect(fakeWs.instances).toHaveLength(1); // one socket for both subscriptions
    const sock = fakeWs.instances[0];
    sock.open();
    expect(sock.orderedSubscribes()).toHaveLength(1);
    expect(sock.topicSubscribes()).toHaveLength(1);

    vi.useFakeTimers();
    offTopic(); // still one subscription left on this project
    expect(sock.closeCalls).toBe(0);
    expect(sock.readyState).toBe(FakeWebSocket.OPEN);

    offScope(); // the last one: the socket goes away for good
    expect(sock.closeCalls).toBe(1);
    expect(fakeWs.instances).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fakeWs.instances).toHaveLength(1); // no reconnect
  });

  it('StatusAggregatesProjectSockets: wsConnected 反映至少一条项目 socket 为 OPEN', async () => {
    const { ws, fakeWs, shell } = await readyWs();
    const seen: boolean[] = [];
    const offStatus = ws.onStatus((v) => seen.push(v));

    const offA = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const offB = ws.subscribeTopic('p2', 'flow:project:p2', () => undefined);
    expect(shell.useShellStore.getState().wsConnected).toBe(false);

    fakeWs.instances[0].open();
    expect(shell.useShellStore.getState().wsConnected).toBe(true);

    fakeWs.instances[1].open();
    fakeWs.instances[0].closeWith(1013); // one down, the other still up
    expect(shell.useShellStore.getState().wsConnected).toBe(true);

    offA();
    expect(shell.useShellStore.getState().wsConnected).toBe(true); // p2 is still open
    offB();
    expect(shell.useShellStore.getState().wsConnected).toBe(false);
    expect(seen).toEqual([false, true, false]);
    offStatus();
  });

  it('MockModeOpensNoSocketAndReportsOnline: DEV mock 不开 socket 且状态在线', async () => {
    vi.resetModules();
    stubWindow('http://localhost:3000', '?mock=1');
    const mods = await freshModules();
    await mods.connection.initBackendConnection({ isTauri: () => false });
    const fakeWs = stubWebSocket();
    const ws = await import('./ws');

    const off = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const offScope = ws.subscribeScope('p1', PROJECT_P1, 0, recorder().listener);
    expect(fakeWs.instances).toHaveLength(0);
    expect(mods.shell.useShellStore.getState().wsConnected).toBe(true);

    off();
    offScope();
    expect(fakeWs.instances).toHaveLength(0);
  });
});

describe('T1.12.b ws — legacy topics on a project stream', () => {
  it('TopicProtocolFramesAndDispatch: 订阅帧为 topic 形状，按项目连接内分发', async () => {
    const { ws, fakeWs } = await readyWs();
    const flowFrames: WsFrame[] = [];
    const workspaceFrames: WsFrame[] = [];
    const offFlow = ws.subscribeTopic('p1', 'flow:project:p1', (f) => flowFrames.push(f));
    const sock = fakeWs.instances[0];
    sock.open();

    expect(sock.frames()).toEqual([{ type: 'subscribe', data: { topic: 'flow:project:p1' } }]);

    sock.message({ type: 'flow_event', data: { project_id: 'p1', event_type: 'stage.started' } });
    expect(flowFrames).toHaveLength(1);
    // Another project's flow event never reaches this connection's handler.
    sock.message({ type: 'flow_event', data: { project_id: 'p2', event_type: 'stage.started' } });
    expect(flowFrames).toHaveLength(1);

    const offWorkspace = ws.subscribeTopic('p1', 'workspace:root:abc', (f) => workspaceFrames.push(f));
    expect(sock.topicSubscribes()).toHaveLength(2);
    sock.message({ type: 'workspace_event', data: { root: 'D:/w', path: 'a.ts', change: 'modified' } });
    expect(workspaceFrames).toHaveLength(1);
    expect(flowFrames).toHaveLength(1); // the flow handler did not see it

    // A second handler on a topic that is already subscribed sends no new frame;
    // both handlers see the frame, and unsubscribing one of them leaves the topic
    // (and the other handler) live.
    const extra: WsFrame[] = [];
    const offExtra = ws.subscribeTopic('p1', 'flow:project:p1', (f) => extra.push(f));
    expect(sock.topicSubscribes()).toHaveLength(2);
    sock.message({ type: 'flow_event', data: { project_id: 'p1' } });
    expect(flowFrames).toHaveLength(2);
    expect(extra).toHaveLength(1);
    offExtra();
    sock.message({ type: 'flow_event', data: { project_id: 'p1' } });
    expect(flowFrames).toHaveLength(3);
    expect(extra).toHaveLength(1);

    // The last handler for the topic sends the unsubscribe frame and the socket
    // stays up while another topic is still watched.
    offFlow();
    expect(sock.frames()).toContainEqual({ type: 'unsubscribe', data: { topic: 'flow:project:p1' } });
    expect(sock.closeCalls).toBe(0);

    offWorkspace();
    expect(sock.closeCalls).toBe(1);
  });

  it('OrderedFrameTypesAreNotTopics: 有序订阅的控制帧不进 topic 分发', async () => {
    const { ws, fakeWs } = await readyWs();
    const seen: WsFrame[] = [];
    ws.subscribeTopic('p1', 'event', (f) => seen.push(f)); // a topic that collides with a frame type
    const sock = fakeWs.instances[0];
    sock.open();

    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:00.000Z'));
    expect(seen).toHaveLength(0);

    sock.message({ type: 'not_a_protocol_frame', data: {} });
    expect(seen).toHaveLength(0);
    sock.message({ type: 'event', data: { anything: true } }); // malformed event: dropped, not dispatched
    expect(seen).toHaveLength(0);
  });
});

describe('T1.12.b ws — ordered subscriptions', () => {
  it('SubscribeAndUnsubscribeFrameShapes: subscribe 四键、unsubscribe 三键且无 after', async () => {
    const { ws, fakeWs } = await readyWs();
    const off = ws.subscribeScope('p1', PROJECT_P1, 7, recorder().listener);
    const sock = fakeWs.instances[0];
    sock.open();

    const sub = sock.orderedSubscribes()[0];
    expect(Object.keys(sub.data).sort()).toEqual(['after', 'client_request_id', 'resource_id', 'resource_type']);
    expect(sub.type).toBe('subscribe');
    expect(sub.data.resource_type).toBe('project');
    expect(sub.data.resource_id).toBe('p1');
    expect(sub.data.after).toBe(7);
    expect(typeof sub.data.client_request_id).toBe('string');
    expect(String(sub.data.client_request_id).length).toBeGreaterThan(0);

    off();
    const unsub = sock.frames().filter((f) => f.type === 'unsubscribe')[0];
    expect(Object.keys(unsub.data).sort()).toEqual(['client_request_id', 'resource_id', 'resource_type']);
    expect(unsub.data).not.toHaveProperty('after');
    expect(unsub.data.resource_type).toBe('project');
    expect(unsub.data.resource_id).toBe('p1');
  });

  it('ArgumentValidation: after 必须是非负整数，同一 scope 不得重复订阅', async () => {
    const { ws } = await readyWs();
    const rec = recorder();
    expect(() => ws.subscribeScope('p1', PROJECT_P1, -1, rec.listener)).toThrow();
    expect(() => ws.subscribeScope('p1', PROJECT_P1, 1.5, rec.listener)).toThrow();
    expect(() => ws.subscribeScope('p1', PROJECT_P1, Number.NaN, rec.listener)).toThrow();

    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    expect(() => ws.subscribeScope('p1', PROJECT_P1, 0, recorder().listener)).toThrow(/already/);
    // Another project, and another scope of the same project, are independent.
    const offRun = ws.subscribeScope('p1', RUN_R1, 0, recorder().listener);
    off();
    const offAgain = ws.subscribeScope('p1', PROJECT_P1, 3, rec.listener); // clean-up released it
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(3);
    offRun();
    offAgain();
  });

  it('ReplayAppliesInOrderAndDropsDuplicates: 按 sequence 应用，重复丢弃，occurred_at 不参与排序', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 3 });

    server.answer();
    expect(rec.phases).toEqual(['subscribing', 'replaying']);
    // occurred_at runs backwards against sequence: order must come from sequence.
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:03.000Z'));
    sock.message(eventFrame('project:p1', 2, 'ev-2', '2026-09-06T08:00:02.000Z'));
    server.finishReplay(2);
    sock.message(eventFrame('project:p1', 3, 'ev-3', '2026-09-06T08:00:01.000Z'));

    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1', 'ev-2', 'ev-3']);
    expect(rec.applied.map((e) => e.sequence)).toEqual([1, 2, 3]);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(3);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('live');
    expect(rec.phases).toEqual(['subscribing', 'replaying', 'live']);

    // Duplicates (the protocol allows the server to re-send after a resubscribe).
    sock.message(eventFrame('project:p1', 3, 'ev-3', '2026-09-06T08:00:01.000Z'));
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:03.000Z'));
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1', 'ev-2', 'ev-3']);
    expect(server.subscribeCount()).toBe(1); // no resubscribe: nothing was missing
    off();
  });

  it('GapIsNotAppliedAndReplaysFromCursor: 缺口不应用、从最后已应用游标重订，补发后每个 id 恰好一次', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 4 });
    server.answer();

    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:04.000Z'));
    sock.message(eventFrame('project:p1', 2, 'ev-2', '2026-09-06T08:00:03.000Z'));
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(2);

    // Sequence 4 arrives with 3 missing: never applied, and the client asks again
    // from its own cursor rather than from the event it just saw.
    sock.message(eventFrame('project:p1', 4, 'ev-4', '2026-09-06T08:00:01.000Z'));
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1', 'ev-2']);
    expect(server.subscribeCount()).toBe(2);
    const retry = server.lastSubscribe();
    expect(retry?.data.after).toBe(2);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('subscribing');

    // The server replays: the duplicate 2 is dropped, 3 and 4 land in order.
    server.answer();
    sock.message(eventFrame('project:p1', 2, 'ev-2', '2026-09-06T08:00:03.000Z'));
    sock.message(eventFrame('project:p1', 3, 'ev-3', '2026-09-06T08:00:02.000Z'));
    sock.message(eventFrame('project:p1', 4, 'ev-4', '2026-09-06T08:00:01.000Z'));

    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1', 'ev-2', 'ev-3', 'ev-4']);
    expect(rec.applied.map((e) => e.sequence)).toEqual([1, 2, 3, 4]);
    expect(new Set(rec.applied.map((e) => e.id)).size).toBe(4);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(4);
    off();
  });

  it('ReplayFinishedShortOfCursorReplaysAgain: 回放没补到游标就当作缺口重订', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 2 });
    server.answer();
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:02.000Z'));

    // The server claims the replay ended at 2 while only 1 was applied.
    server.finishReplay(2);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('subscribing');
    expect(server.subscribeCount()).toBe(2);
    expect(server.lastSubscribe()?.data.after).toBe(1);

    // The honest replay switches to live.
    server.answer();
    sock.message(eventFrame('project:p1', 2, 'ev-2', '2026-09-06T08:00:01.000Z'));
    server.finishReplay(2);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('live');
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1', 'ev-2']);
    off();
  });

  it('ListenerThrowKeepsCursorAndRetries: onEvent 抛错 → 游标不动、从原游标重订', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 1 });
    server.answer();

    vi.useFakeTimers();
    rec.state.throwOn = 'ev-1';
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:01.000Z'));
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(0); // not applied, not covered
    expect(server.subscribeCount()).toBe(1); // the retry is on a backoff timer

    await vi.advanceTimersByTimeAsync(1000);
    expect(server.subscribeCount()).toBe(2);
    expect(server.lastSubscribe()?.data.after).toBe(0);

    // The retry delivers the same sequence again; this time the listener takes it.
    rec.state.throwOn = null;
    server.answer();
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:01.000Z'));
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1']);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(1);
    off();
  });

  it('PoisonEventBacksOffWithoutHotLoop: 监听器持续拒绝一条事件时，后续事件与 replay_finished 不得绕过退避', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 5 });
    // One whole replay of 1..5, the way the server sends it after every subscribe.
    const replay = () => {
      server.answer();
      for (const seq of [1, 2, 3, 4, 5]) {
        sock.message(eventFrame('project:p1', seq, `ev-${seq}`, `2026-09-06T08:00:0${6 - seq}.000Z`));
      }
      server.finishReplay(5);
    };

    vi.useFakeTimers();
    rec.state.throwOn = 'ev-2';
    replay();
    // ev-2 was refused: 3..5 of the same replay are moot (not gaps to chase), and
    // replay_finished must not turn the pending backoff into an immediate retry.
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1']);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(1);
    expect(server.subscribeCount()).toBe(1);
    await vi.advanceTimersByTimeAsync(999);
    expect(server.subscribeCount()).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(server.subscribeCount()).toBe(2);
    expect(server.lastSubscribe()?.data.after).toBe(1);

    // Still refused: the server acknowledging the subscribe is not progress, so
    // the next wait doubles instead of starting over at 1s.
    replay();
    expect(server.subscribeCount()).toBe(2);
    await vi.advanceTimersByTimeAsync(1999);
    expect(server.subscribeCount()).toBe(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(server.subscribeCount()).toBe(3);
    expect(server.lastSubscribe()?.data.after).toBe(1);

    // The listener recovers: every event lands exactly once, in order, and the
    // scope goes live.
    rec.state.throwOn = null;
    replay();
    expect(rec.applied.map((e) => e.sequence)).toEqual([1, 2, 3, 4, 5]);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(5);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('live');
    expect(server.subscribeCount()).toBe(3);
    off();
  });

  it('GapBurstResubscribesOnce: 一串缺口事件只重订一次，补发后每个 id 恰好一次', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 6 });
    server.answer();
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:06.000Z'));
    sock.message(eventFrame('project:p1', 2, 'ev-2', '2026-09-06T08:00:05.000Z'));

    // 3 is lost; 4, 5 and 6 of the same subscription are already on the wire.
    // One resubscribe from the cursor covers all of them.
    for (const seq of [4, 5, 6]) {
      sock.message(eventFrame('project:p1', seq, `ev-${seq}`, `2026-09-06T08:00:0${7 - seq}.000Z`));
    }
    expect(server.subscribeCount()).toBe(2);
    expect(server.lastSubscribe()?.data.after).toBe(2);
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1', 'ev-2']);

    server.answer();
    for (const seq of [3, 4, 5, 6]) {
      sock.message(eventFrame('project:p1', seq, `ev-${seq}`, `2026-09-06T08:00:0${7 - seq}.000Z`));
    }
    server.finishReplay(6);
    expect(rec.applied.map((e) => e.sequence)).toEqual([1, 2, 3, 4, 5, 6]);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('live');
    expect(server.subscribeCount()).toBe(2);
    off();
  });

  it('EventsWaitWhileResyncRequired: 等待新游标期间迟到的事件既不应用也不触发重订', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 5, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 60, retentionFloor: 40 });
    server.answer();

    rec.state.resyncResult = new Promise<number>(() => undefined); // the snapshot is still loading
    sock.message({
      type: 'cursor_expired',
      data: {
        client_request_id: server.lastSubscribe()?.data.client_request_id,
        scope: 'project:p1',
        code: 'cursor_expired',
        retention_floor: 40,
        recovery: 'snapshot',
      },
    });
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('resync_required');

    // A late frame for the scope: its cursor is known to be unusable, so the event
    // is neither applied on it nor chased as a gap (that would re-send the expired
    // cursor and start a second snapshot fetch).
    sock.message(eventFrame('project:p1', 6, 'ev-6', '2026-09-06T08:00:01.000Z'));
    sock.message(eventFrame('project:p1', 9, 'ev-9', '2026-09-06T08:00:00.000Z'));
    await flushMicro();
    expect(rec.applied).toHaveLength(0);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(5);
    expect(server.subscribeCount()).toBe(1);
    expect(rec.resyncs).toHaveLength(1);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('resync_required');
    off();
  });

  it('SlowConsumer1013ReconnectsFromCursorWithoutRebind: 1013 重连用最后已应用游标且不重配对', async () => {
    const { ws, fakeWs, invoke } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const first = fakeWs.instances[0];
    first.open();
    const server = new FakeServer(first, { highWatermark: 9 });
    server.answer();
    for (const seq of [1, 2, 3]) {
      first.message(eventFrame('project:p1', seq, `ev-${seq}`, '2026-09-06T08:00:00.000Z'));
    }
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(3);
    expect(invoke.fn).toHaveBeenCalledTimes(1); // only init so far

    vi.useFakeTimers();
    first.closeWith(1013); // slow consumer: the server sheds this client
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('subscribing');
    await flushMicro();
    expect(invoke.fn).toHaveBeenCalledTimes(1); // 1013 is not an auth rejection

    await vi.advanceTimersByTimeAsync(1000);
    expect(fakeWs.instances).toHaveLength(2);
    const second = fakeWs.instances[1];
    second.open();
    const resumed = second.orderedSubscribes()[0];
    expect(resumed.data.after).toBe(3); // the cursor, not high_watermark (9)
    off();
  });

  it('SlowConsumer1013OnTopicOnlySocketReconnectsToo: topic 订阅同样从表恢复', async () => {
    const { ws, fakeWs } = await readyWs();
    const off = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const first = fakeWs.instances[0];
    first.open();

    vi.useFakeTimers();
    first.closeWith(1013);
    await vi.advanceTimersByTimeAsync(1000);
    expect(fakeWs.instances).toHaveLength(2);
    fakeWs.instances[1].open();
    expect(fakeWs.instances[1].topicSubscribes()[0].data.topic).toBe('flow:project:p1');
    off();
  });

  it('CursorExpiredAsksForSnapshotCursor: cursor_expired → onResync 收到 retention_floor，从返回游标重订', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 5, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 60, retentionFloor: 40 });
    server.answer();

    rec.state.resyncResult = Promise.resolve(40); // an async snapshot fetch
    sock.message({
      type: 'cursor_expired',
      data: {
        client_request_id: server.lastSubscribe()?.data.client_request_id,
        scope: 'project:p1',
        code: 'cursor_expired',
        retention_floor: 40,
        recovery: 'snapshot',
      },
    });

    expect(rec.resyncs).toEqual([{ reason: 'cursor_expired', retentionFloor: 40 }]);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('resync_required');
    await flushMicro();
    expect(server.subscribeCount()).toBe(2);
    expect(server.lastSubscribe()?.data.after).toBe(40); // the snapshot's cursor
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(40);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('subscribing');
    off();
  });

  it('InvalidCursorReportsButNeverAdoptsHighWatermark: invalid_cursor 不自动改成 H，拒绝后停住', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 9, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 5 });
    server.answer();

    rec.state.resyncResult = Promise.reject(new Error('snapshot unavailable'));
    sock.message({
      type: 'invalid_cursor',
      data: {
        client_request_id: server.lastSubscribe()?.data.client_request_id,
        scope: 'project:p1',
        code: 'invalid_cursor',
        high_watermark: 5,
      },
    });

    expect(rec.resyncs).toEqual([{ reason: 'invalid_cursor', highWatermark: 5 }]);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(9); // never rewritten to 5
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('resync_required');

    vi.useFakeTimers();
    await flushMicro();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(server.subscribeCount()).toBe(1); // parked: no retry loop
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('resync_required');
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(9);
    off();
  });

  it('ResyncCursorIsValidated: onResync 返回非整数游标不重订', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 3, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 5 });
    server.answer();

    rec.state.resyncResult = -2 as unknown as number;
    sock.message({
      type: 'invalid_cursor',
      data: { client_request_id: server.lastSubscribe()?.data.client_request_id, scope: 'project:p1' },
    });
    await flushMicro();
    expect(server.subscribeCount()).toBe(1);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(3);
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('resync_required');
    off();
  });

  it('UnavailableRetriesSameCursorOnOwnBackoff: unavailable 同一游标退避重订、socket 不动', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 4, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 10 });
    server.answer();
    const firstRequestId = server.lastSubscribe()?.data.client_request_id;

    vi.useFakeTimers();
    sock.message({ type: 'unavailable', data: { client_request_id: firstRequestId, code: 'backend_unavailable' } });
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('unavailable');
    expect(fakeWs.instances).toHaveLength(1);
    expect(sock.readyState).toBe(FakeWebSocket.OPEN);
    expect(sock.closeCalls).toBe(0);

    await vi.advanceTimersByTimeAsync(1000);
    expect(server.subscribeCount()).toBe(2);
    expect(server.lastSubscribe()?.data.after).toBe(4);
    expect(server.lastSubscribe()?.data.client_request_id).not.toBe(firstRequestId);

    // A second unavailable backs off further (2s) and still keeps the cursor.
    sock.message({
      type: 'unavailable',
      data: { client_request_id: server.lastSubscribe()?.data.client_request_id, code: 'backend_unavailable' },
    });
    await vi.advanceTimersByTimeAsync(1000);
    expect(server.subscribeCount()).toBe(2); // not yet: the wait is 2s now
    await vi.advanceTimersByTimeAsync(1000);
    expect(server.subscribeCount()).toBe(3);
    expect(server.lastSubscribe()?.data.after).toBe(4);
    off();
  });

  it('ForbiddenStopsWithoutRetry: forbidden 不重试，也不断 socket', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 2, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 3 });
    server.answer();

    sock.message({ type: 'forbidden', data: { client_request_id: server.lastSubscribe()?.data.client_request_id } });
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('forbidden');

    vi.useFakeTimers();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(server.subscribeCount()).toBe(1);
    expect(sock.closeCalls).toBe(0);
    expect(rec.applied).toHaveLength(0);
    off();
  });

  it('InvalidRequestFailsWithoutRetryAndLogsOnlyTheReason: invalid_request 不重试、日志无 payload', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 1 });
    server.answer();

    const canary = 'payload-canary-8c71';
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:01.000Z', { secret: canary }));
    expect(rec.applied).toHaveLength(1);

    sock.message({
      type: 'invalid_request',
      data: { client_request_id: server.lastSubscribe()?.data.client_request_id, code: 'invalid_request', reason: 'invalid_after' },
    });
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBe('failed');

    vi.useFakeTimers();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(server.subscribeCount()).toBe(1);
    const logged = warn.mock.calls.map((args) => args.map(String).join(' ')).join('\n');
    expect(logged).toContain('invalid_after');
    expect(logged).not.toContain(canary); // payloads are never logged
    off();
  });

  it('ProjectAndRunScopesKeepIndependentCursors: 两个 scope 游标独立，同一 event id 各交付一次', async () => {
    const { ws, fakeWs } = await readyWs();
    const project = recorder();
    const run = recorder();
    const offProject = ws.subscribeScope('p1', PROJECT_P1, 0, project.listener);
    const offRun = ws.subscribeScope('p1', RUN_R1, 100, run.listener);
    expect(fakeWs.instances).toHaveLength(1); // one connection carries both

    const sock = fakeWs.instances[0];
    sock.open();
    const subs = sock.orderedSubscribes();
    expect(subs).toHaveLength(2);
    expect(subs[0].data).toMatchObject({ resource_type: 'project', resource_id: 'p1', after: 0 });
    expect(subs[1].data).toMatchObject({ resource_type: 'run', resource_id: 'r1', after: 100 });

    const server = new FakeServer(sock, { highWatermark: 100 });
    server.answer();

    // The same event id arrives once per scope: each listener gets its own copy
    // and each cursor moves on its own counter (dedup by id is the render layer's
    // job, T1.14).
    sock.message(eventFrame('project:p1', 1, 'ev-shared', '2026-09-06T08:00:01.000Z'));
    sock.message(eventFrame('run:r1', 101, 'ev-shared', '2026-09-06T08:00:01.000Z'));
    expect(project.applied.map((e) => e.id)).toEqual(['ev-shared']);
    expect(run.applied.map((e) => e.id)).toEqual(['ev-shared']);
    expect(project.applied[0].sequence).toBe(1);
    expect(run.applied[0].sequence).toBe(101);

    // A project event does not touch the run scope, and a run gap does not touch
    // the project scope's subscribe count.
    sock.message(eventFrame('project:p1', 2, 'ev-p2', '2026-09-06T08:00:02.000Z'));
    sock.message(eventFrame('run:r1', 103, 'ev-r3', '2026-09-06T08:00:03.000Z'));
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(2);
    expect(ws.getScopeCursor('p1', RUN_R1)).toBe(101);
    expect(run.applied.map((e) => e.id)).toEqual(['ev-shared']);

    const retry = server.lastSubscribe();
    expect(retry?.data.resource_type).toBe('run');
    expect(retry?.data.after).toBe(101); // the run cursor, not the project one
    offProject();
    offRun();
  });

  it('UnsubscribeDropsLateFramesAndClosesIdleSocket: 退订后迟到帧全丢弃，socket 关闭', async () => {
    const { ws, fakeWs } = await readyWs();
    const rec = recorder();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, rec.listener);
    const sock = fakeWs.instances[0];
    sock.open();
    const server = new FakeServer(sock, { highWatermark: 2 });
    server.answer();
    sock.message(eventFrame('project:p1', 1, 'ev-1', '2026-09-06T08:00:01.000Z'));
    expect(rec.applied).toHaveLength(1);

    off();
    expect(sock.frames().some((f) => f.type === 'unsubscribe' && 'resource_type' in f.data)).toBe(true);
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBeNull();
    expect(ws.getScopePhase('p1', PROJECT_P1)).toBeNull();

    // Frames still in flight for the scope are dropped, not delivered.
    sock.message(eventFrame('project:p1', 2, 'ev-2', '2026-09-06T08:00:02.000Z'));
    sock.message(subscribedFrame('sub-1', 'project:p1'));
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1']);

    vi.useFakeTimers();
    off(); // idempotent: no second unsubscribe frame, no second close
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fakeWs.instances).toHaveLength(1);
    expect(rec.applied.map((e) => e.id)).toEqual(['ev-1']);
  });
});

describe('T1.12.c ws — ReconnectDedupesByEventId', () => {
  it('ReconnectDedupesByEventId: 重复、乱序、缺口与 1013 重连之后，每个 listener 对每个 event id 恰好应用一次', async () => {
    const { ws, fakeWs, invoke } = await readyWs();
    const project = recorder();
    const run = recorder();
    const offProject = ws.subscribeScope('p1', PROJECT_P1, 0, project.listener);
    const offRun = ws.subscribeScope('p1', RUN_R1, 0, run.listener);
    const first = fakeWs.instances[0];
    first.open();
    // H (9) is deliberately far from both cursors, so resuming "from H" and
    // resuming from the last applied sequence cannot look the same below.
    const server1 = new FakeServer(first, { highWatermark: 9 });
    server1.answer();

    // A run event is also a project event: the same id travels on both scopes,
    // each with its own counter. occurred_at runs backwards against sequence
    // throughout, so any time-based ordering would fail the assertions below.
    first.message(eventFrame('project:p1', 1, 'ev-a', '2026-09-06T08:00:09.000Z'));
    first.message(eventFrame('project:p1', 1, 'ev-a', '2026-09-06T08:00:09.000Z')); // duplicate
    first.message(eventFrame('project:p1', 2, 'ev-b', '2026-09-06T08:00:08.000Z'));
    first.message(eventFrame('run:r1', 1, 'ev-b', '2026-09-06T08:00:08.000Z'));
    first.message(eventFrame('run:r1', 1, 'ev-b', '2026-09-06T08:00:08.000Z')); // duplicate
    // Project sequence 3 is lost on the way: 4 arrives first and is not applied.
    first.message(eventFrame('project:p1', 4, 'ev-d', '2026-09-06T08:00:06.000Z'));
    expect(project.applied.map((e) => e.id)).toEqual(['ev-a', 'ev-b']);
    const retry = server1.lastSubscribe();
    expect(retry?.data).toMatchObject({ resource_type: 'project', after: 2 });

    // The server replays the project scope from its cursor, re-sending 2.
    server1.answer();
    first.message(eventFrame('project:p1', 2, 'ev-b', '2026-09-06T08:00:08.000Z'));
    first.message(eventFrame('project:p1', 3, 'ev-c', '2026-09-06T08:00:07.000Z'));
    first.message(eventFrame('project:p1', 4, 'ev-d', '2026-09-06T08:00:06.000Z'));
    server1.finishReplay(4);
    first.message(eventFrame('run:r1', 2, 'ev-c', '2026-09-06T08:00:07.000Z'));
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(4);
    expect(ws.getScopeCursor('p1', RUN_R1)).toBe(2);

    // The server sheds the client with 1013. The reconnect resumes each scope from
    // its own last applied sequence — not from the high watermark — and does not
    // re-pair the connection.
    vi.useFakeTimers();
    first.closeWith(1013);
    await flushMicro();
    expect(invoke.fn).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(fakeWs.instances).toHaveLength(2);
    const second = fakeWs.instances[1];
    second.open();
    const resumed = second.orderedSubscribes();
    expect(resumed.find((f) => f.data.resource_type === 'project')?.data.after).toBe(4);
    expect(resumed.find((f) => f.data.resource_type === 'run')?.data.after).toBe(2);

    // The replay after the reconnect overlaps what was already applied on both
    // scopes, and adds one new event that both scopes carry.
    const server2 = new FakeServer(second, { highWatermark: 5 });
    server2.answer();
    second.message(eventFrame('project:p1', 3, 'ev-c', '2026-09-06T08:00:07.000Z'));
    second.message(eventFrame('project:p1', 4, 'ev-d', '2026-09-06T08:00:06.000Z'));
    second.message(eventFrame('project:p1', 5, 'ev-e', '2026-09-06T08:00:05.000Z'));
    second.message(eventFrame('run:r1', 2, 'ev-c', '2026-09-06T08:00:07.000Z'));
    second.message(eventFrame('run:r1', 3, 'ev-e', '2026-09-06T08:00:05.000Z'));

    expect(project.applied.map((e) => e.id)).toEqual(['ev-a', 'ev-b', 'ev-c', 'ev-d', 'ev-e']);
    expect(project.applied.map((e) => e.sequence)).toEqual([1, 2, 3, 4, 5]);
    expect(run.applied.map((e) => e.id)).toEqual(['ev-b', 'ev-c', 'ev-e']);
    expect(run.applied.map((e) => e.sequence)).toEqual([1, 2, 3]);
    for (const rec of [project, run]) {
      expect(new Set(rec.applied.map((e) => e.id)).size).toBe(rec.applied.length);
    }
    expect(ws.getScopeCursor('p1', PROJECT_P1)).toBe(5);
    expect(ws.getScopeCursor('p1', RUN_R1)).toBe(3);
    offProject();
    offRun();
  });
});

describe('T1.12.b ws — auth behaviour per project socket', () => {
  it('SubprotocolsAndUrlStayCredentialSafe: token 只走子协议，URL 无 token', async () => {
    const { ws, fakeWs } = await readyWs();
    const off = ws.subscribeScope('p1', PROJECT_P1, 0, recorder().listener);
    const sock = fakeWs.instances[0];
    expect(sock.url).toBe('ws://127.0.0.1:41234/api/v1/projects/p1/stream');
    expect(sock.url).not.toContain(TOKEN);
    expect(sock.protocols).toEqual(['codeflow.v1', `codeflow.token.${TOKEN}`]);
    off();
  });

  it('LegacyPairingOffersStableProtocolOnly: legacy 配对只提供稳定子协议', async () => {
    const { ws, fakeWs } = await readyWs([LEGACY_CONN]);
    const off = ws.subscribeTopic('p1', 't', () => undefined);
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1']);
    expect(fakeWs.instances[0].url).toBe('ws://127.0.0.1:18081/api/v1/projects/p1/stream');
    off();
  });

  it('DropRebindsOnceAndReconnectsPerProject: 掉线重配对一次，两个项目各自退避重连', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { ws, fakeWs, invoke, auth } = await readyWs(queue);
    const offA = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const offB = ws.subscribeTopic('p2', 'flow:project:p2', () => undefined);
    fakeWs.instances[0].open();
    fakeWs.instances[1].open();
    expect(invoke.fn).toHaveBeenCalledTimes(1);

    vi.useFakeTimers();
    queue.push(ROTATED_CONN);
    fakeWs.instances[0].drop(); // no close event payload: still a drop
    await flushMicro();
    expect(invoke.fn).toHaveBeenCalledTimes(2); // one single-flight re-pair
    expect(auth.getTokenForUrl(`${BASE}/api/v1/projects/p1/stream`)).toBe(ROTATED_TOKEN);

    await vi.advanceTimersByTimeAsync(1000);
    expect(fakeWs.instances).toHaveLength(3); // only p1 reconnected so far
    expect(fakeWs.instances[2].url).toContain('/projects/p1/stream');
    expect(fakeWs.instances[2].protocols).toEqual(['codeflow.v1', `codeflow.token.${ROTATED_TOKEN}`]);

    fakeWs.instances[1].drop(); // the second project reconnects on its own schedule
    await flushMicro();
    expect(invoke.fn).toHaveBeenCalledTimes(3); // its own re-pair, not one shared per drop
    await vi.advanceTimersByTimeAsync(1000);
    expect(fakeWs.instances).toHaveLength(4);
    expect(fakeWs.instances[3].url).toContain('/projects/p2/stream');
    offA();
    offB();
  });

  it('ForbiddenLatchClosesEveryProjectSocketAndResumesAfterRebind: 403 落闩关全部 socket、清定时器，解锁后恢复', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { ws, fakeWs, auth, connection } = await readyWs(queue);
    const offA = ws.subscribeTopic('p1', 'flow:project:p1', () => undefined);
    const offB = ws.subscribeScope('p2', { resourceType: 'project', resourceId: 'p2' }, 0, recorder().listener);
    fakeWs.instances[0].open();
    fakeWs.instances[1].open();
    expect(fakeWs.instances).toHaveLength(2);

    vi.useFakeTimers();
    // A 403 anywhere on the pairing origin latches: every project socket goes
    // down and no reconnect may be scheduled.
    await auth.handleAuthHttpStatus(`${BASE}/api/v1/projects`, 403);
    expect(auth.getAuthFailure()).toBe('forbidden');
    expect(fakeWs.instances[0].closeCalls).toBe(1);
    expect(fakeWs.instances[1].closeCalls).toBe(1);

    await vi.advanceTimersByTimeAsync(60_000);
    expect(fakeWs.instances).toHaveLength(2); // zero retries in 60s

    // The pairing identity changed: both projects come back with the new token.
    queue.push(ROTATED_CONN);
    await connection.refreshBackendConnection();
    await flushMicro();
    expect(auth.getAuthFailure()).toBeNull();
    expect(fakeWs.instances).toHaveLength(4);
    for (const inst of fakeWs.instances.slice(2)) {
      expect(inst.protocols).toEqual(['codeflow.v1', `codeflow.token.${ROTATED_TOKEN}`]);
    }
    expect(fakeWs.instances[2].url).toContain('/projects/p1/stream');
    expect(fakeWs.instances[3].url).toContain('/projects/p2/stream');
    offA();
    offB();
  });
});
