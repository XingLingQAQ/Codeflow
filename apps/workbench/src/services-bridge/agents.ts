// Agent registry client (experimental API /api/v1/agents/registry) with a
// static fallback copy of the five builtin agents for when the registry is
// unreachable (403/503/network). Dev-mock is served from fixtures.
import { get, getApiBase } from '../../api';

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
}

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

function filterByStage(items: AgentInfo[], stage?: string): AgentInfo[] {
  if (!stage) return items;
  return items.filter((a) => a.stage_tags?.includes(stage));
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
      return filterByStage([...store.MOCK_AGENTS] as AgentInfo[], stage);
    }
  }
  try {
    const res = await get<{ items: AgentInfo[]; total: number }>(
      `${getApiBase()}/api/v1/agents/registry`,
      stage ? { stage } : undefined,
      signal,
    );
    return res.items ?? [];
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    return filterByStage(FALLBACK_AGENTS, stage);
  }
}
