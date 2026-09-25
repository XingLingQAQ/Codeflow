// FU for the T0.08.b auth wiring: HTTP bearer attachment through the shared
// api client and the direct-fetch points, WS stable/token subprotocols, and
// the 401→rebind / 403→permission-latch state machine. Canary tokens prove
// the credential only ever travels in the Authorization header or the
// WebSocket subprotocol offer — never in URLs, logs, or storage.
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BackendConnection } from './connection';
import type { AgentInfo } from './agents';

const BASE = 'http://127.0.0.1:41234';
const TOKEN = 'wire-canary-token-4c91e7a2';
const ROTATED_TOKEN = 'wire-canary-token-b83f0d55';

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

/** Invoke stub that serves queued get_backend_connection responses (null when drained). */
function makeInvoke(queue: unknown[]) {
  const fn = vi.fn(async (command: string) => {
    if (command !== 'get_backend_connection') {
      throw new Error(`unexpected invoke command: ${command}`);
    }
    return queue.length > 0 ? queue.shift() : null;
  });
  return { fn };
}

async function freshModules() {
  vi.resetModules();
  const connection = await import('./connection');
  const auth = await import('./authProvider');
  const api = await import('../../api');
  const readiness = await import('./readiness');
  return { connection, auth, api, readiness };
}

async function readyWith(queue: unknown[]) {
  const mods = await freshModules();
  const invoke = makeInvoke(queue);
  await mods.connection.initBackendConnection({ invoke: invoke.fn, isTauri: () => true, intervalMs: 0 });
  return { ...mods, invoke };
}

function envelope(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const ok = (data: unknown = null) => envelope(200, { success: true, data });
const unauthorized = () => envelope(401, { success: false, error: 'unauthorized' });
const forbidden = () => envelope(403, { success: false, error: 'forbidden' });

type FetchCall = { url: string; headers: Record<string, string> };

/** fetch spy that records url + headers per call and serves queued/looped responses. */
function makeFetchSpy(respond: (callIndex: number, url: string) => Response) {
  const calls: FetchCall[] = [];
  const fn = vi.fn(async (input: unknown, init?: RequestInit) => {
    const index = calls.length;
    calls.push({
      url: String(input),
      headers: { ...((init?.headers ?? {}) as Record<string, string>) },
    });
    return respond(index, String(input));
  });
  return { fn, calls };
}

function stubWindow(origin = 'http://localhost:3000') {
  vi.stubGlobal('window', { location: { origin, search: '' } });
}

/** Minimal WebSocket stand-in: records constructor args, fires handlers on demand. */
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
  onclose: (() => void) | null = null;
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

  close(): void {
    this.closeCalls += 1;
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  /** Server-side drop. */
  drop(): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }
}

function stubWebSocket(): typeof FakeWebSocket {
  FakeWebSocket.instances = [];
  vi.stubGlobal('WebSocket', FakeWebSocket);
  return FakeWebSocket;
}

/** Flush the microtask queue (rebind chains are timer-free). */
async function flushMicro(rounds = 50): Promise<void> {
  for (let i = 0; i < rounds; i++) await Promise.resolve();
}

const CHAT_AGENT = { id: 'coder', name: 'Code Artisan', role_base: 'coder' } as AgentInfo;

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('T0.08.b auth wiring — HTTP bearer', () => {
  it('SameOriginHttpCarriesBearer: 同源请求带 Authorization: Bearer', async () => {
    const { api } = await readyWith([HANDSHAKE_CONN]);
    const fetchSpy = makeFetchSpy(() => ok({ id: 'p1' }));
    vi.stubGlobal('fetch', fetchSpy.fn);

    const data = await api.get(`${BASE}/api/v1/projects`);

    expect(data).toEqual({ id: 'p1' });
    expect(fetchSpy.calls).toHaveLength(1);
    expect(fetchSpy.calls[0].url).toBe(`${BASE}/api/v1/projects`);
    expect(fetchSpy.calls[0].headers.Authorization).toBe(`Bearer ${TOKEN}`);
  });

  it('CrossOriginHttpSendsNoBearer: CDN/模型 provider/异端口一律无 bearer', async () => {
    const { api } = await readyWith([HANDSHAKE_CONN]);
    const fetchSpy = makeFetchSpy(() => ok());
    vi.stubGlobal('fetch', fetchSpy.fn);

    const denied = [
      'https://api.openai.com/v1/models', // 模型 provider
      'https://cdn.example.com/app.js', // CDN
      'http://127.0.0.1:9999/api/v1/projects', // 同 host 异端口
      'https://127.0.0.1:41234/api/v1/projects', // 同 host 异协议族
    ];
    for (const url of denied) {
      await api.get(url);
    }

    expect(fetchSpy.calls).toHaveLength(denied.length);
    for (const call of fetchSpy.calls) {
      expect(call.headers, call.url).not.toHaveProperty('Authorization');
      expect(JSON.stringify(call.headers), call.url).not.toContain(TOKEN);
    }
  });

  it('CrossOriginAuthStatusIgnored: 跨源 401/403 不重配对、不落闩', async () => {
    const { auth, api, invoke } = await readyWith([HANDSHAKE_CONN]);
    const fetchSpy = makeFetchSpy((i) => (i === 0 ? unauthorized() : forbidden()));
    vi.stubGlobal('fetch', fetchSpy.fn);

    await expect(api.get('https://api.openai.com/v1/models')).rejects.toMatchObject({ status: 401 });
    await expect(api.get('https://api.openai.com/v1/models')).rejects.toMatchObject({ status: 403 });

    expect(invoke.fn).toHaveBeenCalledTimes(1); // 仅 init；跨源不触发 refresh
    expect(auth.getAuthFailure()).toBeNull();
  });

  it('LegacyAndBrowserSendNoBearer: legacy 配对与浏览器模式同源也不带 bearer', async () => {
    // legacy 配对：按设计无 token。
    const legacyMods = await readyWith([LEGACY_CONN]);
    const legacyFetch = makeFetchSpy(() => ok());
    vi.stubGlobal('fetch', legacyFetch.fn);
    await legacyMods.api.get('http://127.0.0.1:18081/api/v1/projects');
    expect(legacyFetch.calls[0].headers).not.toHaveProperty('Authorization');

    // 浏览器模式：无配对连接。
    vi.unstubAllGlobals();
    stubWindow('http://localhost:3000');
    const browserMods = await freshModules();
    await browserMods.connection.initBackendConnection({ isTauri: () => false });
    const browserFetch = makeFetchSpy(() => ok());
    vi.stubGlobal('fetch', browserFetch.fn);
    await browserMods.api.get('http://localhost:3000/api/v1/projects');
    expect(browserFetch.calls[0].headers).not.toHaveProperty('Authorization');
  });

  it('UnauthorizedRebinds: 同源 401 清空失效连接并重配对，后续请求只用新 token', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { auth, api, invoke } = await readyWith(queue);
    queue.push(ROTATED_CONN); // sidecar 重启后的新握手等待被 refresh 取到
    const fetchSpy = makeFetchSpy((i) => (i === 0 ? unauthorized() : ok({ via: 'rotated' })));
    vi.stubGlobal('fetch', fetchSpy.fn);

    await expect(api.get(`${BASE}/api/v1/projects`)).rejects.toMatchObject({ status: 401 });

    // 401 处理器在抛出前完成重配对：旧连接被替换，invoke 通道又走了一轮。
    expect(invoke.fn).toHaveBeenCalledTimes(2);
    expect(auth.getTokenForUrl(`${BASE}/api/v1/projects`)).toBe(ROTATED_TOKEN);

    const data = await api.get(`${BASE}/api/v1/projects`);
    expect(data).toEqual({ via: 'rotated' });
    expect(fetchSpy.calls).toHaveLength(2);
    expect(fetchSpy.calls[0].headers.Authorization).toBe(`Bearer ${TOKEN}`); // 当时仍有效
    expect(fetchSpy.calls[1].headers.Authorization).toBe(`Bearer ${ROTATED_TOKEN}`);
    expect(JSON.stringify(fetchSpy.calls[1])).not.toContain(TOKEN); // 旧 token 不再出现
  });

  it('Concurrent401sRebindSingleFlight: 并发 401 只触发一次重配对', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { api, invoke } = await readyWith(queue);
    queue.push(ROTATED_CONN);
    vi.stubGlobal('fetch', makeFetchSpy(() => unauthorized()).fn);

    const results = await Promise.allSettled([
      api.get(`${BASE}/api/v1/projects`),
      api.get(`${BASE}/api/v1/agents`),
      api.get(`${BASE}/api/v1/config`),
    ]);

    expect(results.every((r) => r.status === 'rejected')).toBe(true);
    expect(invoke.fn).toHaveBeenCalledTimes(2); // init + 单次单飞 refresh
  });

  it('RepeatedStaleTokenRebindsAgain: 上一次重配对结束后，新的 401 仍会再次重配对', async () => {
    const THIRD_CONN: BackendConnection = {
      ...HANDSHAKE_CONN,
      token: 'wire-canary-token-e27a9c13',
      process_start_id: 'start-789',
    };
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { auth, api, invoke } = await readyWith(queue);
    vi.stubGlobal('fetch', makeFetchSpy(() => unauthorized()).fn);

    queue.push(ROTATED_CONN); // 第一次 sidecar 重启
    await expect(api.get(`${BASE}/api/v1/projects`)).rejects.toMatchObject({ status: 401 });
    expect(invoke.fn).toHaveBeenCalledTimes(2);
    expect(auth.getTokenForUrl(`${BASE}/api/v1/projects`)).toBe(ROTATED_TOKEN);

    queue.push(THIRD_CONN); // 第二次 sidecar 重启：单飞只合并并发，不能把后续 401 永久短路
    await expect(api.get(`${BASE}/api/v1/projects`)).rejects.toMatchObject({ status: 401 });
    expect(invoke.fn).toHaveBeenCalledTimes(3);
    expect(auth.getTokenForUrl(`${BASE}/api/v1/projects`)).toBe(THIRD_CONN.token);
  });

  it('ForbiddenLatchesUntilPairingChanges: 403 落闩不重配对，配对身份变化才解锁', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { auth, api, connection, invoke } = await readyWith(queue);
    vi.stubGlobal('fetch', makeFetchSpy(() => forbidden()).fn);

    await expect(api.get(`${BASE}/api/v1/projects`)).rejects.toMatchObject({ status: 403 });
    expect(auth.getAuthFailure()).toBe('forbidden');
    expect(invoke.fn).toHaveBeenCalledTimes(1); // 403 不触发 refresh

    // 恒等 refresh（同一连接，无变化事件）不解锁。
    queue.push({ ...HANDSHAKE_CONN });
    await connection.refreshBackendConnection();
    expect(auth.getAuthFailure()).toBe('forbidden');

    // 配对身份变化（新 token）解锁。
    queue.push(ROTATED_CONN);
    await connection.refreshBackendConnection();
    expect(auth.getAuthFailure()).toBeNull();
  });

  it('ReadinessCarriesBearerAndClassifiesAuth: /ready 带 bearer，401 重配对、403 not_ready+落闩', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { auth, readiness } = await readyWith(queue);

    // 200：bearer 已附着，正常解析组件。
    let fetchSpy = makeFetchSpy(() =>
      envelope(200, { status: 'ready', version: 'v1', components: { planner: { ready: true, required: true } } }),
    );
    vi.stubGlobal('fetch', fetchSpy.fn);
    const ready = await readiness.fetchReadiness();
    expect(ready.status).toBe('ready');
    expect(fetchSpy.calls[0].url).toBe(`${BASE}/ready`);
    expect(fetchSpy.calls[0].headers.Authorization).toBe(`Bearer ${TOKEN}`);

    // 401：不可达 + 重配对（queue 里放新握手）。
    queue.push(ROTATED_CONN);
    fetchSpy = makeFetchSpy(() => unauthorized());
    vi.stubGlobal('fetch', fetchSpy.fn);
    const unauth = await readiness.fetchReadiness();
    expect(unauth).toEqual({ reachable: false, status: 'unreachable', components: {} });
    expect(auth.getTokenForUrl(`${BASE}/ready`)).toBe(ROTATED_TOKEN);

    // 403：可达但拒绝 —— not_ready 且权限落闩。
    fetchSpy = makeFetchSpy(() => forbidden());
    vi.stubGlobal('fetch', fetchSpy.fn);
    const denied = await readiness.fetchReadiness();
    expect(denied.reachable).toBe(true);
    expect(denied.status).toBe('not_ready');
    expect(auth.getAuthFailure()).toBe('forbidden');
  });

  it('AuditAndChatCarryBearer: 直接 fetch 点逐一接线', async () => {
    await readyWith([HANDSHAKE_CONN]);
    const fetchSpy = makeFetchSpy((i) =>
      i === 0 ? ok({ entries: [], total: 0, has_more: false }) : i === 1 ? ok({}) : ok({ content: '你好' }),
    );
    vi.stubGlobal('fetch', fetchSpy.fn);

    const audit = await import('../../services/audit');
    const logs = await audit.listAuditLogs({ limit: 5 });
    expect(logs.total).toBe(0);

    const chat = await import('./chat');
    expect(await chat.probeChatEndpoint()).toBe(true);
    const deltas: string[] = [];
    await chat.streamChat(
      { projectId: 'p1', stage: 'coding', agent: CHAT_AGENT, message: 'hi', files: [], history: [] },
      (d) => deltas.push(d),
    );

    expect(fetchSpy.calls).toHaveLength(3);
    for (const call of fetchSpy.calls) {
      expect(call.headers.Authorization, call.url).toBe(`Bearer ${TOKEN}`);
      expect(call.url).not.toContain(TOKEN);
    }
    expect(deltas).toEqual(['你好']);
  });

  it('TokenNeverInUrlLogsOrStorage: canary 只走 header/子协议，URL/console/storage 零泄漏', async () => {
    const logs: string[] = [];
    for (const level of ['log', 'info', 'warn', 'error', 'debug'] as const) {
      vi.spyOn(console, level).mockImplementation((...args: unknown[]) => {
        logs.push(args.map(String).join(' '));
      });
    }
    const localSetItem = vi.fn();
    const sessionSetItem = vi.fn();
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: localSetItem });
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: sessionSetItem });
    stubWindow();
    const fakeWs = stubWebSocket();

    const { api } = await readyWith([HANDSHAKE_CONN]);
    const fetchSpy = makeFetchSpy(() => ok());
    vi.stubGlobal('fetch', fetchSpy.fn);

    await api.get(`${BASE}/api/v1/projects`);
    const ws = await import('./ws');
    ws.subscribe('flow:project:p1', () => undefined);

    const joinedLogs = logs.join('\n');
    expect(joinedLogs).toContain('127.0.0.1:41234'); // 就绪日志确实产生
    expect(joinedLogs).not.toContain(TOKEN);
    expect(localSetItem).not.toHaveBeenCalled();
    expect(sessionSetItem).not.toHaveBeenCalled();
    for (const call of fetchSpy.calls) {
      expect(call.url).not.toContain(TOKEN);
    }
    expect(fakeWs.instances).toHaveLength(1);
    expect(fakeWs.instances[0].url).not.toContain(TOKEN);
    // token 的唯一放行通道：Authorization header 与 token 子协议。
    expect(fakeWs.instances[0].protocols).toContain(`codeflow.token.${TOKEN}`);
    expect(fetchSpy.calls[0].headers.Authorization).toBe(`Bearer ${TOKEN}`);
  });
});

describe('T0.08.b auth wiring — WebSocket subprotocols', () => {
  it('WsDualProtocols: 有 token 时提供 codeflow.v1 + codeflow.token.<token>', async () => {
    await readyWith([HANDSHAKE_CONN]);
    stubWindow();
    const fakeWs = stubWebSocket();

    const ws = await import('./ws');
    ws.subscribe('flow:project:p1', () => undefined);

    expect(fakeWs.instances).toHaveLength(1);
    const inst = fakeWs.instances[0];
    expect(inst.url).toMatch(/^ws:\/\/127\.0\.0\.1:41234\/api\/v1\/conversations\/wb-[a-z0-9]+\/stream$/);
    expect(inst.url).not.toContain(TOKEN);
    expect(inst.protocols).toEqual(['codeflow.v1', `codeflow.token.${TOKEN}`]);
  });

  it('WsStableProtocolOnlyWithoutToken: legacy/浏览器模式只提供稳定子协议', async () => {
    // legacy（Tauri 配对但无 token）
    await readyWith([LEGACY_CONN]);
    stubWindow();
    let fakeWs = stubWebSocket();
    let ws = await import('./ws');
    ws.subscribe('t', () => undefined);
    expect(fakeWs.instances).toHaveLength(1);
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1']);
    expect(JSON.stringify(fakeWs.instances[0].protocols)).not.toContain(TOKEN);

    // 浏览器模式
    vi.resetModules();
    stubWindow('http://localhost:3000');
    const { connection } = await freshModules();
    await connection.initBackendConnection({ isTauri: () => false });
    fakeWs = stubWebSocket();
    ws = await import('./ws');
    ws.subscribe('t', () => undefined);
    expect(fakeWs.instances).toHaveLength(1);
    expect(fakeWs.instances[0].url).toContain('ws://localhost:3000/');
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1']);
  });

  it('WsDropRebindsAndReconnectsWithRotatedToken: 掉线重配对，下一次连接用新 token', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { auth, invoke } = await readyWith(queue);
    stubWindow();
    const fakeWs = stubWebSocket();
    const ws = await import('./ws');
    ws.subscribe('t', () => undefined);
    expect(fakeWs.instances).toHaveLength(1);

    vi.useFakeTimers();
    queue.push(ROTATED_CONN);
    fakeWs.instances[0].drop(); // 服务端断开（疑似 401：旧 token 失效）
    await flushMicro();

    expect(invoke.fn).toHaveBeenCalledTimes(2); // 掉线触发一次单飞 refresh
    expect(auth.getTokenForUrl(`${BASE}/api/v1/x`)).toBe(ROTATED_TOKEN);
    expect(fakeWs.instances).toHaveLength(1); // 重连等待退避定时器

    await vi.advanceTimersByTimeAsync(1000);
    expect(fakeWs.instances).toHaveLength(2);
    expect(fakeWs.instances[1].protocols).toEqual(['codeflow.v1', `codeflow.token.${ROTATED_TOKEN}`]);
  });

  it('WsDropWithoutTokenSkipsRebind: 未附带凭据的掉线不触发重配对', async () => {
    const { invoke } = await readyWith([LEGACY_CONN]);
    stubWindow();
    const fakeWs = stubWebSocket();
    const ws = await import('./ws');
    ws.subscribe('t', () => undefined);

    vi.useFakeTimers();
    fakeWs.instances[0].drop();
    await flushMicro();

    expect(invoke.fn).toHaveBeenCalledTimes(1); // 仅 init
  });

  it('WsForbiddenLatchStopsStorm: 403 落闩后关 socket、清定时器、60s 零重连，解锁后恢复', async () => {
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { auth, connection } = await readyWith(queue);
    stubWindow();
    const fakeWs = stubWebSocket();
    const ws = await import('./ws');
    ws.subscribe('t', () => undefined);
    expect(fakeWs.instances).toHaveLength(1);

    vi.useFakeTimers();
    // 任一同源 HTTP 403 落闩：ws 模块立即断 socket 并停止重连循环。
    await auth.handleAuthHttpStatus(`${BASE}/api/v1/projects`, 403);
    expect(auth.getAuthFailure()).toBe('forbidden');
    expect(fakeWs.instances[0].closeCalls).toBe(1);

    await vi.advanceTimersByTimeAsync(60_000);
    expect(fakeWs.instances).toHaveLength(1); // 60s 内零重试（无风暴）

    // 配对身份变化解锁：自动恢复连接并用新 token。
    queue.push(ROTATED_CONN);
    await connection.refreshBackendConnection();
    await flushMicro();
    expect(auth.getAuthFailure()).toBeNull();
    expect(fakeWs.instances).toHaveLength(2);
    expect(fakeWs.instances[1].protocols).toEqual(['codeflow.v1', `codeflow.token.${ROTATED_TOKEN}`]);

    // 进行中的重连定时器也被落闩清除：断线后排了 1s 重连，落闩后立即作废。
    queue.push({ ...ROTATED_CONN }); // 恒等 refresh：无变化事件，闩不受影响
    fakeWs.instances[1].drop();
    await flushMicro(); // 掉线重配对 settle（恒等，无事件）
    await auth.handleAuthHttpStatus(`${BASE}/api/v1/projects`, 403);
    expect(auth.getAuthFailure()).toBe('forbidden');
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fakeWs.instances).toHaveLength(2); // 定时器已清除，仍零重试
  });
});
