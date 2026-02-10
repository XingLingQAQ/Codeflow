import { get, put } from '../api';
import { API_ENDPOINTS } from '../api';
import type { GlobalConfig } from '../types';

const BASE = API_ENDPOINTS.config;

export function getGlobalConfig(signal?: AbortSignal) {
  return get<GlobalConfig>(`${BASE}/global`, undefined, signal);
}

export function updateGlobalConfig(input: Partial<GlobalConfig>, signal?: AbortSignal) {
  return put<GlobalConfig>(`${BASE}/global`, input, signal);
}

export function getSessionConfig(sessionId: string, signal?: AbortSignal) {
  return get<unknown>(`${BASE}/sessions/${sessionId}`, undefined, signal);
}

export function updateSessionConfig(sessionId: string, input: unknown, signal?: AbortSignal) {
  return put<unknown>(`${BASE}/sessions/${sessionId}`, input, signal);
}

export function resolveConfig(params?: Record<string, string>, signal?: AbortSignal) {
  return get<unknown>(`${BASE}/resolve`, params, signal);
}
