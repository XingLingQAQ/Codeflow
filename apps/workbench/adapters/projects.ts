import type { Project, ProjectStatus, WorkflowMetadata } from '../types';
import { extractWorkflowMetadata } from './workflows';

export interface ProjectViewModel {
  id: string;
  title: string;
  descriptionText: string;
  status: ProjectStatus;
  statusTone: string;
  progress: number;
  progressLabel: string;
  progressTone: string;
  tags: string[];
  lastActiveText: string;
  workflow: WorkflowMetadata;
  workflowSummaryText?: string;
  workflowBadgeText?: string;
  hasReplayLink: boolean;
}

export function extractProjectWorkflowMetadata(metadata?: Record<string, unknown> | null): WorkflowMetadata {
  return extractWorkflowMetadata(metadata);
}

export function formatProjectRelativeTime(ts: number): string {
  if (!ts) return 'Unknown';
  const diff = Math.floor((Date.now() / 1000) - ts);
  if (diff < 60) return 'Just now';
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

export function getProjectStatusTone(status: ProjectStatus | string): string {
  switch (status) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 border-emerald-200';
    case 'completed':
      return 'bg-blue-100 text-blue-700 border-blue-200';
    case 'paused':
      return 'bg-amber-100 text-amber-700 border-amber-200';
    default:
      return 'bg-slate-100 text-slate-600 border-slate-200';
  }
}

export function getProjectProgressLabel(progress: number): string {
  if (progress === 0) return 'Not started';
  if (progress === 100) return 'Completed';
  return 'Progress';
}

export function getProjectProgressTone(progress: number): string {
  if (progress === 0) return 'bg-slate-200';
  if (progress === 100) return 'bg-emerald-500';
  return 'bg-gradient-to-r from-blue-500 to-indigo-500';
}

export function toProjectViewModel(project: Project): ProjectViewModel {
  const workflow = extractProjectWorkflowMetadata(project.metadata);
  const workflowSummaryText = workflow.workflow_title || workflow.blueprint || workflow.template;
  const workflowBadgeText = workflow.blueprint || workflow.template;
  return {
    id: project.id,
    title: project.title,
    descriptionText: project.description || 'No description provided',
    status: project.status,
    statusTone: getProjectStatusTone(project.status),
    progress: project.progress,
    progressLabel: getProjectProgressLabel(project.progress),
    progressTone: getProjectProgressTone(project.progress),
    tags: project.tags ?? [],
    lastActiveText: formatProjectRelativeTime(project.last_active),
    workflow,
    workflowSummaryText,
    workflowBadgeText,
    hasReplayLink: !!workflow.replay_session_id,
  };
}

export function toProjectViewModels(projects: Project[]): ProjectViewModel[] {
  return projects.map(toProjectViewModel);
}
