import { post, API_ENDPOINTS } from '../api';
import type {
  MemoryAgentContextResult,
  MemoryAgentRetrieveResult,
  RawEntryType,
} from '../types';

const getBase = () => API_ENDPOINTS.memoryAgent;

export type MemoryAgentSourceRole = 'user' | 'assistant' | 'system';

export interface MemoryAgentIngestRequest {
  content: string;
  type?: RawEntryType;
  session_id?: string;
  source?: MemoryAgentSourceRole;
  tags?: string[];
  metadata?: Record<string, unknown>;
}

export interface MemoryAgentRetrieveRequest {
  query: string;
  max_results?: number;
  min_heat?: number;
  tier?: 'hot' | 'warm' | 'cold';
  session_id?: string;
}

export interface MemoryAgentContextRequest {
  session_id: string;
  query?: string;
  max_tokens?: number;
}

export interface MemoryAgentIngestResult {
  raw_archive_id: string;
  atomic_memory_id: string;
  samg_triples_count?: number;
}

export function ingestMemoryAgent(
  input: MemoryAgentIngestRequest,
  signal?: AbortSignal,
) {
  return post<MemoryAgentIngestResult>(`${getBase()}/ingest`, input, signal);
}

export function retrieveMemoryAgent(
  input: MemoryAgentRetrieveRequest,
  signal?: AbortSignal,
) {
  return post<MemoryAgentRetrieveResult>(`${getBase()}/retrieve`, input, signal);
}

export function buildMemoryAgentContext(
  input: MemoryAgentContextRequest,
  signal?: AbortSignal,
) {
  return post<MemoryAgentContextResult>(`${getBase()}/context`, input, signal);
}
