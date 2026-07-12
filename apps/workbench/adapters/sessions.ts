import type { Agent } from '../types';
import { toAgentViewModel } from './agents';

export interface SessionViewModel {
  id: string;
  agentId: string;
  sessionId: string;
  title: string;
  subtitleText: string;
  roleText: string;
  roleTone: string;
  statusText: string;
  statusTone: string;
  taskCountText: string;
  tokenCountText: string;
  sessionText: string;
  lastActiveText: string;
  replayTitle: string;
}

export function resolveSessionId(agent: Agent): string {
  return agent.session_id || agent.id;
}

export function toSessionViewModel(agent: Agent): SessionViewModel {
  const card = toAgentViewModel(agent);
  const sessionId = resolveSessionId(agent);

  return {
    id: sessionId,
    agentId: agent.id,
    sessionId,
    title: card.name,
    subtitleText: card.summaryText,
    roleText: card.roleText,
    roleTone: card.roleTone,
    statusText: card.statusText,
    statusTone: card.statusTone,
    taskCountText: card.taskCountText,
    tokenCountText: card.tokenCountText,
    sessionText: `session ${sessionId}`,
    lastActiveText: card.lastActiveText,
    replayTitle: card.name,
  };
}

export function toSessionViewModels(agents: Agent[]): SessionViewModel[] {
  return agents.map(toSessionViewModel);
}
