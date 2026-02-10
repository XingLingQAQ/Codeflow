// --- API Configuration ---
export const API_BASE = 'http://localhost:8080';
export const WS_BASE = 'ws://localhost:8080';

export const API_ENDPOINTS = {
  health: `${API_BASE}/health`,
  projects: `${API_BASE}/api/v1/projects`,
  memory: `${API_BASE}/api/v1/memory`,
  search: `${API_BASE}/api/v1/search`,
  context: `${API_BASE}/api/v1/context`,
  agents: `${API_BASE}/api/v1/agents`,
  conversations: `${API_BASE}/api/v1/conversations`,
  blackboard: `${API_BASE}/api/v1/blackboard`,
  debates: `${API_BASE}/api/v1/debates`,
  plans: `${API_BASE}/api/v1/plans`,
  config: `${API_BASE}/api/v1/config`,
  summarize: `${API_BASE}/api/v1/summarize`,
  audit: `${API_BASE}/api/v1/audit`,
  privacy: `${API_BASE}/api/v1/privacy`,
  isolation: `${API_BASE}/api/v1/isolation`,
  samg: `${API_BASE}/api/v1/samg`,
  hooks: `${API_BASE}/api/v1/hooks`,
  votes: `${API_BASE}/api/v1/votes`,
} as const;

export const WS_ENDPOINTS = {
  events: `${WS_BASE}/ws`,
  debate: (id: string) => `${WS_BASE}/api/v1/debates/${id}/stream`,
  conversation: (sessionId: string) => `${WS_BASE}/api/v1/conversations/${sessionId}/stream`,
} as const;

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

export function del<T>(url: string, signal?: AbortSignal): Promise<T> {
  return request<T>(url, { method: 'DELETE' }, signal);
}
