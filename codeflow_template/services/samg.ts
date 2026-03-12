import { get, post, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { SAMGTriple, SAMGEntity, SAMGPointer, QueryMemoryResponse, SAMGStats } from '../types';

const getBase = () => API_ENDPOINTS.samg;

export async function getTriples(signal?: AbortSignal) {
  const res = await get<{ triples: SAMGTriple[]; count: number }>(`${getBase()}/triples`, undefined, signal);
  return res.triples ?? [];
}

export function getTriple(id: string, signal?: AbortSignal) {
  return get<SAMGTriple>(`${getBase()}/triples/${id}`, undefined, signal);
}

export async function getRelations(id: string, signal?: AbortSignal) {
  const res = await get<{ node_id: string; relations: SAMGTriple[]; count: number }>(`${getBase()}/triples/${id}/relations`, undefined, signal);
  return res.relations ?? [];
}

export function addTriples(triples: Partial<SAMGTriple>[], signal?: AbortSignal) {
  return post<{ message: string; count: number }>(`${getBase()}/triples`, { triples }, signal);
}

export function deleteTriples(ids: string[], signal?: AbortSignal) {
  return del<{ message: string; count: number }>(`${getBase()}/triples`, { ids }, signal);
}

export async function getVisibleNodes(signal?: AbortSignal) {
  const res = await get<{ nodes: SAMGEntity[]; count: number }>(`${getBase()}/nodes/visible`, undefined, signal);
  return res.nodes ?? [];
}

export async function getHiddenNodes(signal?: AbortSignal) {
  const res = await get<{ nodes: SAMGEntity[]; count: number }>(`${getBase()}/nodes/hidden`, undefined, signal);
  return res.nodes ?? [];
}

export async function getTopNodes(n?: number, signal?: AbortSignal) {
  const res = await get<{ nodes: SAMGEntity[]; count: number }>(`${getBase()}/nodes/top`, n ? { n } : undefined, signal);
  return res.nodes ?? [];
}

export function getSAMGStats(signal?: AbortSignal) {
  return get<SAMGStats>(`${getBase()}/stats`, undefined, signal);
}

// --- Pointer System ---

export function queryMemory(
  req: { topic: string; type?: string; max_results?: number; resolve_pointers?: boolean; min_bla?: number },
  signal?: AbortSignal,
) {
  return post<QueryMemoryResponse>(`${getBase()}/query-memory`, req, signal);
}

export async function getNodePointers(nodeId: string, signal?: AbortSignal) {
  const res = await get<{ node_id: string; pointers: SAMGPointer[]; count: number }>(
    `${getBase()}/nodes/${encodeURIComponent(nodeId)}/pointers`,
    undefined,
    signal,
  );
  return res.pointers ?? [];
}

export function addNodePointer(
  nodeId: string,
  pointer: {
    source_id: string;
    source_type: string;
    summary?: string;
    line_range?: string;
    file_path?: string;
    relevance?: number;
  },
  signal?: AbortSignal,
) {
  return post<{ message: string }>(`${getBase()}/nodes/${encodeURIComponent(nodeId)}/pointers`, pointer, signal);
}

export function extractWithPointers(
  req: {
    content: string;
    session_id?: string;
    message_index?: number;
    agent_role?: string;
    raw_archive_id: string;
  },
  signal?: AbortSignal,
) {
  return post<{ triples: SAMGTriple[]; count: number }>(`${getBase()}/extract-with-pointers`, req, signal);
}
