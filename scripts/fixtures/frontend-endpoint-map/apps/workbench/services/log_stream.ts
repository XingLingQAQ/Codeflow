// Old client layer, streamed-body entry: the response body is consumed with
// response.body.getReader(), so the ledger lists GET /api/v1/agents/{id}/logs/raw
// under "Non-JSON entries" as stream. AgentLogStream.tsx imports this with an
// alias (`readAgentLogChunks as readChunks`), which must still count as a
// caller.
import { getApiBase } from '../api';

export async function readAgentLogChunks(agentId: string): Promise<string[]> {
  const response = await fetch(`${getApiBase()}/api/v1/agents/${agentId}/logs/raw`);
  const reader = response.body.getReader();
  const chunks: string[] = [];
  for (;;) {
    const result = await reader.read();
    if (result.done) break;
    chunks.push(String(result.value));
  }
  return chunks;
}
