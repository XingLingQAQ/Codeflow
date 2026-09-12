// Agent registry client (experimental API /api/v1/agents/registry) with a
// static fallback copy of the five builtin agents for when the registry is
// unreachable (403/503/network). Dev-mock is served from fixtures.
import { del, get, getApiBase, patch, post } from '../../api';

export interface AgentBinding {
  model?: string;
  channel?: string;
  temperature?: number;
  max_tokens?: number;
}

export interface AgentMounts {
  mcp_tools?: string[];
  skills?: string[];
}

export interface AgentInfo {
  id: string;
  name: string;
  avatar?: string;
  description?: string;
  role_base: string;
  system_prompt?: string;
  stage_tags?: string[];
  source?: string;
  enabled?: boolean;
  version?: string;
  binding?: AgentBinding;
  mounts?: AgentMounts;
  stats?: { usage_count: number; score: number };
  created_at?: string;
  updated_at?: string;
}

export interface AgentInput {
  name: string;
  description?: string;
  role_base: string;
  system_prompt?: string;
  binding?: AgentBinding;
  mounts?: AgentMounts;
  stage_tags?: string[];
}

export type AgentUpdateInput = Partial<AgentInput> & { enabled?: boolean };

/** Chinese labels for registry role_base values (UI copy). */
export const ROLE_LABEL: Record<string, string> = {
  main: '主控',
  coder: '编码',
  sub: '子代理',
  critic: '评审',
  researcher: '调研',
};

export function roleLabel(role: string | undefined): string {
  return (role && ROLE_LABEL[role]) || role || 'Agent';
}

/**
 * Frontend static copy of the five builtin registry agents
 * (backend/internal/agent/builtins.go). Used only as a failure fallback, so it
 * mirrors the backend definitions verbatim.
 */
export const FALLBACK_AGENTS: AgentInfo[] = [
  {
    id: 'builtin-flow-conductor',
    name: 'Flow Conductor',
    avatar: '🎯',
    description: 'Orchestrates a flow stage, manages gates and handoffs, and summons debate when needed.',
    role_base: 'main',
    stage_tags: ['planning', 'coding', 'review', 'submit'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-code-artisan',
    name: 'Code Artisan',
    avatar: '🛠️',
    description: 'Writes code under guard constraints with minimal, focused diffs.',
    role_base: 'coder',
    stage_tags: ['coding'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-scout',
    name: 'Scout',
    avatar: '🔍',
    description: 'Fast contextual retrieval and exploration; returns structured findings without modifying anything.',
    role_base: 'sub',
    stage_tags: ['research', 'comprehension', 'planning'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-red-critic',
    name: 'Red Critic',
    avatar: '🛡️',
    description: 'Adversarial reviewer that hunts correctness, security, and performance issues.',
    role_base: 'critic',
    stage_tags: ['review'],
    source: 'builtin',
    enabled: true,
  },
  {
    id: 'builtin-deep-researcher',
    name: 'Deep Researcher',
    avatar: '📚',
    description: 'Multi-source research synthesis with evidence-first citation discipline.',
    role_base: 'researcher',
    stage_tags: ['research'],
    source: 'builtin',
    enabled: true,
  },
];

export function filterAvailableAgents(items: AgentInfo[], stage?: string): AgentInfo[] {
  return items.filter((agent) => {
    if (agent.enabled === false) return false;
    return !stage || agent.stage_tags?.includes(stage);
  });
}

const registryBase = () => `${getApiBase()}/api/v1/agents/registry`;

/** Strict registry listing for management surfaces; backend errors stay visible. */
export async function listAgents(signal?: AbortSignal): Promise<AgentInfo[]> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return [...store.MOCK_AGENTS] as AgentInfo[];
    }
  }
  const res = await get<{ items: AgentInfo[]; total: number }>(registryBase(), undefined, signal);
  return res.items ?? [];
}

export async function createAgent(input: AgentInput, signal?: AbortSignal): Promise<AgentInfo> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const now = new Date().toISOString();
      const item: AgentInfo = {
        ...input,
        id: `agent-mock-${Math.random().toString(36).slice(2, 9)}`,
        source: 'user',
        version: '0.1.0',
        enabled: true,
        stats: { usage_count: 0, score: 0 },
        created_at: now,
        updated_at: now,
      };
      (store.MOCK_AGENTS as AgentInfo[]).unshift(item);
      return item;
    }
  }
  return post<AgentInfo>(registryBase(), input, signal);
}

export async function updateAgent(id: string, input: AgentUpdateInput, signal?: AbortSignal): Promise<AgentInfo> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const items = store.MOCK_AGENTS as AgentInfo[];
      const index = items.findIndex((item) => item.id === id);
      if (index < 0) throw new Error('Agent 不存在');
      if (items[index].source === 'builtin') throw new Error('内置 Agent 不可修改');
      items[index] = { ...items[index], ...input, updated_at: new Date().toISOString() };
      return items[index];
    }
  }
  return patch<AgentInfo>(`${registryBase()}/${encodeURIComponent(id)}`, input, signal);
}

export async function deleteAgent(id: string, signal?: AbortSignal): Promise<void> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const items = store.MOCK_AGENTS as AgentInfo[];
      const index = items.findIndex((item) => item.id === id);
      if (index < 0) throw new Error('Agent 不存在');
      if (items[index].source === 'builtin') throw new Error('内置 Agent 不可删除');
      items.splice(index, 1);
      return;
    }
  }
  await del<{ deleted: boolean }>(`${registryBase()}/${encodeURIComponent(id)}`, signal);
}

/**
 * List registry agents, optionally narrowed to one stage. Never rejects for
 * registry failures — falls back to the builtin static copy instead (abort
 * errors still propagate so React Query cancellation works).
 */
export async function listStageAgents(stage?: string, signal?: AbortSignal): Promise<AgentInfo[]> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return filterAvailableAgents([...store.MOCK_AGENTS] as AgentInfo[], stage);
    }
  }
  try {
    const res = await get<{ items: AgentInfo[]; total: number }>(
      registryBase(),
      stage ? { stage } : undefined,
      signal,
    );
    return filterAvailableAgents(res.items ?? [], stage);
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    return filterAvailableAgents(FALLBACK_AGENTS, stage);
  }
}
