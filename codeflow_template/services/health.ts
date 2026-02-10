import { get } from '../api';
import { API_BASE } from '../api';

export function healthCheck(signal?: AbortSignal) {
  return get<{ status: string }>(`${API_BASE}/health`, undefined, signal);
}
