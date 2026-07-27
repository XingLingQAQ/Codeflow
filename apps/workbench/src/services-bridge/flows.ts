// Flow engine client (experimental API /api/v1/flows). Reuses the dynamic
// api base + envelope handling from apps/workbench/api.ts.
import { get, post, getApiBase } from '../../api';

export type FlowStatus = 'active' | 'completed' | 'aborted';
export type StageStatus = 'pending' | 'active' | 'waiting_gate' | 'done' | 'skipped';
export type StageType =
  | 'idea'
  | 'design'
  | 'planning'
  | 'research'
  | 'coding'
  | 'review'
  | 'submit'
  | 'import'
  | 'comprehension';

export interface Gate {
  id: string;
  phase: 'enter' | 'exit';
  kind: string;
  on_fail?: string;
  passed: boolean;
}

export interface Stage {
  id: string;
  type: StageType;
  name: string;
  canvas: string;
  status: StageStatus;
  optional: boolean;
  snapshot_id?: string;
  gates?: Gate[];
  order: number;
}

export interface Artifact {
  id: string;
  stage_id: string;
  type: string;
  version: number;
  status: string;
  content_ref?: string;
  created_at: string;
}

export interface FlowEvent {
  id: string;
  type: string;
  stage_id?: string;
  message: string;
  timestamp: string;
}

export interface Flow {
  id: string;
  project_id: string;
  template_id: string;
  status: FlowStatus;
  stages: Stage[];
  loops?: { from: StageType; to: StageType }[];
  artifacts?: Artifact[];
  events?: FlowEvent[];
  created_at: string;
  updated_at: string;
}

export interface FlowTemplateInfo {
  id: string;
  name?: string;
  description?: string;
  stages?: { type: StageType; name?: string; optional?: boolean }[];
  [k: string]: unknown;
}

const base = () => `${getApiBase()}/api/v1/flows`;

export async function listFlows(projectId?: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore, showMockNotice } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      showMockNotice();
      const store = await getMockStore();
      const items = projectId
        ? store.MOCK_FLOWS.filter((f) => f.project_id === projectId)
        : [...store.MOCK_FLOWS];
      return { items, total: items.length };
    }
  }
  return get<{ items: Flow[]; total: number }>(base(), { project_id: projectId }, signal);
}

export async function getFlow(id: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const f = store.MOCK_FLOWS.find((fl) => fl.id === id);
      if (!f) throw new Error('flow not found');
      return f;
    }
  }
  return get<Flow>(`${base()}/${id}`, undefined, signal);
}

export async function createFlow(
  input: { project_id: string; template_id?: string; session_id?: string },
  signal?: AbortSignal,
) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const tpl = store.MOCK_TEMPLATES.find((t) => t.id === (input.template_id ?? 'new_project')) ?? store.MOCK_TEMPLATES[0];
      const newFlow: Flow = {
        id: 'flow-mock-' + Math.random().toString(36).slice(2, 8),
        project_id: input.project_id,
        template_id: tpl.id,
        status: 'active',
        stages: (tpl.stages ?? []).map((s, i) => ({
          id: 'ms-' + i,
          type: s.type as StageType,
          name: s.name ?? s.type,
          canvas: s.type,
          status: i === 0 ? 'active' as const : 'pending' as const,
          optional: !!s.optional,
          order: i,
          gates: [],
        })),
        artifacts: [],
        events: [],
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      (store.MOCK_FLOWS as Flow[]).unshift(newFlow);
      return newFlow;
    }
  }
  return post<Flow>(base(), input, signal);
}

export async function listFlowTemplates(signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore, showMockNotice } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      showMockNotice();
      const store = await getMockStore();
      const items = [...store.MOCK_TEMPLATES];
      return { items, ids: items.map((t) => t.id), total: items.length };
    }
  }
  return get<{ items: FlowTemplateInfo[]; ids: string[]; total: number }>(`${base()}/templates`, undefined, signal);
}

export function advanceStage(flowId: string, stageId: string, signal?: AbortSignal) {
  return post<Flow>(`${base()}/${flowId}/stages/${stageId}/advance`, {}, signal);
}

export function skipStage(flowId: string, stageId: string, signal?: AbortSignal) {
  return post<Flow>(`${base()}/${flowId}/stages/${stageId}/skip`, {}, signal);
}

export function loopFlow(
  flowId: string,
  fromStageId: string,
  toStageId: string,
  reason?: string,
  signal?: AbortSignal,
) {
  return post<Flow>(`${base()}/${flowId}/loop`, { from_stage_id: fromStageId, to_stage_id: toStageId, reason }, signal);
}
