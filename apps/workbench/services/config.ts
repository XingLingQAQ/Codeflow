import { get, put } from '../api';
import { API_ENDPOINTS } from '../api';
import type { GlobalConfig, ResolvedConfig, SessionConfig } from '../types';

const getBase = () => API_ENDPOINTS.config;

export function getGlobalConfig(signal?: AbortSignal) {
  return get<GlobalConfig>(`${getBase()}/global`, undefined, signal);
}

export function updateGlobalConfig(input: Partial<GlobalConfig>, signal?: AbortSignal) {
  return put<GlobalConfig>(`${getBase()}/global`, input, signal);
}

export function getSessionConfig(sessionId: string, signal?: AbortSignal) {
  return get<SessionConfig>(`${getBase()}/sessions/${sessionId}`, undefined, signal);
}

export function updateSessionConfig(sessionId: string, input: Partial<SessionConfig>, signal?: AbortSignal) {
  return put<SessionConfig>(`${getBase()}/sessions/${sessionId}`, input, signal);
}

export function resolveConfig(params?: Record<string, string>, signal?: AbortSignal) {
  return get<ResolvedConfig>(`${getBase()}/resolve`, params, signal);
}
