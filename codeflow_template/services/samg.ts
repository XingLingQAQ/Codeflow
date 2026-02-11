import { get, post, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { SAMGTriple, SAMGEntity, SAMGPointer, QueryMemoryResponse } from '../types';

const BASE = API_ENDPOINTS.samg;

export async function getTriples(signal?: AbortSignal) {
  const res = await get<{ triples: SAMGTriple[]; count: number }>(`${BASE}/triples`, undefined, signal);
  return res.triples ?? [];
}

export function getTriple(id: string, signal?: AbortSignal) {
  return get<SAMGTriple>(`${BASE}/triples/${id}`, undefined, signal);
}

export async function getRelations(id: string, signal?: AbortSignal) {
  const res = await get<{ node_id: string; relations: SAMGTriple[]; count: number }>(`${BASE}/triples/${id}/relations`, undefined, signal);
  return res.relations ?? [];
}

export function addTriples(triples: Partial<SAMGTriple>[], signal?: AbortSignal) {
  return post<{ message: string; count: number }>(`${BASE}/triples`, { triples }, signal);
}

export function deleteTriples(ids: string[], signal?: AbortSignal) {
  return del<{ message: string; count: number }>(`${BASE}/triples`, signal);
}

export async function getVisibleNodes(signal?: AbortSignal) {
  const res = await get<{ nodes: SAMGEntity[]; count: number }>(`${BASE}/nodes/visible`, undefined, signal);
  return res.nodes ?? [];
}

export async function getHiddenNodes(signal?: AbortSignal) {
  const res = await get<{ nodes: SAMGEntity[]; count: number }>(`${BASE}/nodes/hidden`, undefined, signal);
  return res.nodes ?? [];
}

export async function getTopNodes(signal?: AbortSignal) {
  const res = await get<{ nodes: SAMGEntity[]; count: number }>(`${BASE}/nodes/top`, undefined, signal);
  return res.nodes ?? [];
}

export function getSAMGStats(signal?: AbortSignal) {
  return get<{ triple_count: number; entity_count: number; predicate_count: number }>(`${BASE}/stats`, undefined, signal);
}

// --- Pointer System ---

export function queryMemory(
  req: { topic: string; type?: string; max_results?: number; resolve_pointers?: boolean; min_bla?: number },
  signal?: AbortSignal,
) {
  return post<QueryMemoryResponse>(`${BASE}/query-memory`, req, signal);
}

export async function getNodePointers(nodeId: string, signal?: AbortSignal) {
  const res = await get<{ node_id: string; pointers: SAMGPointer[]; count: number }>(
    `${BASE}/nodes/${encodeURIComponent(nodeId)}/pointers`,
    undefined,
    signal,
  );
  return res.pointers ?? [];
}

export function addNodePointer(
  nodeId: string,
  pointer: { source_id: string; source_type: string; summary?: string; relevance?: number },
  signal?: AbortSignal,
) {
  return post<{ message: string }>(`${BASE}/nodes/${encodeURIComponent(nodeId)}/pointers`, pointer, signal);
}

export function extractWithPointers(
  req: { content: string; session_id?: string; raw_archive_id: string },
  signal?: AbortSignal,
) {
  return post<{ triples: SAMGTriple[]; count: number }>(`${BASE}/extract-with-pointers`, req, signal);
}
