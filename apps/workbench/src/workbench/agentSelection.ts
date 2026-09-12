import type { AgentInfo } from '../services-bridge/agents';

export type AgentSelectionSource = 'user' | 'flow' | 'recommended' | 'fallback' | 'none';

export interface AgentSelection {
  agent?: AgentInfo;
  source: AgentSelectionSource;
  notice?: string;
}

export function agentSelectionKey(projectId: string, stage: string): string {
  return `${projectId}:${stage}`;
}

export function resolveAgentSelection({
  agents,
  chosenId,
  assignedAgentId,
  stage,
}: {
  agents: AgentInfo[];
  chosenId?: string;
  assignedAgentId?: string;
  stage: string;
}): AgentSelection {
  const available = agents.filter((agent) => agent.enabled !== false);
  const allById = new Map(agents.map((agent) => [agent.id, agent]));
  const availableById = new Map(available.map((agent) => [agent.id, agent]));
  const recommended = available.find((agent) => agent.stage_tags?.includes(stage));
  const fallback = recommended ?? available[0];

  if (chosenId) {
    const chosen = availableById.get(chosenId);
    if (chosen) return { agent: chosen, source: 'user' };
    const known = allById.get(chosenId);
    return {
      agent: fallback,
      source: fallback ? 'fallback' : 'none',
      notice: known
        ? `此前手动选择的 Agent「${known.name}」已停用，现已自动回退。`
        : '此前手动选择的 Agent 已删除或停用，现已自动回退。',
    };
  }

  if (assignedAgentId) {
    const assigned = availableById.get(assignedAgentId);
    if (assigned) return { agent: assigned, source: 'flow' };
    const known = allById.get(assignedAgentId);
    return {
      agent: fallback,
      source: fallback ? 'fallback' : 'none',
      notice: known
        ? `流程指定的 Agent「${known.name}」已停用，现已自动回退。`
        : '流程指定的 Agent 已删除或停用，现已自动回退。',
    };
  }

  if (recommended) return { agent: recommended, source: 'recommended' };
  if (fallback) return { agent: fallback, source: 'fallback' };
  return { source: 'none' };
}
