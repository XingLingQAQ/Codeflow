import { get, post, put, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { AtomicMemory, MemoryTier } from '../types';

const getBase = () => API_ENDPOINTS.memory;

export function createAtomicMemory(
  input: { content: string; tags?: string[]; session_id?: string; source?: string },
  signal?: AbortSignal,
) {
  return post<AtomicMemory>(`${getBase()}/atomic`, input, signal);
}

export async function searchAtomicMemory(query: string, limit = 20, signal?: AbortSignal) {
  const res = await get<{ memories: AtomicMemory[]; count: number }>(
    `${getBase()}/atomic/search`,
    { query, limit },
    signal,
  );
  return res.memories ?? [];
}

export async function getAtomicMemoriesBySession(sessionId: string, limit = 50, offset = 0, signal?: AbortSignal) {
  const res = await get<{ memories: AtomicMemory[]; count: number }>(
    `${getBase()}/atomic/session/${sessionId}`,
    { limit, offset },
    signal,
  );
  return res.memories ?? [];
}

export async function getAtomicMemoriesByTier(tier: MemoryTier, limit = 50, signal?: AbortSignal) {
  const res = await get<{ memories: AtomicMemory[]; count: number }>(
    `${getBase()}/atomic/tier/${tier}`,
    { limit },
    signal,
  );
  return res.memories ?? [];
}

export function updateAtomicMemory(id: string, updates: Partial<AtomicMemory>, signal?: AbortSignal) {
  return put<AtomicMemory>(`${getBase()}/atomic/${id}`, updates, signal);
}

export function deleteAtomicMemory(id: string, signal?: AbortSignal) {
  return del<{ deleted: boolean }>(`${getBase()}/atomic/${id}`, signal);
}

export function applyHeatDecay(signal?: AbortSignal) {
  return post<{ affected: number }>(`${getBase()}/atomic/decay`, undefined, signal);
}

export function recomputeTiers(signal?: AbortSignal) {
  return post<{ affected: number }>(`${getBase()}/atomic/recompute-tiers`, undefined, signal);
}

export function boostHeat(id: string, boost = 0.3, signal?: AbortSignal) {
  return post<{ message: string }>(`${getBase()}/atomic/${id}/boost`, { boost }, signal);
}
