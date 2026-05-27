import { get } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Agent } from '../types';

const getBase = () => API_ENDPOINTS.agents;

export async function listAgents(signal?: AbortSignal) {
  const response = await get<{ agents: Agent[]; total: number }>(getBase(), undefined, signal);
  return response.agents ?? [];
}

export async function getAgentLogs(agentId: string, signal?: AbortSignal) {
  const response = await get<{ logs: unknown[]; total: number; has_more?: boolean }>(`${getBase()}/${agentId}/logs`, undefined, signal);
  return response.logs ?? [];
}
