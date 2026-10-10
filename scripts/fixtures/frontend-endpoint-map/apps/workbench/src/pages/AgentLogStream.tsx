// Named import + call: the live caller of the SSE bridge function, so the
// stream endpoint reads bridge_only (no legacy client, one bridge caller).
// The same file also calls the old client for the chunked body endpoint with
// an *aliased* named import (`readAgentLogChunks as readChunks`): both call
// shapes must be recognised as callers, or T12.03.b would call that client
// unused while this page still reads from it.
import { streamAgentLogs } from '../services-bridge/agents';
import { readAgentLogChunks as readChunks } from '../../services/log_stream';

export function AgentLogStream({ agentId }: { agentId: string }) {
  const source = streamAgentLogs(agentId);
  void source;
  return <pre>log stream</pre>;
}

export async function loadRawLogs(agentId: string): Promise<string[]> {
  return readChunks(agentId);
}
