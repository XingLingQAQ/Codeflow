import { get, post, put, del } from '../api';
import { API_ENDPOINTS } from '../api';
import type { Project, ProjectListResponse } from '../types';

export interface ProjectCreateInput {
  title: string;
  description?: string;
  status?: string;
  tags?: string[];
  git_branch?: string;
  metadata?: Record<string, unknown>;
}

export interface ProjectUpdateInput {
  title?: string;
  description?: string;
  status?: string;
  tags?: string[];
  git_branch?: string;
  progress?: number;
  metadata?: Record<string, unknown>;
}

export interface ProjectListParams {
  status?: string;
  tag?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

const BASE = API_ENDPOINTS.projects;

export function listProjects(params?: ProjectListParams, signal?: AbortSignal) {
  return get<ProjectListResponse>(BASE, params as Record<string, string | number | undefined>, signal);
}

export function createProject(input: ProjectCreateInput, signal?: AbortSignal) {
  return post<Project>(BASE, input, signal);
}

export function getProject(id: string, signal?: AbortSignal) {
  return get<Project>(`${BASE}/${id}`, undefined, signal);
}

export function updateProject(id: string, input: ProjectUpdateInput, signal?: AbortSignal) {
  return put<Project>(`${BASE}/${id}`, input, signal);
}

export function deleteProject(id: string, signal?: AbortSignal) {
  return del<{ deleted: boolean; id: string }>(`${BASE}/${id}`, signal);
}

export function getProjectPlans(id: string, signal?: AbortSignal) {
  return get<{ plans: unknown[]; total: number }>(`${BASE}/${id}/plans`, undefined, signal);
}

export function addPlanToProject(id: string, planId: string, signal?: AbortSignal) {
  return post<{ project_id: string; plan_id: string; associated: boolean }>(`${BASE}/${id}/plans`, { plan_id: planId }, signal);
}

export function removePlanFromProject(id: string, planId: string, signal?: AbortSignal) {
  return del<{ project_id: string; plan_id: string; removed: boolean }>(`${BASE}/${id}/plans/${planId}`, signal);
}
