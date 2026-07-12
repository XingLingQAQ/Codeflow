import { get, post } from '../api';
import { API_ENDPOINTS } from '../api';
import type { RawEntry, RawEntryType } from '../types';

const getBase = () => API_ENDPOINTS.memory;

export function storeRawEntry(
  entry: { type?: RawEntryType; content: string; metadata?: Record<string, unknown>; session_id?: string },
  signal?: AbortSignal,
) {
  return post<{ id: string }>(`${getBase()}/archive`, entry, signal);
}

export function getRawEntry(id: string, signal?: AbortSignal) {
  return get<RawEntry>(`${getBase()}/archive/${id}`, undefined, signal);
}

export async function searchRawArchive(q: string, limit = 20, signal?: AbortSignal) {
  const res = await get<{ entries: RawEntry[]; count: number }>(
    `${getBase()}/archive/search?q=${encodeURIComponent(q)}&limit=${limit}`,
    undefined,
    signal,
  );
  return res.entries ?? [];
}

export async function listRawArchive(
  opts?: { type?: RawEntryType; session_id?: string; limit?: number; offset?: number },
  signal?: AbortSignal,
) {
  const params = new URLSearchParams();
  if (opts?.type) params.set('type', opts.type);
  if (opts?.session_id) params.set('session_id', opts.session_id);
  if (opts?.limit) params.set('limit', String(opts.limit));
  if (opts?.offset) params.set('offset', String(opts.offset));

  const qs = params.toString();
  const res = await get<{ entries: RawEntry[]; count: number; total: number }>(
    `${getBase()}/archive${qs ? '?' + qs : ''}`,
    undefined,
    signal,
  );
  return res;
}

export function getRawArchiveStats(signal?: AbortSignal) {
  return get<{ total_entries: number }>(`${getBase()}/archive/stats`, undefined, signal);
}
