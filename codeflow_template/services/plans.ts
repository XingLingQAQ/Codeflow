import { get, post, patch, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Plan, PlanListResponse, PlanTask, PlanTaskListResponse } from '../types';

const BASE = API_ENDPOINTS.plans;

export function listPlans(signal?: AbortSignal) {
  return get<PlanListResponse>(BASE, undefined, signal);
}

export function createPlan(input: { title: string; description?: string }, signal?: AbortSignal) {
  return post<Plan>(BASE, input, signal);
}

export function getPlanTasks(planId: string, signal?: AbortSignal) {
  return get<PlanTaskListResponse>(`${BASE}/${planId}/tasks`, undefined, signal);
}

export function createPlanTask(planId: string, input: { title: string; description?: string; priority?: string }, signal?: AbortSignal) {
  return post<PlanTask>(`${BASE}/${planId}/tasks`, input, signal);
}

export function updatePlanTask(planId: string, taskId: string, input: Partial<PlanTask>, signal?: AbortSignal) {
  return patch<PlanTask>(`${BASE}/${planId}/tasks/${taskId}`, input, signal);
}

export function deletePlanTask(planId: string, taskId: string, signal?: AbortSignal) {
  return del<{ deleted: boolean }>(`${BASE}/${planId}/tasks/${taskId}`, signal);
}

export function reorderPlanTask(planId: string, taskId: string, input: { order: number }, signal?: AbortSignal) {
  return post<PlanTask>(`${BASE}/${planId}/tasks/${taskId}/reorder`, input, signal);
}

export function batchUpdateTaskModel(planId: string, input: { task_ids: string[]; model: string }, signal?: AbortSignal) {
  return post<{ updated: number }>(`${BASE}/${planId}/tasks/batch-model`, input, signal);
}
