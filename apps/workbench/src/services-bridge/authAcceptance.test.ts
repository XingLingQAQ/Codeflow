// T0.08.c acceptance FU: the three end-to-end assertions the T0.08 ticket
// names, all on the real wiring (no mocks of the auth modules themselves).
//
//   CredentialsNotSentCrossOrigin — a credential may leave the app only toward
//     the pairing origin; CDN / model provider / same-host-different-port /
//     scheme-family mismatch / userinfo spoofing all get nothing, over both
//     the HTTP helpers and the WebSocket subprotocol offer.
//   WebSocketTokenProtocol — the offer is exactly ['codeflow.v1'] or
//     ['codeflow.v1', 'codeflow.token.<token>'], in that order, never in the URL.
//   RebindClearsCaches — a sidecar re-pair empties every registered
//     server-derived cache (React Query + chat availability), while an
//     identical refresh, the first ready, and an A→unavailable→A recovery
//     leave them intact.
//
// Every case installs console spies and checks localStorage/sessionStorage: a
// canary token must appear zero times outside its two sanctioned channels
// (Authorization header value, WebSocket token subprotocol).
import { QueryClient, QueryObserver } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BackendConnection } from './connection';

const BASE = 'http://127.0.0.1:41234';
const TOKEN = 'accept-canary-token-7d21f4b9';
const ROTATED_TOKEN = 'accept-canary-token-19c0ae63';

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

/** Same host+port, different process: a restarted sidecar with a recycled port. */
const RESTART_SAME_PORT_CONN: BackendConnection = {
  ...HANDSHAKE_CONN,
  token: 'accept-canary-token-4a8e2f10',
  process_start_id: 'start-123-b',
};

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

async function freshModules() {
  vi.resetModules();
  const connection = await import('./connection');
  const auth = await import('./authProvider');
  const api = await import('../../api');
  const caches = await import('./identityCaches');
  const chat = await import('./chat');
  return { connection, auth, api, caches, chat };
}

async function readyWith(queue: unknown[]) {
  const mods = await freshModules();
  const invoke = makeInvoke(queue);
  await mods.connection.initBackendConnection({
    invoke: invoke.fn,
    isTauri: () => true,
    intervalMs: 0,
  });
  return { ...mods, invoke };
}

type FetchCall = { url: string; headers: Record<string, string> };

/** fetch spy recording url + headers, serving 200 envelopes. */
function makeFetchSpy() {
  const calls: FetchCall[] = [];
  const fn = vi.fn(async (input: unknown, init?: RequestInit) => {
    calls.push({
      url: String(input),
      headers: { ...((init?.headers ?? {}) as Record<string, string>) },
    });
    return new Response(JSON.stringify({ success: true, data: null }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  return { fn, calls };
}

function stubWindow(origin = 'http://localhost:3000') {
  vi.stubGlobal('window', { location: { origin, search: '' } });
}

/** Flush the microtask queue (the identity reset's refetches are promise-based). */
async function flushMicro(rounds = 50): Promise<void> {
  for (let i = 0; i < rounds; i++) await Promise.resolve();
}

/** Minimal WebSocket stand-in: records constructor args. */
class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  readonly protocols: string[];
  readyState = FakeWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;

  constructor(url: string, protocols?: string | string[]) {
    this.url = url;
    this.protocols = Array.isArray(protocols) ? protocols : protocols ? [protocols] : [];
    FakeWebSocket.instances.push(this);
  }

  send(): void {
    /* no-op */
  }

  close(): void {
    this.readyState = FakeWebSocket.CLOSED;
  }
}

function stubWebSocket(): typeof FakeWebSocket {
  FakeWebSocket.instances = [];
  vi.stubGlobal('WebSocket', FakeWebSocket);
  return FakeWebSocket;
}

/**
 * Install console + storage spies. Returns the collected log text and the
 * setItem spies so each case can assert the canary never leaked.
 */
function installLeakCanaries() {
  const logs: string[] = [];
  for (const level of ['log', 'info', 'warn', 'error', 'debug'] as const) {
    vi.spyOn(console, level).mockImplementation((...args: unknown[]) => {
      logs.push(args.map(String).join(' '));
    });
  }
  const store = new Map<string, string>();
  const localSetItem = vi.fn((k: string, v: string) => {
    store.set(k, v);
  });
  const sessionSetItem = vi.fn((k: string, v: string) => {
    store.set(k, v);
  });
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: localSetItem,
  });
  vi.stubGlobal('sessionStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: sessionSetItem,
  });
  return {
    logs,
    localSetItem,
    sessionSetItem,
    /** Canary occurrences in every captured sink outside the two sanctioned channels. */
    canaryOutsidePayloads(): number {
      const haystack = [...logs, ...[...store.values()]].join('\n');
      return haystack.split(TOKEN).length - 1 + (haystack.split(ROTATED_TOKEN).length - 1);
    },
    /** Guard against a vacuous zero: the canary check only means something if logs/storage were observed. */
    assertObservedSomething(): void {
      if (logs.length === 0) throw new Error('leak canary observed no console output at all');
    },
  };
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('T0.08.c acceptance — credential origin gate', () => {
  it('CredentialsNotSentCrossOrigin: HTTP 与 WebSocket 都只对配对源携带凭据', async () => {
    const canaries = installLeakCanaries();
    stubWindow();
    const { api } = await readyWith([HANDSHAKE_CONN]);
    const fetchSpy = makeFetchSpy();
    vi.stubGlobal('fetch', fetchSpy.fn);

    // 同源（相对路径 + 绝对路径）→ 唯一带凭据的方向。
    await api.get(`${BASE}/api/v1/projects`);
    expect(fetchSpy.calls[0].headers.Authorization).toBe(`Bearer ${TOKEN}`);

    // 跨源全部零凭据。
    const denied = [
      'https://cdn.example.com/app.js', // CDN
      'https://api.openai.com/v1/models', // 模型 provider
      'http://127.0.0.1:9999/api/v1/projects', // 同 host 异端口
      'https://127.0.0.1:41234/api/v1/projects', // 协议族错配（http 配对 → https 目标）
      'http://127.0.0.1:41234@evil.example/api/v1/projects', // userinfo 欺骗
      'http://127.0.0.1:41234.evil.example/api/v1/projects', // 后缀欺骗
    ];
    for (const url of denied) {
      await api.get(url);
    }
    for (const call of fetchSpy.calls.slice(1)) {
      expect(call.headers, call.url).not.toHaveProperty('Authorization');
      expect(JSON.stringify(call.headers), call.url).not.toContain(TOKEN);
    }

    // 直接 fetch 点（audit 服务）也要遵守同一道门。
    const audit = await import('../../services/audit');
    const before = fetchSpy.calls.length;
    await audit.listAuditLogs({ limit: 5 });
    expect(fetchSpy.calls[before].headers.Authorization).toBe(`Bearer ${TOKEN}`);

    // WebSocket 同源：唯一带凭据的方向。
    const fakeWs = stubWebSocket();
    const ws = await import('./ws');
    ws.subscribe('flow:project:p1', () => undefined);
    expect(fakeWs.instances).toHaveLength(1);
    expect(fakeWs.instances[0].url.startsWith('ws://127.0.0.1:41234/')).toBe(true);
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1', `codeflow.token.${TOKEN}`]);

    // 用真实的 Origin 门验证同一判定：异 origin 的 ws URL 一律不产生 token
    // 子协议（ws.ts 的 wsProtocols 只在此基础上加 codeflow.token.<t>）。
    const { getTokenForUrl } = await import('./authProvider');
    const deniedWs = [
      'ws://127.0.0.1:9999/api/v1/conversations/x/stream', // 同 host 异端口
      'wss://127.0.0.1:41234/api/v1/conversations/x/stream', // 协议族错配
      'ws://127.0.0.1:41234@evil.example/api/v1/conversations/x/stream', // userinfo 欺骗
      'ws://127.0.0.1:41234.evil.example/api/v1/conversations/x/stream', // 后缀欺骗
      'ws://cdn.example.com/api/v1/conversations/x/stream', // CDN
    ];
    for (const url of deniedWs) {
      expect(getTokenForUrl(url), url).toBeNull();
    }
    expect(getTokenForUrl('ws://127.0.0.1:41234/api/v1/conversations/x/stream')).toBe(TOKEN);

    // 强制 WS 目标落在异 origin 上：只替换 ws URL 的来源（getWsBase），Origin
    // 门与 authProvider 全部保持真实实现 —— 子协议必须只剩稳定协议。
    vi.resetModules();
    vi.doMock('../../api', () => ({
      getApiBase: () => BASE,
      getWsBase: () => 'ws://evil.example:9999',
      API_ENDPOINTS: {},
      WS_ENDPOINTS: {},
    }));
    const xconn = await import('./connection');
    await xconn.initBackendConnection({
      invoke: makeInvoke([HANDSHAKE_CONN]).fn,
      isTauri: () => true,
      intervalMs: 0,
    });
    // 配对确实 ready 且 token 在手：下面的“无凭据”不是因为没有 token。
    expect(xconn.getBackendConnectionSnapshot().status).toBe('ready');
    const xauth = await import('./authProvider');
    expect(xauth.getTokenForUrl(`${BASE}/api/v1/projects`)).toBe(TOKEN);
    expect(xauth.getTokenForUrl('ws://evil.example:9999/api/v1/conversations/x/stream')).toBeNull();
    const crossWs = stubWebSocket();
    const xws = await import('./ws');
    xws.subscribe('t', () => undefined);
    expect(crossWs.instances).toHaveLength(1);
    expect(crossWs.instances[0].url.startsWith('ws://evil.example:9999/')).toBe(true);
    expect(crossWs.instances[0].protocols).toEqual(['codeflow.v1']);
    expect(crossWs.instances[0].url).not.toContain(TOKEN);
    vi.doUnmock('../../api');

    expect(fetchSpy.calls.every((c) => !c.url.includes(TOKEN))).toBe(true);
    expect(fakeWs.instances[0].url).not.toContain(TOKEN);
    canaries.assertObservedSomething();
    expect(canaries.canaryOutsidePayloads()).toBe(0);
  });
});

describe('T0.08.c acceptance — WebSocket token subprotocol', () => {
  it('WebSocketTokenProtocol: 子协议恰为 codeflow.v1(+token)，URL 不含 token', async () => {
    const canaries = installLeakCanaries();
    stubWindow();
    await readyWith([HANDSHAKE_CONN]);
    let fakeWs = stubWebSocket();

    let ws = await import('./ws');
    ws.subscribe('t', () => undefined);
    expect(fakeWs.instances).toHaveLength(1);
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1', `codeflow.token.${TOKEN}`]);
    expect(fakeWs.instances[0].url).not.toContain(TOKEN);
    expect(fakeWs.instances[0].protocols).toHaveLength(2);

    // legacy 配对（Tauri 但无 token）：只有稳定协议。
    stubWindow();
    await readyWith([LEGACY_CONN]);
    fakeWs = stubWebSocket();
    ws = await import('./ws');
    ws.subscribe('t', () => undefined);
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1']);
    expect(JSON.stringify(fakeWs.instances[0].protocols)).not.toContain(TOKEN);

    // 浏览器模式：无配对连接，同样只有稳定协议。
    stubWindow('http://localhost:3000');
    const browser = await freshModules();
    await browser.connection.initBackendConnection({ isTauri: () => false });
    fakeWs = stubWebSocket();
    const browserWs = await import('./ws');
    browserWs.subscribe('t', () => undefined);
    expect(fakeWs.instances[0].protocols).toEqual(['codeflow.v1']);
    expect(fakeWs.instances[0].url).not.toContain(TOKEN);

    canaries.assertObservedSomething();
    expect(canaries.canaryOutsidePayloads()).toBe(0);
  });
});

describe('T0.08.c acceptance — identity-scoped cache reset', () => {
  it('RebindClearsCaches: 换配对清空 React Query 与 chat 可用性缓存，同身份保持', async () => {
    const canaries = installLeakCanaries();
    stubWindow();
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { connection, caches, chat } = await readyWith(queue);

    // 真实 QueryClient + 生产同一个 helper（AppRoot 的注册点同构）。
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 0, refetchOnWindowFocus: false } },
    });
    caches.registerIdentityScopedCache('react-query', () =>
      caches.resetQueryClientForNewPairing(queryClient)
    );
    expect(caches.registeredIdentityScopedCaches()).toEqual([
      'services-bridge/chat.availability',
      'react-query',
    ]);

    const seed = (marker: string) => {
      queryClient.setQueryData(['projects'], { marker });
      queryClient.setQueryData(['flows', 'p1'], { marker });
    };
    const cached = () => queryClient.getQueryData(['projects']) ?? null;

    // chat 可用性：真实探测一次，缓存住 'available'。
    let fetchSpy = makeFetchSpy();
    vi.stubGlobal('fetch', fetchSpy.fn);
    expect(await chat.probeChatEndpoint()).toBe(true);

    // --- 不清理的三条路径 ---
    seed('first-ready');
    queue.push({ ...HANDSHAKE_CONN }); // 同身份 refresh（内容相同，不 emit）
    await connection.refreshBackendConnection();
    expect(cached()).toEqual({ marker: 'first-ready' });
    expect(await chat.probeChatEndpoint()).toBe(true); // 命中缓存，无新请求

    // 首次 ready（新模块实例下的第一轮）也不清。
    stubWindow();
    const fresh = await readyWith([HANDSHAKE_CONN]);
    const freshClient = new QueryClient();
    fresh.caches.registerIdentityScopedCache('react-query', () =>
      fresh.caches.resetQueryClientForNewPairing(freshClient)
    );
    freshClient.setQueryData(['projects'], { marker: 'fresh-boot' });
    expect(freshClient.getQueryData(['projects'])).toEqual({ marker: 'fresh-boot' });

    // A → unavailable → A（同 process_start_id）保持。
    queue.push(null);
    expect(await connection.refreshBackendConnection()).toBe('unavailable');
    expect(cached()).toEqual({ marker: 'first-ready' });
    queue.push({ ...HANDSHAKE_CONN });
    expect(await connection.refreshBackendConnection()).toBe('ready');
    expect(cached()).toEqual({ marker: 'first-ready' });

    // chat 可用性在 A→unavailable→A 后仍命中缓存。
    fetchSpy = makeFetchSpy();
    vi.stubGlobal('fetch', fetchSpy.fn);
    expect(await chat.probeChatEndpoint()).toBe(true);
    expect(fetchSpy.calls).toHaveLength(0);

    // --- 换身份：sidecar 重启（新 process_start_id + 新 token） ---
    queue.push(ROTATED_CONN);
    expect(await connection.refreshBackendConnection()).toBe('ready');
    expect(queryClient.getQueryData(['projects'])).toBeUndefined();
    expect(queryClient.getQueryData(['flows', 'p1'])).toBeUndefined();

    // chat 可用性回到 'unknown'：下一次探测真的发请求。
    fetchSpy = makeFetchSpy();
    vi.stubGlobal('fetch', fetchSpy.fn);
    expect(await chat.probeChatEndpoint()).toBe(true);
    expect(fetchSpy.calls).toHaveLength(1);

    // --- 换身份：端口复用但进程变了 ---
    seed('after-restart');
    queue.push(RESTART_SAME_PORT_CONN);
    await connection.refreshBackendConnection();
    expect(queryClient.getQueryData(['projects'])).toBeUndefined();

    // --- 用户本地数据不受影响：zustand persist 的用户数据项原样保留 ---
    const persisted = JSON.stringify({ state: { drafts: { 'p1:idea': '草稿' } }, version: 1 });
    localStorage.setItem('codeflow.drafts', persisted);
    seed('user-local-probe');
    queue.push({ ...RESTART_SAME_PORT_CONN }); // 同身份 refresh：不清
    await connection.refreshBackendConnection();
    expect(localStorage.getItem('codeflow.drafts')).toBe(persisted);
    expect(queryClient.getQueryData(['projects'])).toEqual({ marker: 'user-local-probe' });

    canaries.assertObservedSomething();
    expect(canaries.canaryOutsidePayloads()).toBe(0);
  });

  it('RebindRefreshesMountedObservers: 换配对后挂载中的观察者不再显示旧后端数据', async () => {
    const canaries = installLeakCanaries();
    stubWindow();
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { connection, caches } = await readyWith(queue);

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 0, refetchOnWindowFocus: false } },
    });
    caches.registerIdentityScopedCache('react-query', () =>
      caches.resetQueryClientForNewPairing(queryClient)
    );

    // 后端 A 的响应，换成后端 B 后 queryFn 会返回不同数据 —— 模拟真实组件。
    let backend = 'A';
    const queryFn = vi.fn(async () => ({ backend }));
    const observer = new QueryObserver(queryClient, { queryKey: ['projects'], queryFn });
    const seen: unknown[] = [];
    const unsubscribe = observer.subscribe(() => {
      seen.push(observer.getCurrentResult().data);
    });

    await observer.refetch();
    expect(observer.getCurrentResult().data).toEqual({ backend: 'A' });
    const callsAfterA = queryFn.mock.calls.length;
    seen.length = 0;

    // 同身份 refresh：观察者不受打扰。
    queue.push({ ...HANDSHAKE_CONN });
    await connection.refreshBackendConnection();
    expect(observer.getCurrentResult().data).toEqual({ backend: 'A' });
    expect(seen).toHaveLength(0);
    expect(queryFn.mock.calls.length).toBe(callsAfterA);

    // 换配对到 B：观察者必须收到通知、不得再显示 A 的数据，且重新请求。
    backend = 'B';
    queue.push(ROTATED_CONN);
    expect(await connection.refreshBackendConnection()).toBe('ready');
    await flushMicro();

    expect(queryFn.mock.calls.length).toBeGreaterThan(callsAfterA);
    expect(observer.getCurrentResult().data).toEqual({ backend: 'B' });
    expect(JSON.stringify(observer.getCurrentResult().data)).not.toContain('"A"');
    expect(seen.some((d) => JSON.stringify(d) === JSON.stringify({ backend: 'A' }))).toBe(false);

    unsubscribe();
    canaries.assertObservedSomething();
    expect(canaries.canaryOutsidePayloads()).toBe(0);
  });

  it('FirstReadyAfterProductionOrderDoesNotReset: 生产顺序（先注册后 init）首次 ready 不清', async () => {
    const canaries = installLeakCanaries();
    stubWindow();

    // 生产顺序：模块加载时注册（连接仍是 pending，seed echo 无 identity），
    // 之后才 initBackendConnection 到 ready(A)。
    const mods = await freshModules();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 0, refetchOnWindowFocus: false } },
    });
    mods.caches.registerIdentityScopedCache('react-query', () =>
      mods.caches.resetQueryClientForNewPairing(queryClient)
    );
    expect(mods.connection.getBackendConnectionSnapshot().status).toBe('pending');

    // 注册早于配对：缓存里先有数据，观察者也已挂载。
    queryClient.setQueryData(['flows', 'p1'], { marker: 'registered-before-pairing' });
    let backend = 'A';
    const queryFn = vi.fn(async () => ({ backend }));
    const observer = new QueryObserver(queryClient, { queryKey: ['projects'], queryFn });
    const seen: unknown[] = [];
    const unsubscribe = observer.subscribe(() => {
      seen.push(observer.getCurrentResult().data);
    });

    const queue: unknown[] = [HANDSHAKE_CONN];
    const invoke = makeInvoke(queue);
    expect(
      await mods.connection.initBackendConnection({
        invoke: invoke.fn,
        isTauri: () => true,
        intervalMs: 0,
      })
    ).toBe('ready');
    await flushMicro();

    // 首次 ready 不算换身份：注册时已有的数据与挂载观察者都保持。
    expect(queryClient.getQueryData(['flows', 'p1'])).toEqual({
      marker: 'registered-before-pairing',
    });
    await observer.refetch();
    expect(observer.getCurrentResult().data).toEqual({ backend: 'A' });
    const callsAfterA = queryFn.mock.calls.length;
    seen.length = 0;

    // 之后换配对到 B：仍然要清、要通知、要重新请求。
    backend = 'B';
    queue.push(ROTATED_CONN);
    expect(await mods.connection.refreshBackendConnection()).toBe('ready');
    await flushMicro();

    expect(queryClient.getQueryData(['flows', 'p1'])).toBeUndefined();
    expect(queryFn.mock.calls.length).toBeGreaterThan(callsAfterA);
    expect(observer.getCurrentResult().data).toEqual({ backend: 'B' });

    unsubscribe();
    canaries.assertObservedSomething();
    expect(canaries.canaryOutsidePayloads()).toBe(0);
  });
});
