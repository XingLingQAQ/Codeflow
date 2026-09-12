import { get, post, put, del } from '../api';
import { API_ENDPOINTS } from '../api';
import { createFlow } from '../src/services-bridge/flows';
import type { Project, ProjectCreationResult, ProjectListResponse } from '../types';

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

const getBase = () => API_ENDPOINTS.projects;

export async function listProjects(params?: ProjectListParams, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore, showMockNotice } = await import('../src/mocks');
    if (isMockActive()) {
      await jitter();
      showMockNotice();
      const store = await getMockStore();
      const projects = [...store.MOCK_PROJECTS];
      return { projects, total: projects.length, has_more: false };
    }
  }
  return get<ProjectListResponse>(
    getBase(),
    params as Record<string, string | number | undefined>,
    signal
  );
}

export async function createProject(input: ProjectCreateInput, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../src/mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const now = Date.now() / 1000;
      const p: Project = {
        id: 'proj-mock-' + Math.random().toString(36).slice(2, 8),
        title: input.title,
        description: input.description,
        status: 'active',
        progress: 0,
        tags: input.tags,
        git_branch: input.git_branch,
        created_at: now,
        updated_at: now,
        last_active: now,
      };
      (store.MOCK_PROJECTS as Project[]).unshift(p);
      try {
        const flow = await createFlow({ project_id: p.id, template_id: 'new_project' }, signal);
        return { ...p, flow } satisfies ProjectCreationResult;
      } catch (error) {
        const index = (store.MOCK_PROJECTS as Project[]).findIndex(
          (project) => project.id === p.id
        );
        if (index >= 0) (store.MOCK_PROJECTS as Project[]).splice(index, 1);
        throw error;
      }
    }
  }
  return post<ProjectCreationResult>(getBase(), input, signal);
}

export function getProject(id: string, signal?: AbortSignal) {
  return get<Project>(`${getBase()}/${id}`, undefined, signal);
}

export function updateProject(id: string, input: ProjectUpdateInput, signal?: AbortSignal) {
  return put<Project>(`${getBase()}/${id}`, input, signal);
}

export function deleteProject(id: string, signal?: AbortSignal) {
  return del<{ deleted: boolean; id: string }>(`${getBase()}/${id}`, signal);
}

export function getProjectPlans(id: string, signal?: AbortSignal) {
  return get<{ plans: unknown[]; total: number }>(`${getBase()}/${id}/plans`, undefined, signal);
}

export function addPlanToProject(id: string, planId: string, signal?: AbortSignal) {
  return post<{ project_id: string; plan_id: string; associated: boolean }>(
    `${getBase()}/${id}/plans`,
    { plan_id: planId },
    signal
  );
}

export function removePlanFromProject(id: string, planId: string, signal?: AbortSignal) {
  return del<{ project_id: string; plan_id: string; removed: boolean }>(
    `${getBase()}/${id}/plans/${planId}`,
    signal
  );
}
