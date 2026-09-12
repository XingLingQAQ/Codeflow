import { describe, expect, it, vi } from 'vitest';
import { filterAvailableAgents, type AgentInfo } from '../services-bridge/agents';
import { useChatStore } from '../stores/chat';
import { agentSelectionKey, resolveAgentSelection } from './agentSelection';

const agents: AgentInfo[] = [
  {
    id: 'flow-agent',
    name: 'Flow Agent',
    role_base: 'coder',
    stage_tags: ['coding'],
    enabled: true,
  },
  {
    id: 'manual-agent',
    name: 'Manual Agent',
    role_base: 'critic',
    stage_tags: ['review'],
    enabled: true,
  },
  {
    id: 'disabled-agent',
    name: 'Disabled Agent',
    role_base: 'coder',
    stage_tags: ['coding'],
    enabled: false,
  },
];

describe('resolveAgentSelection', () => {
  it('uses a project-stage user choice before the flow assignment', () => {
    const selection = resolveAgentSelection({
      agents,
      chosenId: 'manual-agent',
      assignedAgentId: 'flow-agent',
      stage: 'coding',
    });
    expect(selection.agent?.id).toBe('manual-agent');
    expect(selection.source).toBe('user');
  });

  it('uses the flow assignment when there is no explicit user choice', () => {
    const selection = resolveAgentSelection({ agents, assignedAgentId: 'flow-agent', stage: 'coding' });
    expect(selection.agent?.id).toBe('flow-agent');
    expect(selection.source).toBe('flow');
  });

  it('falls back visibly when the assigned agent is disabled or missing', () => {
    const disabled = resolveAgentSelection({ agents, assignedAgentId: 'disabled-agent', stage: 'coding' });
    expect(disabled.source).toBe('fallback');
    expect(disabled.agent?.id).toBe('flow-agent');
    expect(disabled.notice).toContain('已停用');

    const missing = resolveAgentSelection({ agents, assignedAgentId: 'deleted-agent', stage: 'coding' });
    expect(missing.source).toBe('fallback');
    expect(missing.notice).toContain('已删除或停用');
  });
});

describe('agent availability and memory isolation', () => {
  it('excludes disabled agents from stage candidates', () => {
    expect(filterAvailableAgents(agents, 'coding').map((agent) => agent.id)).toEqual(['flow-agent']);
  });

  it('stores explicit choices independently for each project and stage', () => {
    const persistWarning = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const persistWarn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    useChatStore.setState({ agentByStage: {} });
    useChatStore.getState().setAgentForStage('project-a', 'coding', 'flow-agent');
    useChatStore.getState().setAgentForStage('project-b', 'coding', 'manual-agent');
    useChatStore.getState().setAgentForStage('project-a', 'review', 'manual-agent');

    const remembered = useChatStore.getState().agentByStage;
    expect(remembered[agentSelectionKey('project-a', 'coding')]).toBe('flow-agent');
    expect(remembered[agentSelectionKey('project-b', 'coding')]).toBe('manual-agent');
    expect(remembered[agentSelectionKey('project-a', 'review')]).toBe('manual-agent');

    useChatStore.getState().setAgentForStage('project-a', 'coding', undefined);
    expect(useChatStore.getState().agentByStage[agentSelectionKey('project-a', 'coding')]).toBeUndefined();
    expect(useChatStore.getState().agentByStage[agentSelectionKey('project-b', 'coding')]).toBe('manual-agent');
    persistWarning.mockRestore();
    persistWarn.mockRestore();
  });
});
