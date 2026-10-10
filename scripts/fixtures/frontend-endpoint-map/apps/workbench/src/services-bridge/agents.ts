// Bridge layer: GET /api/v1/agents/{id}/logs/stream arrives by SSE, not JSON.
// The AgentLogStream page calls it, so the endpoint is bridge_only.
import { getApiBase } from '../../api';

export async function streamAgentLogs(agentId: string): Promise<EventSource> {
  return new EventSource(`${getApiBase()}/api/v1/agents/${agentId}/logs/stream`);
}
