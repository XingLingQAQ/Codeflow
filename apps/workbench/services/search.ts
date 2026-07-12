import { get, post } from '../api';
import { API_ENDPOINTS } from '../api';

const getBase = () => API_ENDPOINTS.search;

export interface SearchResult {
  id: string;
  content: string;
  score: number;
  source: string;
  metadata?: Record<string, unknown>;
}

export function vectorSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${getBase()}/vector`, { query, ...params }, signal);
}

export function fulltextSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${getBase()}/fulltext`, { query, ...params }, signal);
}

export function graphSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${getBase()}/graph`, { query, ...params }, signal);
}

export function hybridSearch(query: string, params?: { limit?: number }, signal?: AbortSignal) {
  return post<SearchResult[]>(`${getBase()}/hybrid`, { query, ...params }, signal);
}
