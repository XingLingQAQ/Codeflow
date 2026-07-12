import type {
  AuditLogEntry,
  CallTrace,
  Plan,
  PlanTask,
  Project,
  RawEntry,
  WorkflowApproval,
  WorkflowDecision,
  WorkflowMetadata,
  WorkflowReplayData,
  WorkflowReplayEvent,
  WorkflowReplayItem,
  WorkflowTimelineEvent,
} from '../types';

const toNumber = (value: unknown): number | undefined => {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
};

const toStringValue = (value: unknown): string | undefined =>
  typeof value === 'string' && value.trim().length > 0 ? value.trim() : undefined;

const toStringArray = (value: unknown): string[] => Array.isArray(value)
  ? value.map((item) => toStringValue(item)).filter((item): item is string => Boolean(item))
  : [];

const normalizeWorkflowApproval = (value: unknown): WorkflowApproval[] => {
  if (!Array.isArray(value)) return [];
  return value
    .map((item, index): WorkflowApproval | null => {
      if (!item || typeof item !== 'object') return null;
      const record = item as Record<string, unknown>;
      return {
        stage: toStringValue(record.stage) ?? `Stage ${index + 1}`,
        status: toStringValue(record.status) ?? 'pending',
        owner: toStringValue(record.owner),
        note: toStringValue(record.note),
        updated_at: toNumber(record.updated_at),
      };
    })
    .filter((item): item is WorkflowApproval => item !== null);
};

const normalizeWorkflowDecisions = (value: unknown): WorkflowDecision[] => {
  if (!Array.isArray(value)) return [];
  return value
    .map((item, index): WorkflowDecision | null => {
      if (!item || typeof item !== 'object') return null;
      const record = item as Record<string, unknown>;
      return {
        id: toStringValue(record.id) ?? `decision-${index + 1}`,
        summary: toStringValue(record.summary) ?? toStringValue(record.title) ?? 'Decision recorded',
        owner: toStringValue(record.owner),
        reason: toStringValue(record.reason),
        timestamp: toNumber(record.timestamp),
      };
    })
    .filter((item): item is WorkflowDecision => item !== null);
};

const normalizeTimelineItems = (value: unknown): WorkflowReplayItem[] => {
  if (!Array.isArray(value)) return [];
  return value
    .map((item, index): WorkflowReplayItem | null => {
      if (!item || typeof item !== 'object') return null;
      const record = item as Record<string, unknown>;
      return {
        id: toStringValue(record.id) ?? `timeline-${index + 1}`,
        lane: (toStringValue(record.lane) as WorkflowReplayItem['lane']) ?? 'plan',
        title: toStringValue(record.title) ?? 'Workflow event',
        message: toStringValue(record.message) ?? toStringValue(record.summary) ?? 'No details provided',
        timestamp: toNumber(record.timestamp),
        status: toStringValue(record.status),
        actor: toStringValue(record.actor),
        evidence: toStringValue(record.evidence),
        sourceId: toStringValue(record.sourceId) ?? toStringValue(record.source_id),
      };
    })
    .filter((item): item is WorkflowReplayItem => item !== null);
};

export function extractWorkflowMetadata(metadata?: Record<string, unknown> | null): WorkflowMetadata {
  return {
    workflow_id: toStringValue(metadata?.workflow_id),
    workflow_title: toStringValue(metadata?.workflow_title),
    blueprint: toStringValue(metadata?.blueprint),
    template: toStringValue(metadata?.template),
    replay_session_id: toStringValue(metadata?.replay_session_id),
    approval: normalizeWorkflowApproval(metadata?.approval),
    decisions: normalizeWorkflowDecisions(metadata?.decisions),
    timeline: normalizeTimelineItems(metadata?.timeline),
  };
}

export function buildFallbackWorkflowMetadata(project?: Project | null, plan?: Plan | null, tasks: PlanTask[] = []): WorkflowMetadata {
  const baseTime = project?.updated_at ?? plan?.updated_at ?? Math.floor(Date.now() / 1000);
  const taskItems = tasks.slice(0, 4).map((task, index) => ({
    id: `task-${task.id}`,
    lane: 'task' as const,
    title: task.title,
    message: task.description || 'Task synchronized into workflow lane.',
    timestamp: task.updated_at || baseTime + index,
    status: task.status,
    actor: task.assignee || task.model,
    evidence: task.dependencies?.length ? `depends on ${task.dependencies.join(', ')}` : undefined,
    sourceId: task.id,
  }));

  return {
    workflow_id: project?.id ?? plan?.id,
    workflow_title: plan?.title ?? project?.title,
    blueprint: toStringValue(project?.metadata?.blueprint) ?? toStringValue(project?.metadata?.template),
    template: toStringValue(project?.metadata?.template),
    replay_session_id: toStringValue(project?.metadata?.replay_session_id),
    approval: [
      {
        stage: 'Intent review',
        status: plan?.status === 'completed' ? 'approved' : 'in_review',
        owner: 'Plan reviewer',
        note: 'Minimal approval surface derived from plan status.',
        updated_at: plan?.updated_at,
      },
    ],
    decisions: plan ? [{
      id: `decision-${plan.id}`,
      summary: 'Reuse existing Project / Plan / Task objects as workflow anchors.',
      owner: 'Planner',
      reason: 'Keep MVP on current entities without introducing a separate DSL.',
      timestamp: plan.updated_at,
    }] : [],
    timeline: [
      ...(project ? [{
        id: `project-${project.id}`,
        lane: 'project' as const,
        title: project.title,
        message: `Project opened${toStringValue(project.metadata?.blueprint) ? ` from ${toStringValue(project.metadata?.blueprint)}` : ''}.`,
        timestamp: project.updated_at || project.created_at,
        status: project.status,
        actor: 'Project lead',
        evidence: toStringArray(project.tags).join(' · ') || undefined,
        sourceId: project.id,
      }] : []),
      ...(plan ? [{
        id: `plan-${plan.id}`,
        lane: 'plan' as const,
        title: plan.title,
        message: plan.description || 'Plan orchestrates approval, dependencies, and delivery checkpoints.',
        timestamp: plan.updated_at || plan.created_at,
        status: plan.status,
        actor: 'Planner',
        evidence: `${plan.completed_count}/${plan.task_count} tasks completed`,
        sourceId: plan.id,
      }] : []),
      ...taskItems,
    ],
  };
}

export function sortReplayItems(items: WorkflowReplayItem[]): WorkflowReplayItem[] {
  return [...items].sort((a, b) => {
    const aTime = a.timestamp ?? 0;
    const bTime = b.timestamp ?? 0;
    return aTime - bTime;
  });
}

export function mergeWorkflowTimeline(
  metadataItems: WorkflowReplayItem[] = [],
  trace?: CallTrace,
  rawArchive: RawEntry[] = [],
  auditEntries: AuditLogEntry[] = [],
): WorkflowReplayItem[] {
  const traceItems = (trace?.children ?? []).map((child, index) => ({
    id: `trace-${child.id || index}`,
    lane: 'trace' as const,
    title: child.tool_name || 'Trace event',
    message: child.output || 'Trace completed without textual output.',
    timestamp: child.end_time ?? child.start_time,
    status: child.status,
    actor: child.agent_role,
    evidence: child.duration_ms ? `${child.duration_ms}ms` : undefined,
    sourceId: child.id,
  }));

  const archiveItems = rawArchive.map((entry, index) => ({
    id: `archive-${entry.id || index}`,
    lane: 'archive' as const,
    title: toStringValue(entry.metadata?.title) ?? `${entry.type} archive`,
    message: entry.content,
    timestamp: entry.timestamp,
    status: entry.type,
    actor: toStringValue(entry.metadata?.actor) ?? 'raw archive',
    evidence: toStringValue(entry.metadata?.summary),
    sourceId: entry.id,
  }));

  const auditItems = auditEntries.map((entry) => ({
    id: `audit-${entry.id}`,
    lane: 'audit' as const,
    title: entry.action || entry.event_type,
    message: toStringValue(entry.details?.message) ?? `${entry.resource.type}:${entry.resource.id}`,
    timestamp: entry.timestamp,
    status: entry.outcome,
    actor: entry.actor.name || entry.actor.id,
    evidence: entry.trace?.path || entry.resource.path,
    sourceId: entry.id,
  }));

  const merged = [...metadataItems, ...traceItems, ...archiveItems, ...auditItems];
  const seen = new Set<string>();
  return sortReplayItems(
    merged.filter((item) => {
      if (seen.has(item.id)) return false;
      seen.add(item.id);
      return true;
    }),
  );
}

function toTimelineLane(lane?: string): WorkflowReplayItem['lane'] {
  switch ((lane || '').toLowerCase()) {
    case 'project':
      return 'project';
    case 'plan':
      return 'plan';
    case 'task':
      return 'task';
    case 'audit':
      return 'audit';
    case 'trace':
    case 'agent':
    default:
      return 'trace';
  }
}

export function workflowTimelineEventToItem(event: WorkflowTimelineEvent): WorkflowReplayItem {
  return {
    id: event.id,
    lane: toTimelineLane(event.lane),
    title: event.title,
    message: event.detail || event.title,
    timestamp: event.timestamp,
    status: event.status,
    actor: event.source,
    evidence: [event.session_id, event.agent_id].filter(Boolean).join(' · ') || undefined,
    sourceId: event.trace_id || event.audit_id || event.task_id || event.plan_id || event.project_id,
  };
}

export function workflowReplayEventToItem(event: WorkflowReplayEvent): WorkflowReplayItem {
  return {
    id: event.id,
    lane: toTimelineLane(event.lane),
    title: event.speaker || event.type,
    message: event.message,
    timestamp: event.timestamp,
    status: event.status,
    actor: event.speaker,
    evidence: event.evidence,
    sourceId: event.trace_id || event.audit_id || event.task_id || event.plan_id || event.project_id,
  };
}

export function buildWorkflowReplayData(
  title: string,
  metadata: WorkflowMetadata,
  timeline: WorkflowReplayItem[],
): WorkflowReplayData {
  const approvals = metadata.approval ?? [];
  const status = approvals.length === 0
    ? 'In flight'
    : approvals.some((item) => item.status === 'rejected')
      ? 'Blocked'
      : approvals.every((item) => item.status === 'approved')
        ? 'Approved'
        : 'In flight';

  return {
    title: metadata.workflow_title || title,
    subtitle: [metadata.blueprint, metadata.template].filter(Boolean).join(' • ') || 'Template → Plan → Task → Trace → Archive → Audit',
    status,
    summary: 'Workflow replay aggregates project template metadata, plan orchestration, task dependencies, conversation trace, raw archive, and audit evidence in one minimal timeline.',
    items: timeline,
  };
}
