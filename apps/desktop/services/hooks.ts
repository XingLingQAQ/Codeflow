import { get } from '../api';
import { API_ENDPOINTS } from '../api';

const getBase = () => API_ENDPOINTS.hooks;

export interface HookSummary {
  name: string;
  type: string;
  enabled: boolean;
  priority: number;
  timeout: string;
  retry_count: number;
  metadata?: Record<string, unknown>;
}

export interface HookEventSummary {
  id: string;
  hook_name: string;
  hook_type: string;
  timestamp: string;
  duration: number;
  success: boolean;
  error?: string;
  input_size: number;
  output_size: number;
  metadata?: Record<string, unknown>;
}

export function listHooks(signal?: AbortSignal) {
  return get<{ hooks: HookSummary[] }>(getBase(), undefined, signal);
}

export function getHookEvents(
  params?: { hook_name?: string; limit?: number; offset?: number },
  signal?: AbortSignal,
) {
  return get<{ events: HookEventSummary[]; total: number; limit: number; offset: number }>(
    `${getBase()}/events`,
    params,
    signal,
  );
}
