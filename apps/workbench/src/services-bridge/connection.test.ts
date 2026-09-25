// FU for the backend connection model (T0.07.c). The invoke channel is
// injected, so no Tauri runtime is needed; each test re-imports the modules
// to get a fresh connection state machine.
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BackendConnection, BackendConnectionStatus } from './connection';

const HANDSHAKE_CONN: BackendConnection = {
  host: '127.0.0.1',
  port: 41234,
  token: 'test-token-canary-7f3a9c51',
  expires_at: 'process',
  process_start_id: 'start-123',
  legacy: false,
};

/** Invoke stub that serves queued get_backend_connection responses (null when drained). */
function makeInvoke(queue: unknown[]) {
  const calls: string[] = [];
  const fn = vi.fn(async (command: string) => {
    calls.push(command);
    if (command !== 'get_backend_connection') {
      throw new Error(`unexpected invoke command: ${command}`);
    }
    return queue.length > 0 ? queue.shift() : null;
  });
  return { fn, calls };
}

async function freshModules() {
  vi.resetModules();
  const connection = await import('./connection');
  const api = await import('../../api');
  return { connection, api };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('backend connection model', () => {
  it('HandshakeSplitLines: 分片/延迟握手只在完整连接到达后推进到 ready', async () => {
    const { connection, api } = await freshModules();
    // Rust 侧握手行被分片/延迟时，invoke 一直返回 null，直到完整行解析完成。
    const { fn, calls } = makeInvoke([null, null, HANDSHAKE_CONN]);
    const seen: BackendConnectionStatus[] = [];
    connection.onBackendConnectionChange((s) => seen.push(s.status));

    const pending = connection.initBackendConnection({
      invoke: fn,
      isTauri: () => true,
      intervalMs: 0,
    });
    // 轮询期间不得产生任何默认/猜测 base（无 8080 回退）。
    expect(api.getApiBase()).toBe('');
    expect(connection.getBackendConnectionSnapshot().status).toBe('pending');

    const result = await pending;
    expect(result).toBe('ready');
    expect(seen).toEqual(['pending', 'ready']);
    expect(fn).toHaveBeenCalledTimes(3);
    expect(new Set(calls)).toEqual(new Set(['get_backend_connection']));
    expect(api.getApiBase()).toBe('http://127.0.0.1:41234');
    expect(api.getWsBase()).toBe('ws://127.0.0.1:41234');
    expect(connection.getBackendConnection()).toEqual(HANDSHAKE_CONN);
  });

  it('LegacyModeMarked: legacy 连接无 token 且 legacy 标记为真', async () => {
    const { connection, api } = await freshModules();
    const legacyConn: BackendConnection = { host: '127.0.0.1', port: 18081, legacy: true };
    const { fn } = makeInvoke([legacyConn]);

    const result = await connection.initBackendConnection({
      invoke: fn,
      isTauri: () => true,
      intervalMs: 0,
    });
    expect(result).toBe('ready');
    const conn = connection.getBackendConnection();
    expect(conn?.legacy).toBe(true);
    expect(conn?.token).toBeUndefined();
    expect(api.getApiBase()).toBe('http://127.0.0.1:18081');
  });

  it('ClearsConnectionOnExit: invoke 返回 null 时连接清空并转为 unavailable，重启后重新配对', async () => {
    const { connection, api } = await freshModules();
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { fn } = makeInvoke(queue);
    const seen: BackendConnectionStatus[] = [];
    connection.onBackendConnectionChange((s) => seen.push(s.status));

    await connection.initBackendConnection({ invoke: fn, isTauri: () => true, intervalMs: 0 });
    expect(api.getApiBase()).toBe('http://127.0.0.1:41234');

    // sidecar 退出：Rust 侧已清空连接，invoke 返回 null。
    queue.push(null);
    expect(await connection.refreshBackendConnection()).toBe('unavailable');
    expect(connection.getBackendConnection()).toBeNull();
    expect(connection.getBackendConnectionSnapshot().baseUrl).toBeNull();
    expect(api.getApiBase()).toBe('');
    expect(seen).toEqual(['pending', 'ready', 'unavailable']);

    // sidecar 重启并完成新握手：新 process_start_id 的连接替换旧连接。
    const restarted: BackendConnection = {
      ...HANDSHAKE_CONN,
      port: 45555,
      process_start_id: 'start-456',
      token: 'test-token-canary-b2c4d6e8',
    };
    queue.push(restarted);
    expect(await connection.refreshBackendConnection()).toBe('ready');
    expect(api.getApiBase()).toBe('http://127.0.0.1:45555');
    expect(connection.getBackendConnection()?.process_start_id).toBe('start-456');
  });

  it('DoesNotLogToken: token 不进入 console、storage、URL 或请求头', async () => {
    const { connection, api } = await freshModules();
    const token = HANDSHAKE_CONN.token as string;
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
    const fetchSpy = vi.fn(
      async (_url: string, _init?: RequestInit) =>
        new Response(JSON.stringify({ success: true, data: null }), { status: 200 })
    );
    vi.stubGlobal('fetch', fetchSpy);

    const { fn } = makeInvoke([HANDSHAKE_CONN, HANDSHAKE_CONN]);
    await connection.initBackendConnection({ invoke: fn, isTauri: () => true, intervalMs: 0 });
    // refresh 路径也带着含 token 的连接跑一遍。
    await connection.refreshBackendConnection();
    // T0.08.b 起同源请求必须携带 Authorization: Bearer（唯一放行通道）；
    // canary 的负向面收窄为 console/storage/URL 零泄漏。
    await api.get(`${api.getApiBase()}/ready`);

    const joinedLogs = logs.join('\n');
    expect(joinedLogs).toContain('127.0.0.1:41234'); // 就绪日志确实产生
    expect(joinedLogs).not.toContain(token);
    expect(localSetItem).not.toHaveBeenCalled();
    expect(sessionSetItem).not.toHaveBeenCalled();
    expect(api.getApiBase()).not.toContain(token);
    expect(api.getWsBase()).not.toContain(token);
    for (const [reqUrl, reqInit] of fetchSpy.mock.calls) {
      // token 只允许出现在 Authorization header 值里，绝不进 URL。
      expect(String(reqUrl)).not.toContain(token);
      const headers = (reqInit as RequestInit | undefined)?.headers as Record<string, string>;
      expect(headers.Authorization).toBe(`Bearer ${token}`);
    }
  });

  it('TimeoutStaysUnavailable: 握手超时停在 unavailable，不发任何探测请求', async () => {
    const { connection, api } = await freshModules();
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
    const { fn, calls } = makeInvoke([]); // 握手永远未就绪

    const result = await connection.initBackendConnection({
      invoke: fn,
      isTauri: () => true,
      maxAttempts: 3,
      intervalMs: 0,
    });
    expect(result).toBe('unavailable');
    expect(connection.getBackendConnection()).toBeNull();
    expect(connection.getBackendConnectionSnapshot().baseUrl).toBeNull();
    expect(api.getApiBase()).toBe('');
    // 不扫描端口：整个解析过程没有发出任何 fetch。
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(fn).toHaveBeenCalledTimes(3);
    expect(new Set(calls)).toEqual(new Set(['get_backend_connection']));
  });

  it('BrowserModeUsesConfiguredBaseWithoutProbing: 浏览器模式用同源 base 且不探测', async () => {
    const { connection, api } = await freshModules();
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
    vi.stubGlobal('window', { location: { origin: 'http://localhost:3000' } });

    const result = await connection.initBackendConnection({ isTauri: () => false });
    expect(result).toBe('ready');
    expect(api.getApiBase()).toBe('http://localhost:3000');
    expect(api.getWsBase()).toBe('ws://localhost:3000');
    // 浏览器模式没有配对连接，也没有 token。
    expect(connection.getBackendConnection()).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
