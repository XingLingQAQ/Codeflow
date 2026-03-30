import type { Agent, AgentRole, AgentStatus } from '../types';

export interface AgentViewModel {
  id: string;
  name: string;
  initial: string;
  role: AgentRole;
  roleText: string;
  roleTone: string;
  avatarTone: string;
  status: AgentStatus;
  statusText: string;
  statusTone: string;
  modelText: string;
  summaryText: string;
  taskCount: number;
  taskCountText: string;
  tokenCount: number;
  tokenCountText: string;
  errorCount: number;
  errorCountText?: string;
  sessionText?: string;
  lastActiveText: string;
}

function toSafeNumber(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function formatTitle(value: string): string {
  if (!value) return 'Unknown';
  return value
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

export function formatAgentRelativeTime(timestamp?: number): string {
  if (!timestamp) return 'Unknown';
  const diff = Math.max(0, Math.floor(Date.now() / 1000) - timestamp);
  if (diff < 60) return 'Just now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

export function getAgentStatusTone(status: AgentStatus | string): string {
  switch (status) {
    case 'running':
      return 'text-blue-600 bg-blue-50 border-blue-100';
    case 'paused':
      return 'text-amber-600 bg-amber-50 border-amber-100';
    case 'error':
      return 'text-red-600 bg-red-50 border-red-100';
    case 'stopped':
      return 'text-slate-500 bg-slate-100 border-slate-200';
    case 'idle':
    default:
      return 'text-slate-500 bg-slate-50 border-slate-100';
  }
}

export function getAgentRoleTone(role: AgentRole | string): string {
  switch (role) {
    case 'orchestrator':
      return 'bg-violet-100 text-violet-700';
    case 'coder':
      return 'bg-blue-100 text-blue-700';
    case 'reviewer':
      return 'bg-emerald-100 text-emerald-700';
    case 'researcher':
      return 'bg-amber-100 text-amber-700';
    case 'planner':
      return 'bg-indigo-100 text-indigo-700';
    case 'tester':
      return 'bg-rose-100 text-rose-700';
    default:
      return 'bg-slate-100 text-slate-700';
  }
}

export function getAgentAvatarTone(role: AgentRole | string): string {
  switch (role) {
    case 'orchestrator':
      return 'bg-violet-200 text-violet-700';
    case 'coder':
      return 'bg-blue-200 text-blue-700';
    case 'reviewer':
      return 'bg-emerald-200 text-emerald-700';
    case 'researcher':
      return 'bg-amber-200 text-amber-700';
    case 'planner':
      return 'bg-indigo-200 text-indigo-700';
    case 'tester':
      return 'bg-rose-200 text-rose-700';
    default:
      return 'bg-slate-200 text-slate-700';
  }
}

export function toAgentViewModel(agent: Agent): AgentViewModel {
  const taskCount = toSafeNumber(agent.task_count);
  const tokenCount = toSafeNumber(agent.tokens_used);
  const errorCount = toSafeNumber(agent.error_count);
  const roleText = formatTitle(agent.role);
  const statusText = formatTitle(agent.status);
  const modelText = agent.model || 'Unknown model';
  const lastActiveText = formatAgentRelativeTime(agent.last_active_at || agent.started_at);

  return {
    id: agent.id,
    name: agent.name,
    initial: (agent.name || roleText).charAt(0).toUpperCase(),
    role: agent.role,
    roleText,
    roleTone: getAgentRoleTone(agent.role),
    avatarTone: getAgentAvatarTone(agent.role),
    status: agent.status,
    statusText,
    statusTone: getAgentStatusTone(agent.status),
    modelText,
    summaryText: `${modelText} • ${roleText}`,
    taskCount,
    taskCountText: `${taskCount} tasks`,
    tokenCount,
    tokenCountText: `${tokenCount.toLocaleString()} tokens`,
    errorCount,
    errorCountText: errorCount > 0 ? `${errorCount} errors` : undefined,
    sessionText: agent.session_id ? `session ${agent.session_id}` : undefined,
    lastActiveText,
  };
}

export function toAgentViewModels(agents: Agent[]): AgentViewModel[] {
  return agents.map(toAgentViewModel);
}
