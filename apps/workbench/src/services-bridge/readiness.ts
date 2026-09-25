import { getApiBase } from '../../api';
import { refreshBackendConnection } from './connection';
import { authHeadersFor, handleAuthHttpStatus } from './authProvider';

export interface ReadinessComponent {
  ready: boolean;
  required: boolean;
}

export interface Readiness {
  reachable: boolean;
  status: 'ready' | 'not_ready' | 'unreachable';
  version?: string;
  components: Record<string, ReadinessComponent>;
}

/**
 * Raw GET /ready — unlike the shared api client we must read the body even on a
 * 503 (backend up but not ready) so the startup checklist can show per-component
 * progress instead of a single failure.
 */
export async function fetchReadiness(signal?: AbortSignal): Promise<Readiness> {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const { jitter } = await import('../mocks');
      await jitter();
      return {
        reachable: true,
        status: 'ready',
        version: 'mock-dev',
        components: {
          planner: { ready: true, required: true },
          project: { ready: true, required: true },
          context: { ready: true, required: true },
          audit: { ready: true, required: false },
          hooks: { ready: true, required: false },
          floweng: { ready: true, required: false },
          workspace: { ready: true, required: false },
          guard: { ready: true, required: false },
          skill: { ready: true, required: false },
        },
      };
    }
  }
  try {
    const url = `${getApiBase()}/ready`;
    const resp = await fetch(url, { method: 'GET', signal, headers: authHeadersFor(url) });
    if (resp.status === 401 || resp.status === 403) {
      // 401: token stale — rebind runs inside the handler; report unreachable
      // this round so the next poll uses the fresh pairing. 403: reachable
      // but denied — not_ready, and the permission latch drives the UI state.
      await handleAuthHttpStatus(url, resp.status);
      return {
        reachable: resp.status === 403,
        status: resp.status === 403 ? 'not_ready' : 'unreachable',
        components: {},
      };
    }
    const body = (await resp.json().catch(() => null)) as
      | { data?: Record<string, unknown>; status?: string; components?: unknown }
      | null;
    const data = (body?.data ?? body ?? {}) as {
      status?: string;
      version?: string;
      components?: Record<string, { ready?: boolean; Ready?: boolean; required?: boolean; Required?: boolean }>;
    };
    const rawComponents = data.components ?? {};
    const components: Record<string, ReadinessComponent> = {};
    for (const [key, value] of Object.entries(rawComponents)) {
      components[key] = {
        ready: Boolean(value?.ready ?? value?.Ready),
        required: Boolean(value?.required ?? value?.Required),
      };
    }
    return {
      reachable: true,
      status: data.status === 'ready' ? 'ready' : 'not_ready',
      version: data.version,
      components,
    };
  } catch {
    // The base URL stopped answering: re-check the sidecar pairing once so an
    // exited sidecar flips the connection model to unavailable (and a
    // restarted one re-pairs) instead of this page holding a stale base.
    await refreshBackendConnection();
    return { reachable: false, status: 'unreachable', components: {} };
  }
}
