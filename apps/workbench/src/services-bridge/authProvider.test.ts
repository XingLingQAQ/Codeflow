// FU for the origin-scoped auth provider (T0.08.a). The provider derives every
// decision from the live connection module; tests re-import the modules for a
// fresh state machine and use canary tokens to prove the token never leaks
// into logs, storage, URLs, context objects, or cross-origin requests.
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BackendConnection } from './connection';

const BASE = 'http://127.0.0.1:41234';
const TOKEN = 'auth-canary-token-9d2f4b17';
const ROTATED_TOKEN = 'auth-canary-token-e5c7a1f3';

const HANDSHAKE_CONN: BackendConnection = {
  host: '127.0.0.1',
  port: 41234,
  token: TOKEN,
  expires_at: 'process',
  process_start_id: 'start-123',
  legacy: false,
};

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
  return { connection, auth };
}

async function readyWith(conn: BackendConnection) {
  const { connection, auth } = await freshModules();
  const { fn } = makeInvoke([conn]);
  await connection.initBackendConnection({ invoke: fn, isTauri: () => true, intervalMs: 0 });
  return { connection, auth };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('auth provider (origin-scoped token attachment)', () => {
  it('SameOriginMatrix: 连接 base、相对路径、绝对同 origin、ws 族允许附带 token', async () => {
    const { auth } = await readyWith(HANDSHAKE_CONN);

    const allowed = [
      BASE, // 连接 base 本身
      `${BASE}/api/v1/ready`, // 绝对同 origin
      `${BASE}/api/v1/workspace/read?root=%2Fproj&path=a.ts`, // 深路径带查询
      '/api/v1/ready', // 相对路径解析到连接 base
      'api/v1/ready', // 无前导斜杠的相对路径
      'ws://127.0.0.1:41234/api/v1/conversations/wb-1/stream', // ws 族等价 http
    ];
    for (const target of allowed) {
      const ctx = auth.getAuthContext(target);
      expect(ctx.sameOrigin, target).toBe(true);
      expect(ctx.tokenAvailable, target).toBe(true);
      expect(ctx.legacy, target).toBe(false);
      expect(ctx.attachToken, target).toBe(true);
      expect(ctx.baseOrigin, target).toBe(BASE);
      expect(auth.getTokenForUrl(target), target).toBe(TOKEN);
      // 上下文对象不携带 token 本体。
      expect('token' in ctx, target).toBe(false);
      expect(JSON.stringify(ctx), target).not.toContain(TOKEN);
    }
  });

  it('CrossOriginMatrix: 外域 http/https、端口不同、协议不同、userinfo 欺骗一律不附带', async () => {
    const { auth } = await readyWith(HANDSHAKE_CONN);

    const denied = [
      'http://127.0.0.1:9999/api/v1/ready', // 端口不同
      'https://127.0.0.1:41234/api/v1/ready', // 协议不同
      'wss://127.0.0.1:41234/ws', // 跨越 http->https 族边界
      'http://example.com/x', // 外域 http (CDN)
      'https://api.openai.com/v1/chat/completions', // 外域 https (模型 provider)
      '//evil.example.com/x', // 协议相对，解析后仍为外域
      'http://127.0.0.1.evil.com/x', // 后缀欺骗
      'http://127.0.0.1:41234@evil.com/x', // userinfo 欺骗，真实 host 是 evil.com
      'data:text/plain,hello', // 非 http 族方案
      'tauri://localhost/api/v1/ready', // Tauri 资产协议
      'http://[', // 不可解析
    ];
    for (const target of denied) {
      const ctx = auth.getAuthContext(target);
      expect(ctx.sameOrigin, target).toBe(false);
      expect(ctx.attachToken, target).toBe(false);
      expect(auth.getTokenForUrl(target), target).toBeNull();
      expect(JSON.stringify(ctx), target).not.toContain(TOKEN);
    }
  });

  it('DefaultPortNormalization: 显式默认端口与省略端口视为同一 origin', async () => {
    const { connection, auth } = await freshModules();
    vi.stubGlobal('window', { location: { origin: 'http://localhost' } });
    await connection.initBackendConnection({ isTauri: () => false });

    // 浏览器模式 base 无显式端口；:80 是 http 默认端口 -> 同源。
    const ctx = auth.getAuthContext('http://localhost:80/api/v1/ready');
    expect(ctx.baseOrigin).toBe('http://localhost');
    expect(ctx.sameOrigin).toBe(true);
    // 浏览器模式无配对连接，token 不可用 -> 仍不附带。
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl('http://localhost:80/api/v1/ready')).toBeNull();
    // :443 不是 http 默认端口 -> 端口不同，跨源。
    expect(auth.getAuthContext('http://localhost:443/x').sameOrigin).toBe(false);
  });

  it('LegacyModeNoToken: legacy 连接同源也明确标记无 token、不附带', async () => {
    const legacyConn: BackendConnection = { host: '127.0.0.1', port: 18081, legacy: true };
    const { auth } = await readyWith(legacyConn);

    const ctx = auth.getAuthContext('http://127.0.0.1:18081/api/v1/ready');
    expect(ctx.sameOrigin).toBe(true);
    expect(ctx.legacy).toBe(true);
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl('http://127.0.0.1:18081/api/v1/ready')).toBeNull();
  });

  it('LegacyWithTokenStillAttachesNothing: legacy 连接即使意外带 token 也一律不附带', async () => {
    // 防御性断言：legacy 配对按设计无 token，若负载意外携带也必须被 legacy 闸拦下。
    const legacyWithToken: BackendConnection = {
      host: '127.0.0.1',
      port: 18081,
      token: TOKEN,
      process_start_id: 'start-legacy',
      legacy: true,
    };
    const { auth } = await readyWith(legacyWithToken);

    const ctx = auth.getAuthContext('http://127.0.0.1:18081/api/v1/ready');
    expect(ctx.sameOrigin).toBe(true);
    expect(ctx.legacy).toBe(true);
    expect(ctx.tokenAvailable).toBe(true); // token 存在，但 legacy 闸优先
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl('http://127.0.0.1:18081/api/v1/ready')).toBeNull();
    expect(JSON.stringify(ctx)).not.toContain(TOKEN);
  });

  it('EmptyTokenAttachesNothing: 空串 token 视为无 token，同源也不附带', async () => {
    const emptyToken: BackendConnection = { ...HANDSHAKE_CONN, token: '' };
    const { auth } = await readyWith(emptyToken);

    const ctx = auth.getAuthContext(`${BASE}/api/v1/ready`);
    expect(ctx.sameOrigin).toBe(true);
    expect(ctx.legacy).toBe(false);
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl(`${BASE}/api/v1/ready`)).toBeNull();
  });

  it('PendingAttachesNothing: 握手 pending 期间不附带', async () => {
    const { connection, auth } = await freshModules();
    const never = vi.fn(() => new Promise<unknown>(() => {}));
    const pending = connection.initBackendConnection({
      invoke: never,
      isTauri: () => true,
      intervalMs: 0,
    });

    expect(connection.getBackendConnectionSnapshot().status).toBe('pending');
    const ctx = auth.getAuthContext(`${BASE}/api/v1/ready`);
    expect(ctx.baseOrigin).toBeNull();
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl(`${BASE}/api/v1/ready`)).toBeNull();
    // pending 的 init 永不 settle；不 await，避免悬挂断言。
    void pending;
  });

  it('UnavailableAttachesNothing: 握手超时停在 unavailable 后不附带', async () => {
    const { connection, auth } = await freshModules();
    const { fn } = makeInvoke([]);
    const result = await connection.initBackendConnection({
      invoke: fn,
      isTauri: () => true,
      maxAttempts: 2,
      intervalMs: 0,
    });

    expect(result).toBe('unavailable');
    const ctx = auth.getAuthContext(`${BASE}/api/v1/ready`);
    expect(ctx.baseOrigin).toBeNull();
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl(`${BASE}/api/v1/ready`)).toBeNull();
  });

  it('BrowserModeNoToken: 浏览器模式同源也无 token 可附带', async () => {
    const { connection, auth } = await freshModules();
    vi.stubGlobal('window', { location: { origin: 'http://localhost:3000' } });
    await connection.initBackendConnection({ isTauri: () => false });

    const ctx = auth.getAuthContext('http://localhost:3000/api/v1/ready');
    expect(ctx.sameOrigin).toBe(true);
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl('http://localhost:3000/api/v1/ready')).toBeNull();
  });

  it('TokenRotationUsesOnlyNewToken: 新 process_start_id 配对后旧 token 不再被使用', async () => {
    const { connection, auth } = await freshModules();
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { fn } = makeInvoke(queue);
    await connection.initBackendConnection({ invoke: fn, isTauri: () => true, intervalMs: 0 });
    expect(auth.getTokenForUrl(`${BASE}/ready`)).toBe(TOKEN);

    // sidecar 重启并完成新握手：新 process_start_id + 新 token。
    const restarted: BackendConnection = {
      ...HANDSHAKE_CONN,
      token: ROTATED_TOKEN,
      process_start_id: 'start-456',
    };
    queue.push(restarted);
    expect(await connection.refreshBackendConnection()).toBe('ready');

    // provider 无缓存，每次实时派生：只返回新 token，旧 token 不再出现。
    expect(auth.getTokenForUrl(`${BASE}/ready`)).toBe(ROTATED_TOKEN);
    expect(auth.getTokenForUrl(`${BASE}/ready`)).not.toBe(TOKEN);
    const ctx = auth.getAuthContext(`${BASE}/ready`);
    expect(JSON.stringify(ctx)).not.toContain(TOKEN);
    expect(JSON.stringify(ctx)).not.toContain(ROTATED_TOKEN);
    // 轮换后跨源判定不变：外域依然拿不到任何 token。
    expect(auth.getTokenForUrl('https://api.openai.com/v1/chat/completions')).toBeNull();
  });

  it('SidecarExitRevokesAttachment: sidecar 退出清空连接后，原同源 URL 也不再附带', async () => {
    const { connection, auth } = await freshModules();
    const queue: unknown[] = [HANDSHAKE_CONN];
    const { fn } = makeInvoke(queue);
    await connection.initBackendConnection({ invoke: fn, isTauri: () => true, intervalMs: 0 });
    expect(auth.getTokenForUrl(`${BASE}/ready`)).toBe(TOKEN);

    // sidecar 退出：Rust 侧清空连接（invoke 返回 null），refresh 翻到 unavailable。
    expect(await connection.refreshBackendConnection()).toBe('unavailable');
    expect(connection.getBackendConnectionSnapshot().status).toBe('unavailable');
    const ctx = auth.getAuthContext(`${BASE}/ready`);
    expect(ctx.baseOrigin).toBeNull();
    expect(ctx.tokenAvailable).toBe(false);
    expect(ctx.attachToken).toBe(false);
    expect(auth.getTokenForUrl(`${BASE}/ready`)).toBeNull();
  });

  it('TokenNeverLoggedOrStored: token 不进入 console、storage 或任何返回值', async () => {
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

    const { auth } = await readyWith(HANDSHAKE_CONN);
    const sweep = [
      `${BASE}/api/v1/ready`,
      '/api/v1/ready',
      'ws://127.0.0.1:41234/ws',
      'http://127.0.0.1:9999/x',
      'https://api.openai.com/v1/chat/completions',
    ];
    const contexts = sweep.map((t) => auth.getAuthContext(t));
    const tokens = sweep.map((t) => auth.getTokenForUrl(t));

    const joinedLogs = logs.join('\n');
    expect(joinedLogs).toContain('127.0.0.1:41234'); // 连接就绪日志确实产生
    expect(joinedLogs).not.toContain(TOKEN);
    expect(localSetItem).not.toHaveBeenCalled();
    expect(sessionSetItem).not.toHaveBeenCalled();
    expect(JSON.stringify(contexts)).not.toContain(TOKEN);
    // token 只经 getTokenForUrl 的同源分支返回，跨源分支全部为 null。
    expect(tokens.filter((t) => t !== null)).toEqual([TOKEN, TOKEN, TOKEN]);
  });
});
