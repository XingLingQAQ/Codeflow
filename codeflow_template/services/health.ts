import { get, getApiBase } from '../api';

export function healthCheck(signal?: AbortSignal) {
  return get<{ status: string }>(`${getApiBase()}/health`, undefined, signal);
}
