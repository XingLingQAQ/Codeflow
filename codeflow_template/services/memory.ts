import { get, post, patch, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { MemoryItem, MemoryListResponse, MemoryStatus, MemoryType, MemorySource } from '../types';

const getBase = () => API_ENDPOINTS.memory;

export async function getMemoryItems(
  params?: { type?: MemoryType; session_id?: string; status?: MemoryStatus; sort_by?: 'timestamp' | 'heat' | 'surprise'; order?: 'asc' | 'desc'; limit?: number; offset?: number },
  signal?: AbortSignal,
) {
  const res = await get<MemoryListResponse>(`${getBase()}/items`, params, signal);
  return res.items ?? [];
}

export function createMemoryItem(
  input: { content: string; type?: MemoryType; session_id?: string; source?: MemorySource; tags?: string[]; is_permanent?: boolean },
  signal?: AbortSignal,
) {
  return post<MemoryItem>(`${getBase()}/items`, input, signal);
}

export function updateMemoryItem(
  id: string,
  input: { tags?: string[]; is_permanent?: boolean },
  signal?: AbortSignal,
) {
  return patch<MemoryItem>(`${getBase()}/items/${id}`, input, signal);
}

export function deleteMemoryItem(id: string, signal?: AbortSignal) {
  return del<{ deleted: boolean; id: string }>(`${getBase()}/items/${id}`, signal);
}

export function archiveMemoryItem(id: string, signal?: AbortSignal) {
  return post<MemoryItem>(`${getBase()}/items/${id}/archive`, undefined, signal);
}

export function restoreMemoryItem(id: string, signal?: AbortSignal) {
  return post<MemoryItem>(`${getBase()}/items/${id}/restore`, undefined, signal);
}
