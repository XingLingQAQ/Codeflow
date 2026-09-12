import { useMemo } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as projectsSvc from '../../services/projects';
import * as flowsSvc from '../services-bridge/flows';
import {
  guardRules,
  listExemptionRequests,
  decideExemptionRequest,
  type ExemptionRequestStatus,
} from '../services-bridge/guard';
import { listAgents, listStageAgents } from '../services-bridge/agents';
import { getGlobalConfig } from '../services-bridge/config';
import { probeChatEndpoint } from '../services-bridge/chat';
import {
  listWorkspace,
  readWorkspaceFile,
  writeWorkspaceFile,
  listStagedWorkspace,
  promoteWorkspaceFile,
  discardStagedWorkspace,
  promoteAllWorkspace,
  discardAllStagedWorkspace,
  detectWorkspaceScripts,
  type WriteMode,
} from '../services-bridge/workspace';
import { diffLines, diffStats } from './diff';

export const qk = {
  projects: ['projects'] as const,
  flows: (projectId?: string) => ['flows', projectId ?? 'all'] as const,
  flow: (id: string) => ['flow', id] as const,
  templates: ['flow-templates'] as const,
  guardRules: ['guard-rules'] as const,
  exemptionRequests: (status?: string) => ['guard-exemption-requests', status ?? 'all'] as const,
  agents: ['agents-registry'] as const,
  agentRegistry: ['agents-registry-management'] as const,
  chatAvailability: ['chat-availability'] as const,
  globalConfig: ['global-config'] as const,
  workspace: (root: string, path: string) => ['workspace', root, path] as const,
  workspaceFile: (root: string, path: string, staged: boolean) =>
    ['workspace-file', root, path, staged] as const,
  staged: (root: string) => ['workspace-staged', root] as const,
  scripts: (root: string) => ['workspace-scripts', root] as const,
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

/**
 * The flow the workbench operates on for a project: the first active one,
 * falling back to the most recent. Shared by the workbench frame, canvases,
 * and the bottom panel so they all agree.
 */
export function useProjectFlow(projectId?: string) {
  const q = useFlows(projectId, !!projectId);
  const flows = q.data?.items ?? [];
  const flow = flows.find((f) => f.status === 'active') ?? flows[0];
  return { ...q, flow };
}

export function useAdvanceStage(projectId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { flowId: string; stageId: string }) =>
      flowsSvc.advanceStage(input.flowId, input.stageId),
    // A blocked advance still mutates state (stage → waiting_gate), so refresh
    // on error too.
    onSettled: () => qc.invalidateQueries({ queryKey: qk.flows(projectId) }),
  });
}

export function useSkipStage(projectId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { flowId: string; stageId: string }) =>
      flowsSvc.skipStage(input.flowId, input.stageId),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.flows(projectId) }),
  });
}

export function useLoopFlow(projectId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { flowId: string; fromStageId: string; toStageId: string; reason?: string }) =>
      flowsSvc.loopFlow(input.flowId, input.fromStageId, input.toStageId, input.reason),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.flows(projectId) }),
  });
}

export function useDecideGate(projectId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { flowId: string; gateId: string; decision: 'approve' | 'reject'; reason?: string }) =>
      flowsSvc.decideGate(input.flowId, input.gateId, input.decision, input.reason),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.flows(projectId) }),
  });
}

export function useAgents() {
  return useQuery({
    queryKey: qk.agents,
    queryFn: ({ signal }) => listStageAgents(undefined, signal),
    retry: 0,
    staleTime: 60_000,
  });
}

/** Strict registry query for management/editor surfaces; never masks backend errors. */
export function useAgentRegistry() {
  return useQuery({
    queryKey: qk.agentRegistry,
    queryFn: ({ signal }) => listAgents(signal),
    retry: 0,
    staleTime: 60_000,
  });
}

/** One-shot probe of the experimental chat endpoint (mock mode: always true). */
export function useChatAvailability() {
  return useQuery({
    queryKey: qk.chatAvailability,
    queryFn: () => probeChatEndpoint(),
    retry: 0,
    staleTime: Infinity,
  });
}

export function useGlobalConfig() {
  return useQuery({
    queryKey: qk.globalConfig,
    queryFn: ({ signal }) => getGlobalConfig(signal),
    retry: 0,
    staleTime: 60_000,
  });
}

export function useExemptionRequests(status?: ExemptionRequestStatus, enabled = true) {
  return useQuery({
    queryKey: qk.exemptionRequests(status),
    queryFn: ({ signal }) => listExemptionRequests(status, signal),
    retry: 0,
    enabled,
    staleTime: 10_000,
  });
}

export function useDecideExemption() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; decision: 'approve' | 'reject'; reason?: string }) =>
      decideExemptionRequest(input.id, input.decision, input.reason),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['guard-exemption-requests'] }),
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

export function useWorkspaceFile(root: string, path: string, staged: boolean, enabled = true) {
  return useQuery({
    queryKey: qk.workspaceFile(root, path, staged),
    queryFn: ({ signal }) => readWorkspaceFile(root, path, staged, signal),
    retry: 0,
    enabled: enabled && !!root && !!path,
    staleTime: 5_000,
  });
}

export function useStagedList(root: string, enabled = true) {
  return useQuery({
    queryKey: qk.staged(root),
    queryFn: ({ signal }) => listStagedWorkspace(root, signal),
    retry: 0,
    enabled: enabled && !!root,
    staleTime: 5_000,
  });
}

export function useWorkspaceScripts(root: string, enabled = true) {
  return useQuery({
    queryKey: qk.scripts(root),
    queryFn: ({ signal }) => detectWorkspaceScripts(root, signal),
    retry: 0,
    enabled: enabled && !!root,
    staleTime: 60_000,
  });
}

/** Invalidate every workspace-scoped query for a root (list/file/staged). */
function invalidateWorkspace(qc: ReturnType<typeof useQueryClient>, root: string) {
  qc.invalidateQueries({ queryKey: ['workspace', root] });
  qc.invalidateQueries({ queryKey: ['workspace-file', root] });
  qc.invalidateQueries({ queryKey: qk.staged(root) });
}

export function useWriteWorkspaceFile(root: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { path: string; contentText: string; mode?: WriteMode }) =>
      writeWorkspaceFile({ root, ...input }),
    onSuccess: () => invalidateWorkspace(qc, root),
  });
}

export function usePromoteStaged(root: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (path: string) => promoteWorkspaceFile(root, path),
    onSuccess: () => invalidateWorkspace(qc, root),
  });
}

export function useDiscardStaged(root: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (path: string) => discardStagedWorkspace(root, path),
    onSuccess: () => invalidateWorkspace(qc, root),
  });
}

export function usePromoteAllStaged(root: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => promoteAllWorkspace(root),
    onSuccess: () => invalidateWorkspace(qc, root),
  });
}

export function useDiscardAllStaged(root: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => discardAllStagedWorkspace(root),
    onSuccess: () => invalidateWorkspace(qc, root),
  });
}

/**
 * Baseline vs staged content pair for one staged file, plus line diff + stats.
 * A missing baseline (newly created file) is treated as empty content.
 */
export function useStagedDiff(root: string, path: string, enabled = true) {
  const baseQ = useWorkspaceFile(root, path, false, enabled);
  const stagedQ = useWorkspaceFile(root, path, true, enabled);
  const baseText = baseQ.isError ? '' : baseQ.data?.content_text;
  const stagedText = stagedQ.data?.content_text;
  const ready = baseText !== undefined && stagedText !== undefined;

  const ops = useMemo(
    () => (ready ? diffLines(baseText, stagedText) : []),
    [ready, baseText, stagedText],
  );
  const stats = useMemo(
    () => (ready ? diffStats(baseText, stagedText) : { adds: 0, dels: 0 }),
    [ready, baseText, stagedText],
  );

  return {
    isLoading: (baseQ.isLoading && !baseQ.isError) || stagedQ.isLoading,
    isError: stagedQ.isError,
    base: baseText ?? '',
    current: stagedText ?? '',
    ready,
    ops,
    stats,
  };
}
