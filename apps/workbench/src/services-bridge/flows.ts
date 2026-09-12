// Flow engine client (experimental API /api/v1/flows). Reuses the dynamic
// api base + envelope handling from apps/workbench/api.ts.
import { del, get, post, getApiBase } from '../../api';

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
  agent_id?: string;
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
  source?: 'builtin' | 'custom';
  stages?: FlowTemplateStage[];
  loops?: { from: StageType; to: StageType }[];
  [k: string]: unknown;
}

export interface FlowTemplateGate {
  phase: 'enter' | 'exit';
  kind: 'auto' | 'human_approval' | 'agent_check';
  on_fail?: 'block' | 'escalate_to_debate';
}

export interface FlowTemplateStage {
  type: StageType;
  name: string;
  canvas: string;
  agent_id?: string;
  optional: boolean;
  gates?: FlowTemplateGate[];
}

export interface FlowTemplateInput {
  id: string;
  name: string;
  description?: string;
  stages: FlowTemplateStage[];
  loops?: { from: StageType; to: StageType }[];
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
          canvas: s.canvas ?? s.type,
          agent_id: s.agent_id,
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

export async function saveFlowTemplate(input: FlowTemplateInput, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const templates = store.MOCK_TEMPLATES as FlowTemplateInfo[];
      const index = templates.findIndex((item) => item.id === input.id);
      if (index >= 0 && templates[index].source !== 'custom') {
        throw new Error('内置模板不可修改');
      }
      const item: FlowTemplateInfo = { ...input, source: 'custom' };
      if (index >= 0) templates[index] = item;
      else templates.push(item);
      return { id: input.id };
    }
  }
  return post<{ id: string }>(`${base()}/templates/import`, input, signal);
}

export async function deleteFlowTemplate(id: string, signal?: AbortSignal): Promise<void> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const templates = store.MOCK_TEMPLATES as FlowTemplateInfo[];
      const index = templates.findIndex((item) => item.id === id);
      if (index < 0) throw new Error('模板不存在');
      if (templates[index].source !== 'custom') throw new Error('内置模板不可删除');
      templates.splice(index, 1);
      return;
    }
  }
  await del<{ deleted: boolean }>(`${base()}/templates/${encodeURIComponent(id)}`, signal);
}

// ---- Stage lifecycle (advance / skip / loop / gate decisions) ----
//
// Mock branches mirror backend/internal/floweng/engine.go semantics: advance
// blocks on unpassed human/agent exit gates (stage → waiting_gate), snapshots
// the completed stage, then activates the next pending stage and applies its
// enter gates; loop resets later stages and marks artifacts stale. Mutations
// clone-and-replace the fixture flow so React Query consumers see fresh
// object identities after invalidation.

type MockStore = Awaited<ReturnType<typeof import('../mocks').getMockStore>>;

let mockEventSeq = 0;
function pushMockEvent(flow: Flow, type: string, stageId: string, message: string): void {
  flow.events = [
    ...(flow.events ?? []),
    {
      id: `ev-mock-${Date.now().toString(36)}-${(++mockEventSeq).toString(36)}`,
      type,
      stage_id: stageId || undefined,
      message,
      timestamp: new Date().toISOString(),
    },
  ];
}

async function withMockFlow(
  store: MockStore,
  flowId: string,
  mutate: (flow: Flow) => void,
): Promise<Flow> {
  const flows = store.MOCK_FLOWS as Flow[];
  const idx = flows.findIndex((f) => f.id === flowId);
  if (idx === -1) throw new Error(`flow not found: ${flowId}`);
  const flow = structuredClone(flows[idx]);
  mutate(flow);
  flow.updated_at = new Date().toISOString();
  flows[idx] = flow;
  return flow;
}

function activateNextStage(flow: Flow, fromIndex: number): void {
  const next = flow.stages.find((s, i) => i > fromIndex && s.status === 'pending');
  if (!next) {
    flow.status = 'completed';
    pushMockEvent(flow, 'flow.completed', '', '全部阶段已完成');
    return;
  }
  next.status = 'active';
  pushMockEvent(flow, 'stage.active', next.id, `进入阶段：${next.name}`);
  const enterGate = next.gates?.find((g) => g.phase === 'enter' && g.kind !== 'auto' && !g.passed);
  next.gates?.forEach((g) => {
    if (g.phase === 'enter' && g.kind === 'auto') g.passed = true;
  });
  if (enterGate) {
    next.status = 'waiting_gate';
    pushMockEvent(flow, 'gate.waiting', next.id, `阶段「${next.name}」等待进入审批`);
  }
}

export async function advanceStage(flowId: string, stageId: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      let blocked: string | null = null;
      const flow = await withMockFlow(store, flowId, (f) => {
        if (f.status !== 'active') throw new Error(`工作流未激活：${f.status}`);
        const si = f.stages.findIndex((s) => s.id === stageId);
        if (si === -1) throw new Error(`阶段不存在：${stageId}`);
        const stage = f.stages[si];
        if (stage.status !== 'active') throw new Error(`阶段未处于进行中：${stage.status}`);
        const blocking = stage.gates?.find(
          (g) => g.phase === 'exit' && g.kind !== 'auto' && !g.passed,
        );
        if (blocking) {
          stage.status = 'waiting_gate';
          pushMockEvent(f, 'gate.waiting', stage.id, `阶段「${stage.name}」等待出口 Gate 审批`);
          blocked = `阶段「${stage.name}」被 Gate 拦截，需要人工审批后才能推进`;
          return;
        }
        stage.gates?.forEach((g) => {
          if (g.phase === 'exit' && g.kind === 'auto') g.passed = true;
        });
        stage.status = 'done';
        stage.snapshot_id = stage.snapshot_id ?? `snap-${Math.random().toString(36).slice(2, 7)}`;
        pushMockEvent(f, 'stage.done', stage.id, `完成阶段：${stage.name}`);
        activateNextStage(f, si);
      });
      if (blocked) throw new Error(blocked);
      return flow;
    }
  }
  return post<Flow>(`${base()}/${flowId}/stages/${stageId}/advance`, {}, signal);
}

export async function skipStage(flowId: string, stageId: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return withMockFlow(store, flowId, (f) => {
        if (f.status !== 'active') throw new Error(`工作流未激活：${f.status}`);
        const si = f.stages.findIndex((s) => s.id === stageId);
        if (si === -1) throw new Error(`阶段不存在：${stageId}`);
        const stage = f.stages[si];
        if (!stage.optional) throw new Error(`阶段「${stage.name}」不是可选阶段，无法跳过`);
        if (stage.status !== 'pending' && stage.status !== 'active') {
          throw new Error(`当前状态不可跳过：${stage.status}`);
        }
        const wasActive = stage.status === 'active';
        stage.status = 'skipped';
        pushMockEvent(f, 'stage.skipped', stage.id, `跳过可选阶段：${stage.name}`);
        if (wasActive) activateNextStage(f, si);
      });
    }
  }
  return post<Flow>(`${base()}/${flowId}/stages/${stageId}/skip`, {}, signal);
}

export async function loopFlow(
  flowId: string,
  fromStageId: string,
  toStageId: string,
  reason?: string,
  signal?: AbortSignal,
) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return withMockFlow(store, flowId, (f) => {
        if (f.status !== 'active') throw new Error(`工作流未激活：${f.status}`);
        const fromIdx = f.stages.findIndex((s) => s.id === fromStageId);
        const toIdx = f.stages.findIndex((s) => s.id === toStageId);
        if (fromIdx === -1 || toIdx === -1) throw new Error('回环阶段不存在');
        if (toIdx >= fromIdx) throw new Error('回环目标必须是更早的阶段');
        const from = f.stages[fromIdx];
        const to = f.stages[toIdx];
        // Later stages reset to pending, snapshots and gate passes cleared.
        for (let i = toIdx + 1; i < f.stages.length; i++) {
          const st = f.stages[i];
          if (st.status === 'done' || st.status === 'active' || st.status === 'waiting_gate') {
            st.status = 'pending';
            st.snapshot_id = undefined;
            st.gates?.forEach((g) => {
              g.passed = false;
            });
          }
        }
        to.status = 'active';
        to.snapshot_id = undefined;
        to.gates?.forEach((g) => {
          g.passed = false;
        });
        f.artifacts?.forEach((a) => {
          const artIdx = f.stages.findIndex((s) => s.id === a.stage_id);
          if (artIdx >= toIdx) a.status = 'stale';
        });
        pushMockEvent(
          f,
          'flow.loop',
          to.id,
          `回环 ${from.name} → ${to.name}${reason ? `：${reason}` : ''}`,
        );
      });
    }
  }
  return post<Flow>(
    `${base()}/${flowId}/loop`,
    { from_stage_id: fromStageId, to_stage_id: toStageId, reason },
    signal,
  );
}

/** POST /flows/:id/gates/:gid/decide — approve or reject a waiting gate. */
export async function decideGate(
  flowId: string,
  gateId: string,
  decision: 'approve' | 'reject',
  reason?: string,
  signal?: AbortSignal,
) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return withMockFlow(store, flowId, (f) => {
        let found = false;
        for (const stage of f.stages) {
          for (const g of stage.gates ?? []) {
            if (g.id !== gateId) continue;
            found = true;
            if (stage.status !== 'active' && stage.status !== 'waiting_gate') {
              throw new Error(`Gate 所在阶段未激活：${stage.status}`);
            }
            if (decision === 'approve') {
              g.passed = true;
              if (stage.status === 'waiting_gate') stage.status = 'active';
              pushMockEvent(f, 'gate.approved', stage.id, `Gate ${gateId} 已批准${reason ? `：${reason}` : ''}`);
            } else {
              g.passed = false;
              stage.status = 'waiting_gate';
              pushMockEvent(f, 'gate.rejected', stage.id, `Gate ${gateId} 被驳回${reason ? `：${reason}` : ''}`);
              if (g.on_fail === 'escalate_to_debate') {
                pushMockEvent(f, 'gate.escalate_debate', stage.id, `Gate ${gateId} 已升级为多方辩论`);
              }
            }
          }
        }
        if (!found) throw new Error(`Gate 不存在：${gateId}`);
      });
    }
  }
  return post<Flow>(
    `${base()}/${flowId}/gates/${gateId}/decide`,
    { approved: decision === 'approve', reason },
    signal,
  );
}
