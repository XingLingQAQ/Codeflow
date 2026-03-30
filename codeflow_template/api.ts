// --- API Configuration (dynamic port for browser + Tauri sidecar) ---

const DEFAULT_PORT = 8080;
const FALLBACK_PORT = 18080;

/** Mutable base URLs updated by initApiBase(). */
let _apiBase = `http://localhost:${DEFAULT_PORT}`;
let _wsBase = `ws://localhost:${DEFAULT_PORT}`;
let _initialized = false;

export function getApiBase(): string { return _apiBase; }
export function getWsBase(): string { return _wsBase; }

function normalizeBase(raw: string): string {
  return raw.trim().replace(/\/+$/, '');
}

function httpToWs(base: string): string {
  if (base.startsWith('https://')) return `wss://${base.slice('https://'.length)}`;
  if (base.startsWith('http://')) return `ws://${base.slice('http://'.length)}`;
  return base;
}

function setApiBases(base: string): void {
  const normalized = normalizeBase(base);
  _apiBase = normalized;
  _wsBase = httpToWs(normalized);
}

function getBrowserCandidates(): string[] {
  const envBase = (import.meta as unknown as { env?: { VITE_API_BASE?: string } }).env?.VITE_API_BASE;
  const candidates = [
    envBase ? normalizeBase(envBase) : '',
    `http://localhost:${DEFAULT_PORT}`,
    `http://127.0.0.1:${DEFAULT_PORT}`,
    `http://localhost:${FALLBACK_PORT}`,
    `http://127.0.0.1:${FALLBACK_PORT}`,
  ].filter(Boolean);

  return Array.from(new Set(candidates));
}

async function isCodeFlowBackend(base: string): Promise<boolean> {
  try {
    const resp = await fetch(`${base}/health`, { method: 'GET' });
    if (!resp.ok) return false;

    const body = await resp.json().catch(() => null) as Record<string, unknown> | null;
    if (!body || typeof body !== 'object') return false;

    const topStatus = String(body.status ?? '').toLowerCase();
    const topService = String(body.service ?? '').toLowerCase();

    const nested = typeof body.data === 'object' && body.data !== null
      ? (body.data as Record<string, unknown>)
      : null;
    const nestedStatus = String(nested?.status ?? '').toLowerCase();
    const nestedService = String(nested?.service ?? '').toLowerCase();

    return (
      topStatus === 'healthy' ||
      topService.includes('codeflow') ||
      nestedStatus === 'healthy' ||
      nestedService.includes('codeflow')
    );
  } catch {
    return false;
  }
}

async function resolveBrowserApiBase(): Promise<void> {
  const candidates = getBrowserCandidates();
  for (const base of candidates) {
    if (await isCodeFlowBackend(base)) {
      setApiBases(base);
      console.log(`[CodeFlow] Browser mode backend resolved: ${_apiBase}`);
      return;
    }
  }

  console.warn(`[CodeFlow] Could not resolve backend from candidates, fallback to ${_apiBase}`);
}

/**
 * Detect Tauri environment and resolve the sidecar port.
 * Call once at app startup (before first render).
 */
export async function initApiBase(): Promise<void> {
  if (_initialized) return;
  _initialized = true;

  // Not in Tauri: auto-detect backend in browser mode.
  if (typeof window === 'undefined' || !(window as any).__TAURI_INTERNALS__) {
    await resolveBrowserApiBase();
    return;
  }

  // In Tauri: resolve sidecar port.
  console.log('[CodeFlow] Tauri detected, resolving sidecar port...');
  try {
    const { invoke } = await import('@tauri-apps/api/core');

    // Poll for sidecar port (may not be ready immediately)
    for (let i = 0; i < 20; i++) {
      const port = await invoke<number | null>('get_backend_port');
      if (port) {
        setApiBases(`http://localhost:${port}`);
        console.log(`[CodeFlow] Sidecar port resolved: ${port}, apiBase=${_apiBase}`);
        return;
      }
      console.log(`[CodeFlow] Port not ready, retry ${i + 1}/20...`);
      await new Promise(r => setTimeout(r, 500));
    }
    console.error('[CodeFlow] Could not resolve sidecar port after 10s, using default', DEFAULT_PORT);
  } catch (e) {
    console.error('[CodeFlow] Failed to resolve sidecar port:', e);
  }
}

/** Computed endpoint helpers, always using current _apiBase/_wsBase. */
export const API_ENDPOINTS = {
  get health() { return `${_apiBase}/health`; },
  get projects() { return `${_apiBase}/api/v1/projects`; },
  get workflows() { return `${_apiBase}/api/v1/workflows`; },
  get memory() { return `${_apiBase}/api/v1/memory`; },
  get memoryAgent() { return `${_apiBase}/api/v1/memory/agent`; },
  get search() { return `${_apiBase}/api/v1/search`; },
  get context() { return `${_apiBase}/api/v1/context`; },
  get agents() { return `${_apiBase}/api/v1/agents`; },
  get conversations() { return `${_apiBase}/api/v1/conversations`; },
  get blackboard() { return `${_apiBase}/api/v1/blackboard`; },
  get debates() { return `${_apiBase}/api/v1/debates`; },
  get plans() { return `${_apiBase}/api/v1/plans`; },
  get plugins() { return `${_apiBase}/api/v1/plugins`; },
  get config() { return `${_apiBase}/api/v1/config`; },
  get summarize() { return `${_apiBase}/api/v1/summarize`; },
  get audit() { return `${_apiBase}/api/v1/audit`; },
  get privacy() { return `${_apiBase}/api/v1/privacy`; },
  get isolation() { return `${_apiBase}/api/v1/isolation`; },
  get samg() { return `${_apiBase}/api/v1/samg`; },
  get hooks() { return `${_apiBase}/api/v1/hooks`; },
  get votes() { return `${_apiBase}/api/v1/votes`; },
};

export const WS_ENDPOINTS = {
  get events() { return `${_wsBase}/ws`; },
  debate: (id: string) => `${_wsBase}/api/v1/debates/${id}/stream`,
  conversation: (sessionId: string) => `${_wsBase}/api/v1/conversations/${sessionId}/stream`,
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
