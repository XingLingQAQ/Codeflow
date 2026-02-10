import { get, post } from '../api';
import { API_ENDPOINTS } from '../api';

const BASE = API_ENDPOINTS.search;

export interface SearchResult {
  id: string;
  content: string;
  score: number;
  source: string;
  metadata?: Record<string, unknown>;
}

export function vectorSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${BASE}/vector`, { query, ...params }, signal);
}

export function fulltextSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${BASE}/fulltext`, { query, ...params }, signal);
}

export function graphSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${BASE}/graph`, { query, ...params }, signal);
}

export function hybridSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${BASE}/hybrid`, { query, ...params }, signal);
}
