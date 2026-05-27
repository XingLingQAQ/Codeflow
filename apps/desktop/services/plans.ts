import { get, post, patch, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Plan, PlanListResponse, PlanTask, PlanTaskListResponse } from '../types';

const getBase = () => API_ENDPOINTS.plans;

export function listPlans(signal?: AbortSignal) {
  return get<PlanListResponse>(getBase(), undefined, signal);
}

export function createPlan(input: { title: string; description?: string }, signal?: AbortSignal) {
  return post<Plan>(getBase(), input, signal);
}

export function getPlanTasks(planId: string, signal?: AbortSignal) {
  return get<PlanTaskListResponse>(`${getBase()}/${planId}/tasks`, undefined, signal);
}

export function createPlanTask(planId: string, input: { title: string; description?: string; priority?: string }, signal?: AbortSignal) {
  return post<PlanTask>(`${getBase()}/${planId}/tasks`, input, signal);
}

export function updatePlanTask(planId: string, taskId: string, input: Partial<PlanTask>, signal?: AbortSignal) {
  return patch<PlanTask>(`${getBase()}/${planId}/tasks/${taskId}`, input, signal);
}

export function deletePlanTask(planId: string, taskId: string, signal?: AbortSignal) {
  return del<{ deleted: boolean }>(`${getBase()}/${planId}/tasks/${taskId}`, signal);
}

export function reorderPlanTask(planId: string, taskId: string, input: { order: number }, signal?: AbortSignal) {
  return post<PlanTask>(`${getBase()}/${planId}/tasks/${taskId}/reorder`, input, signal);
}

export function batchUpdateTaskModel(planId: string, input: { task_ids: string[]; model: string }, signal?: AbortSignal) {
  return post<{ updated: number }>(`${getBase()}/${planId}/tasks/batch-model`, input, signal);
}
