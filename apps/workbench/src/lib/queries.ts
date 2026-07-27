import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as projectsSvc from '../../services/projects';
import * as flowsSvc from '../services-bridge/flows';
import { guardRules } from '../services-bridge/guard';
import { listWorkspace } from '../services-bridge/workspace';

export const qk = {
  projects: ['projects'] as const,
  flows: (projectId?: string) => ['flows', projectId ?? 'all'] as const,
  flow: (id: string) => ['flow', id] as const,
  templates: ['flow-templates'] as const,
  guardRules: ['guard-rules'] as const,
  workspace: (root: string, path: string) => ['workspace', root, path] as const,
};

export function useProjects() {
  return useQuery({
    queryKey: qk.projects,
    queryFn: ({ signal }) => projectsSvc.listProjects({ limit: 100 }, signal),
    retry: 0,
    staleTime: 15_000,
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: projectsSvc.ProjectCreateInput) => projectsSvc.createProject(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.projects }),
  });
}

export function useFlows(projectId?: string, enabled = true) {
  return useQuery({
    queryKey: qk.flows(projectId),
    queryFn: ({ signal }) => flowsSvc.listFlows(projectId, signal),
    retry: 0,
    enabled,
    staleTime: 5_000,
  });
}

export function useFlowTemplates() {
  return useQuery({
    queryKey: qk.templates,
    queryFn: ({ signal }) => flowsSvc.listFlowTemplates(signal),
    retry: 0,
    staleTime: 60_000,
  });
}

export function useGuardRules(enabled = true) {
  return useQuery({
    queryKey: qk.guardRules,
    queryFn: ({ signal }) => guardRules(signal),
    retry: 0,
    enabled,
  });
}

export function useWorkspaceList(root: string, path: string, enabled: boolean) {
  return useQuery({
    queryKey: qk.workspace(root, path),
    queryFn: ({ signal }) => listWorkspace(root, path, signal),
    retry: 0,
    enabled: enabled && !!root,
  });
}
