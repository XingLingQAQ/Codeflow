import { get } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Agent } from '../types';

const BASE = API_ENDPOINTS.agents;

export function listAgents(signal?: AbortSignal) {
  return get<Agent[]>(BASE, undefined, signal);
}

export function getAgentLogs(agentId: string, signal?: AbortSignal) {
  return get<unknown[]>(`${BASE}/${agentId}/logs`, undefined, signal);
}
