import { getApiBase } from '../../api';
import { refreshBackendConnection } from './connection';
import { authHeadersFor, handleAuthHttpStatus } from './authProvider';
import type { ReadinessBlocker, ReadinessCapability } from '../../generated/openapi-types';

export interface ReadinessComponent {
  ready: boolean;
  required: boolean;
}

/**
 * The capability sets GET /ready publishes under `data.capabilities` (plan
 * section 15 T0.12 step 3), in the shape the generated OpenAPI types describe:
 * read_only and merge are {state, blocking[]}, execution nests one entry per
 * backend under `backends`. The generated `ReadinessCapabilities.execution`
 * types backends as `Record<string, unknown>` (the OpenAPI schema composes it
 * with allOf), so this view re-types the per-backend map as a capability.
 */
export interface ReadinessCapabilitiesView {
  read_only: ReadinessCapability;
  execution: ReadinessCapability & { backends: Record<string, ReadinessCapability> };
  merge: ReadinessCapability;
}

export interface Readiness {
  reachable: boolean;
  status: 'ready' | 'not_ready' | 'unreachable';
  version?: string;
  components: Record<string, ReadinessComponent>;
  /**
   * Absent when the backend did not publish capabilities (an older build) or
   * published something unparseable. Consumers must treat that as "unknown",
   * never as "ready": a missing set is no evidence that a Run may be created.
   */
  capabilities?: ReadinessCapabilitiesView;
}

const CAPABILITY_STATES = new Set(['ready', 'unavailable']);
const BLOCKER_STATES = new Set(['ready', 'degraded', 'failed', 'not_configured']);

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function optionalString(value: unknown): string | null {
  if (value === undefined || value === null) return '';
  return typeof value === 'string' ? value : null;
}

/** Parse one blocker; null when the entry cannot be trusted. */
function parseBlocker(raw: unknown): ReadinessBlocker | null {
  const obj = asRecord(raw);
  if (!obj) return null;
  const component = optionalString(obj.component);
  if (!component) return null;
  if (typeof obj.state !== 'string' || !BLOCKER_STATES.has(obj.state)) return null;
  const errorCode = optionalString(obj.error_code);
  const remediation = optionalString(obj.remediation);
  if (errorCode === null || remediation === null) return null;
  return {
    component,
    state: obj.state as ReadinessBlocker['state'],
    error_code: errorCode,
    remediation,
  };
}

/**
 * Parse one capability set. Returns null for anything that is not a
 * well-formed {state, blocking[]} pair, so a malformed payload degrades to
 * "capabilities unknown" instead of a guessed verdict.
 */
function parseCapability(raw: unknown): ReadinessCapability | null {
  const obj = asRecord(raw);
  if (!obj) return null;
  if (typeof obj.state !== 'string' || !CAPABILITY_STATES.has(obj.state)) return null;
  // `blocking` is required by the contract (the backend always publishes the
  // key, empty when ready), so a set without it is not trustworthy.
  if (!Array.isArray(obj.blocking)) return null;
  const blocking: ReadinessBlocker[] = [];
  for (const entry of obj.blocking) {
    const blocker = parseBlocker(entry);
    if (!blocker) return null;
    blocking.push(blocker);
  }
  return { state: obj.state as ReadinessCapability['state'], blocking };
}

/** Parse `data.capabilities`; null when absent or unparseable (unknown). */
export function parseCapabilities(raw: unknown): ReadinessCapabilitiesView | null {
  const obj = asRecord(raw);
  if (!obj) return null;
  const readOnly = parseCapability(obj.read_only);
  const merge = parseCapability(obj.merge);
  const execution = parseCapability(obj.execution);
  if (!readOnly || !merge || !execution) return null;
  const rawBackends = asRecord(obj.execution)?.backends;
  const backends: Record<string, ReadinessCapability> = {};
  if (rawBackends !== undefined && rawBackends !== null) {
    const entries = asRecord(rawBackends);
    if (!entries) return null;
    for (const [name, value] of Object.entries(entries)) {
      const backend = parseCapability(value);
      if (!backend) return null;
      backends[name] = backend;
    }
  }
  return {
    read_only: readOnly,
    execution: { ...execution, backends },
    merge,
  };
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
        // Mock mode stands in for a fully wired backend, so the capability
        // strip shows all three states as usable (T0.12.c).
        capabilities: {
          read_only: { state: 'ready', blocking: [] },
          execution: { state: 'ready', blocking: [], backends: { mock: { state: 'ready', blocking: [] } } },
          merge: { state: 'ready', blocking: [] },
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
      capabilities?: unknown;
    };
    const rawComponents = data.components ?? {};
    const components: Record<string, ReadinessComponent> = {};
    for (const [key, value] of Object.entries(rawComponents)) {
      components[key] = {
        ready: Boolean(value?.ready ?? value?.Ready),
        required: Boolean(value?.required ?? value?.Required),
      };
    }
    const capabilities = parseCapabilities(data.capabilities);
    return {
      reachable: true,
      status: data.status === 'ready' ? 'ready' : 'not_ready',
      version: data.version,
      components,
      ...(capabilities ? { capabilities } : {}),
    };
  } catch {
    // The base URL stopped answering: re-check the sidecar pairing once so an
    // exited sidecar flips the connection model to unavailable (and a
    // restarted one re-pairs) instead of this page holding a stale base.
    await refreshBackendConnection();
    return { reachable: false, status: 'unreachable', components: {} };
  }
}
