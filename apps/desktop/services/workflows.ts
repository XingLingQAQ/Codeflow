import { get } from '../api';
import { API_ENDPOINTS } from '../api';
import type { WorkflowOverview, WorkflowTimelineResponse, WorkflowReplayResponse } from '../types';

const getBase = () => API_ENDPOINTS.workflows;

export function getWorkflowOverview(projectId: string, signal?: AbortSignal) {
  return get<WorkflowOverview>(`${getBase()}/${projectId}/overview`, undefined, signal);
}

export function getWorkflowTimeline(projectId: string, signal?: AbortSignal) {
  return get<WorkflowTimelineResponse>(`${getBase()}/${projectId}/timeline`, undefined, signal);
}

export function getWorkflowReplay(projectId: string, params?: { session_id?: string }, signal?: AbortSignal) {
  return get<WorkflowReplayResponse>(`${getBase()}/${projectId}/replay`, params, signal);
}
