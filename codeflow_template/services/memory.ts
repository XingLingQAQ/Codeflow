import { get, post, patch, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { MemoryItem } from '../types';

const BASE = API_ENDPOINTS.memory;

export function getMemoryItems(signal?: AbortSignal) {
  return get<MemoryItem[]>(`${BASE}/items`, undefined, signal);
}

export function createMemoryItem(input: { content: string; type: string; tags?: string[] }, signal?: AbortSignal) {
  return post<MemoryItem>(`${BASE}/items`, input, signal);
}

export function updateMemoryItem(id: string, input: Partial<MemoryItem>, signal?: AbortSignal) {
  return patch<MemoryItem>(`${BASE}/items/${id}`, input, signal);
}

export function deleteMemoryItem(id: string, signal?: AbortSignal) {
  return del<{ deleted: boolean }>(`${BASE}/items/${id}`, signal);
}

export function archiveMemoryItem(id: string, signal?: AbortSignal) {
  return post<MemoryItem>(`${BASE}/items/${id}/archive`, undefined, signal);
}

export function restoreMemoryItem(id: string, signal?: AbortSignal) {
  return post<MemoryItem>(`${BASE}/items/${id}/restore`, undefined, signal);
}
