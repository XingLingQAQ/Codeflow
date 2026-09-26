// FU for the /ready capabilities bridge (T0.12.c).
//
// fetchReadiness must parse data.capabilities into the typed shape the view
// model consumes, and must treat a missing or malformed set as "unknown"
// (capabilities undefined) rather than inventing a verdict — a backend that
// cannot prove executability must never be read as executable.
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BackendConnection } from './connection';

vi.mock('../mocks', () => ({
  isMockActive: () => false,
  jitter: async () => undefined,
  activateMock: () => undefined,
  showMockNotice: () => undefined,
}));

const BASE = 'http://127.0.0.1:41234';
const HANDSHAKE_CONN: BackendConnection = {
  host: '127.0.0.1',
  port: 41234,
  token: 'readiness-canary-token-51ab',
  expires_at: 'process',
  process_start_id: 'start-123',
  legacy: false,
};

const PRODUCTION_CAPABILITIES = {
  read_only: { state: 'ready', blocking: [] },
  execution: {
    state: 'unavailable',
    blocking: [
      { component: 'migrations', state: 'not_configured', error_code: 'probe_not_configured', remediation: 'migrations_pending' },
      { component: 'vault', state: 'not_configured', error_code: 'probe_not_configured', remediation: 'vault_locked' },
      { component: 'exec_backend', state: 'not_configured', error_code: 'probe_not_configured', remediation: 'backend_not_installed' },
    ],
    backends: {
      codex: {
        state: 'unavailable',
        blocking: [
          { component: 'exec_backend:codex', state: 'not_configured', error_code: 'probe_not_configured', remediation: 'backend_not_installed' },
        ],
      },
    },
  },
  merge: {
    state: 'unavailable',
    blocking: [
      { component: 'vault', state: 'not_configured', error_code: 'probe_not_configured', remediation: 'vault_locked' },
    ],
  },
};

async function freshModules() {
  vi.resetModules();
  const connection = await import('./connection');
  const readiness = await import('./readiness');
  return { connection, readiness };
}

async function readyWith(fetchImpl: () => Promise<Response>) {
  const { connection, readiness } = await freshModules();
  await connection.initBackendConnection({
    invoke: async () => HANDSHAKE_CONN,
    isTauri: () => true,
    intervalMs: 0,
  });
  vi.stubGlobal('fetch', vi.fn(fetchImpl));
  return readiness;
}

function envelope(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('ReadinessCapabilitiesParsing', () => {
  it('ParsesCapabilities: 解析 read_only/execution(backends)/merge', async () => {
    const readiness = await readyWith(async () =>
      envelope(200, {
        success: true,
        data: { status: 'ready', version: 'v1', components: {}, capabilities: PRODUCTION_CAPABILITIES },
      }),
    );
    const result = await readiness.fetchReadiness();
    expect(result.reachable).toBe(true);
    expect(result.status).toBe('ready');
    expect(result.capabilities?.read_only.state).toBe('ready');
    expect(result.capabilities?.execution.state).toBe('unavailable');
    expect(result.capabilities?.execution.backends.codex.state).toBe('unavailable');
    expect(result.capabilities?.execution.backends.codex.blocking[0].remediation).toBe(
      'backend_not_installed',
    );
    expect(result.capabilities?.merge.blocking[0].remediation).toBe('vault_locked');
  });

  it('MissingCapabilitiesStayUnknown: 旧后端不返回 capabilities 时字段缺省而非 ready', async () => {
    const readiness = await readyWith(async () =>
      envelope(200, {
        success: true,
        data: { status: 'ready', version: 'legacy', components: { planner: { ready: true, required: true } } },
      }),
    );
    const result = await readiness.fetchReadiness();
    expect(result.status).toBe('ready');
    expect(result.capabilities).toBeUndefined();
  });

  it('MalformedCapabilitiesStayUnknown: 形状不对时字段缺省而非部分解析', async () => {
    const readiness = await readyWith(async () =>
      envelope(200, {
        success: true,
        data: {
          status: 'ready',
          components: {},
          capabilities: {
            read_only: { state: 'ready', blocking: [] },
            // execution missing entirely
            merge: { state: 'ready', blocking: [] },
          },
        },
      }),
    );
    const result = await readiness.fetchReadiness();
    expect(result.capabilities).toBeUndefined();
  });

  it('ParseCapabilitiesIsPure: 直接调用 parseCapabilities 对坏输入返回 null', async () => {
    const { readiness } = await freshModules();
    expect(readiness.parseCapabilities(undefined)).toBeNull();
    expect(readiness.parseCapabilities(null)).toBeNull();
    expect(readiness.parseCapabilities('nope')).toBeNull();
    expect(readiness.parseCapabilities({ read_only: { state: 'ready' } })).toBeNull();
    // A backend entry without a blocking array is not a capability.
    expect(
      readiness.parseCapabilities({
        read_only: { state: 'ready', blocking: [] },
        execution: { state: 'ready', blocking: [], backends: { codex: { state: 'ready' } } },
        merge: { state: 'ready', blocking: [] },
      }),
    ).toBeNull();
    // No backends key at all is legal: no backend is registered.
    const noBackends = readiness.parseCapabilities({
      read_only: { state: 'ready', blocking: [] },
      execution: { state: 'ready', blocking: [] },
      merge: { state: 'ready', blocking: [] },
    });
    expect(noBackends?.execution.backends).toEqual({});
    // A missing capability set is unknown, never partially parsed.
    expect(
      readiness.parseCapabilities({
        read_only: { state: 'ready', blocking: [] },
        merge: { state: 'ready', blocking: [] },
      }),
    ).toBeNull();
    // An unknown state value is not a capability: the payload is unknown.
    expect(
      readiness.parseCapabilities({
        read_only: { state: 'ready', blocking: [] },
        execution: { state: 'maybe', blocking: [] },
        merge: { state: 'ready', blocking: [] },
      }),
    ).toBeNull();
    // A blocker without a component name is not trustworthy either.
    expect(
      readiness.parseCapabilities({
        read_only: { state: 'ready', blocking: [{ component: 'x' }] },
        execution: { state: 'ready', blocking: [] },
        merge: { state: 'ready', blocking: [] },
      }),
    ).toBeNull();

    const parsed = readiness.parseCapabilities(PRODUCTION_CAPABILITIES);
    expect(parsed?.execution.backends.codex.state).toBe('unavailable');
  });
});
