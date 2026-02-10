import { get, post, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { SAMGTriple, SAMGEntity } from '../types';

const BASE = API_ENDPOINTS.samg;

export function getTriples(signal?: AbortSignal) {
  return get<SAMGTriple[]>(`${BASE}/triples`, undefined, signal);
}

export function getTriple(id: string, signal?: AbortSignal) {
  return get<SAMGTriple>(`${BASE}/triples/${id}`, undefined, signal);
}

export function getRelations(id: string, signal?: AbortSignal) {
  return get<SAMGTriple[]>(`${BASE}/triples/${id}/relations`, undefined, signal);
}

export function addTriples(triples: Partial<SAMGTriple>[], signal?: AbortSignal) {
  return post<SAMGTriple[]>(`${BASE}/triples`, { triples }, signal);
}

export function deleteTriples(ids: string[], signal?: AbortSignal) {
  return del<{ deleted: number }>(`${BASE}/triples`, signal);
}

export function getVisibleNodes(signal?: AbortSignal) {
  return get<SAMGEntity[]>(`${BASE}/nodes/visible`, undefined, signal);
}

export function getHiddenNodes(signal?: AbortSignal) {
  return get<SAMGEntity[]>(`${BASE}/nodes/hidden`, undefined, signal);
}

export function getTopNodes(signal?: AbortSignal) {
  return get<SAMGEntity[]>(`${BASE}/nodes/top`, undefined, signal);
}

export function getSAMGStats(signal?: AbortSignal) {
  return get<{ triple_count: number; entity_count: number; predicate_count: number }>(`${BASE}/stats`, undefined, signal);
}
