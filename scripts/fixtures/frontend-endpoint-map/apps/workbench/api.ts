// Fixture shared transport: the shape apps/workbench/api.ts has (get/post/...
// add the origin-gated bearer and unwrap the {success,data} envelope), trimmed
// to what the ledger reads. No real dependency is imported.
export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

export function getApiBase(): string {
  return '';
}

export function getWsBase(): string {
  return '';
}

const token = '';

export function authHeadersFor(url: string): Record<string, string> {
  return url && token ? { Authorization: `Bearer ${token}` } : {};
}

export function handleAuthHttpStatus(url: string, status: number): void {
  void url;
  void status;
}

async function request<T>(url = '', options: RequestInit = {}, signal?: AbortSignal): Promise<T> {
  const mergedOptions: RequestInit = {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeadersFor(url),
      ...options.headers,
    },
    signal,
  };
  const response = await fetch(url, mergedOptions);
  if (response.status === 401 || response.status === 403) {
    await handleAuthHttpStatus(url, response.status);
  }
  const body = (await response.json()) as ApiResponse<T>;
  if (!response.ok || !body.success) {
    throw new Error(body.error || `HTTP ${response.status}`);
  }
  return body.data as T;
}

export function get<T>(url: string, params?: Record<string, string | number | undefined>, signal?: AbortSignal): Promise<T> {
  void params;
  return request<T>(url, { method: 'GET' }, signal);
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

export const API_ENDPOINTS = {
  get projects() {
    return `${getApiBase()}/api/v1/projects`;
  },
  get memory() {
    return `${getApiBase()}/api/v1/memory`;
  },
};

// Deliberately NOT a string: the legacy WS entry reads `WS_ENDPOINTS.events`,
// and a value the extractor cannot turn into a path must end up in
// unresolved_imports / unknown rather than being guessed into a route.
export const WS_ENDPOINTS = {
  get events(): string {
    return readRuntimeEndpoint('CODEFLOW_WS_EVENTS') ?? '';
  },
};

declare function readRuntimeEndpoint(name: string): string | null;
