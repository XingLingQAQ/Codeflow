// --- API Configuration (backend connection model; T0.07.c) ---
//
// Base URLs derive from services-bridge/connection: in Tauri they come from
// the sidecar handshake exposed by `get_backend_connection`, in a browser
// from VITE_API_BASE or the page origin. While the connection is pending or
// unavailable the bases are empty — there is no default-port fallback and no
// probing of localhost services.

import { getBackendConnectionSnapshot, initBackendConnection } from './src/services-bridge/connection';
import { authHeadersFor, handleAuthHttpStatus } from './src/services-bridge/authProvider';

export function getApiBase(): string {
  return getBackendConnectionSnapshot().baseUrl ?? '';
}

export function getWsBase(): string {
  return httpToWs(getApiBase());
}

function httpToWs(base: string): string {
  if (base.startsWith('https://')) return `wss://${base.slice('https://'.length)}`;
  if (base.startsWith('http://')) return `ws://${base.slice('http://'.length)}`;
  return base;
}

/**
 * Resolve the backend connection once at app startup (before first render).
 * Times out into `unavailable`; StartupGate/readiness surface that state.
 */
export async function initApiBase(): Promise<void> {
  await initBackendConnection();
}

/** Computed endpoint helpers, always using the current connection base. */
export const API_ENDPOINTS = {
  get health() { return `${getApiBase()}/health`; },
  get projects() { return `${getApiBase()}/api/v1/projects`; },
  get workflows() { return `${getApiBase()}/api/v1/workflows`; },
  get memory() { return `${getApiBase()}/api/v1/memory`; },
  get memoryAgent() { return `${getApiBase()}/api/v1/memory/agent`; },
  get search() { return `${getApiBase()}/api/v1/search`; },
  get context() { return `${getApiBase()}/api/v1/context`; },
  get agents() { return `${getApiBase()}/api/v1/agents`; },
  get conversations() { return `${getApiBase()}/api/v1/conversations`; },
  get blackboard() { return `${getApiBase()}/api/v1/blackboard`; },
  get debates() { return `${getApiBase()}/api/v1/debates`; },
  get plans() { return `${getApiBase()}/api/v1/plans`; },
  get plugins() { return `${getApiBase()}/api/v1/plugins`; },
  get config() { return `${getApiBase()}/api/v1/config`; },
  get summarize() { return `${getApiBase()}/api/v1/summarize`; },
  get audit() { return `${getApiBase()}/api/v1/audit`; },
  get privacy() { return `${getApiBase()}/api/v1/privacy`; },
  get isolation() { return `${getApiBase()}/api/v1/isolation`; },
  get samg() { return `${getApiBase()}/api/v1/samg`; },
  get hooks() { return `${getApiBase()}/api/v1/hooks`; },
  get votes() { return `${getApiBase()}/api/v1/votes`; },
};

export const WS_ENDPOINTS = {
  get events() { return `${getWsBase()}/ws`; },
  debate: (id: string) => `${getWsBase()}/api/v1/debates/${id}/stream`,
  conversation: (sessionId: string) => `${getWsBase()}/api/v1/conversations/${sessionId}/stream`,
};

// --- Error Types ---

export class NetworkError extends Error {
  constructor(message: string, public readonly cause?: unknown) {
    super(message);
    this.name = 'NetworkError';
  }
}

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly serverError?: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

// --- Response Types ---

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

// --- HTTP Client ---

async function request<T>(
  url: string,
  options: RequestInit = {},
  signal?: AbortSignal,
): Promise<T> {
  console.log(`[CodeFlow API] ${options.method || 'GET'} ${url}`);
  const mergedOptions: RequestInit = {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      // Bearer only when the origin gate allows (same-origin + live token);
      // cross-origin targets (CDN / model providers) get nothing. The token
      // never enters the URL — the log line above stays safe.
      ...authHeadersFor(url),
      ...options.headers,
    },
    signal,
  };

  let response: Response;
  try {
    response = await fetch(url, mergedOptions);
  } catch (err: unknown) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err; // re-throw abort as-is
    }
    throw new NetworkError(
      `Network request failed: ${err instanceof Error ? err.message : String(err)}`,
      err,
    );
  }

  // Auth failures on the pairing origin: 401 clears + re-pairs the stale
  // connection, 403 latches the permission-failure state. Cross-origin
  // statuses are left untouched (the gate inside no-ops for them).
  if (response.status === 401 || response.status === 403) {
    await handleAuthHttpStatus(url, response.status);
  }

  // Parse JSON body
  let body: ApiResponse<T>;
  try {
    body = await response.json();
  } catch {
    if (!response.ok) {
      throw new ApiError(`HTTP ${response.status}`, response.status);
    }
    return undefined as unknown as T;
  }

  if (!response.ok || !body.success) {
    throw new ApiError(
      body.error || `HTTP ${response.status}`,
      response.status,
      body.error,
    );
  }

  return body.data as T;
}

// --- Public HTTP Methods ---

function buildUrl(base: string, params?: Record<string, string | number | undefined>): string {
  if (!params) return base;
  const searchParams = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      searchParams.set(key, String(value));
    }
  }
  const qs = searchParams.toString();
  return qs ? `${base}?${qs}` : base;
}

export function get<T>(url: string, params?: Record<string, string | number | undefined>, signal?: AbortSignal): Promise<T> {
  return request<T>(buildUrl(url, params), { method: 'GET' }, signal);
}

export function post<T>(url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  return request<T>(url, { method: 'POST', body: body != null ? JSON.stringify(body) : undefined }, signal);
}

export function put<T>(url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  return request<T>(url, { method: 'PUT', body: body != null ? JSON.stringify(body) : undefined }, signal);
}

export function patch<T>(url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  return request<T>(url, { method: 'PATCH', body: body != null ? JSON.stringify(body) : undefined }, signal);
}

export function del<T>(url: string, bodyOrSignal?: unknown | AbortSignal, signal?: AbortSignal): Promise<T> {
  const hasBody = bodyOrSignal !== undefined && !(bodyOrSignal instanceof AbortSignal);
  const resolvedSignal = bodyOrSignal instanceof AbortSignal ? bodyOrSignal : signal;
  return request<T>(
    url,
    { method: 'DELETE', body: hasBody ? JSON.stringify(bodyOrSignal) : undefined },
    resolvedSignal,
  );
}
