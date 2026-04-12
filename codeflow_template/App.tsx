import React, { useState, useEffect, useRef, useCallback } from 'react';
import {
  Terminal, Home, MessageSquare, Database, Users,
  Settings, Search, Bell, Mic, Paperclip, ArrowUp,
  ChevronRight, Plus, Monitor, Shield, LayoutGrid,
  Code, Zap, FileText, Check, MoreHorizontal,
  Lightbulb, Filter, Play, Edit3, Grid, Activity,
  Menu, X, ChevronLeft, Layers, User, Briefcase,
  Folder, Calendar, Clock, MoreVertical, GitBranch,
  Github, Globe, Moon, Sun, Laptop, ToggleLeft, ToggleRight,
  LogOut, CreditCard, Key, Smartphone,
  Flame, Snowflake, Archive, ExternalLink, RefreshCw
} from 'lucide-react';
import { ViewMode, NavItem, AgentPreset, Project, ProjectListResponse, Plan, PlanListResponse, PlanTask, PlanTaskListResponse, Agent, CallTrace, GlobalConfig, ResolvedConfig, ConversationTraceResponse, MemoryAgentContextResult, MemoryAgentRetrieveResult, MemoryAgentSource, MemoryTier, WorkflowMetadata, WorkflowApproval, WorkflowDecision, WorkflowReplayItem, WorkflowReplayData, WorkflowOverview, WorkflowTimelineResponse, WorkflowReplayResponse, WorkflowTimelineEvent, WorkflowReplayEvent, RawEntry, RawArchiveListResponse, AuditLogEntry, AuditLogListResponse, QueryMemoryNode, SAMGPathResponse, SAMGGraph, SAMGGraphImportResult } from './types';
import { LogModal } from './components/LogModal';
import { useApi, useMutation } from './hooks/useApi';
import { EmptyState } from './components/EmptyState';
import { LoadingSkeleton } from './components/LoadingSkeleton';
import { ErrorState } from './components/ErrorState';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ConnectionStatus } from './components/ConnectionStatus';
import { listProjects, createProject } from './services/projects';
import { listPlans, getPlanTasks, createPlan, updatePlanTask } from './services/plans';
import { listAgents } from './services/agents';
import { getConversationTrace, stopConversation, retryConversation } from './services/conversations';
import { listRawArchive } from './services/raw_archive';
import { getWorkflowOverview, getWorkflowTimeline, getWorkflowReplay } from './services/workflows';
import { listAuditLogs } from './services/audit';
import { retrieveMemoryAgent, buildMemoryAgentContext } from './services/memory_agent';
import { getGlobalConfig, updateGlobalConfig, resolveConfig } from './services/config';
import { healthCheck } from './services/health';
import { listHooks } from './services/hooks';
import { findPaths, exportGraph, importGraph } from './services/samg';
import type { ProjectCreateInput } from './services/projects';

// --- Types & Constants ---
interface ToastMsg {
  id: number;
  message: string;
  type: 'success' | 'info';
}

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

const extractWorkflowMetadata = (metadata?: Record<string, unknown> | null): WorkflowMetadata => ({
  workflow_id: toStringValue(metadata?.workflow_id),
  workflow_title: toStringValue(metadata?.workflow_title),
  blueprint: toStringValue(metadata?.blueprint),
  template: toStringValue(metadata?.template),
  replay_session_id: toStringValue(metadata?.replay_session_id),
  approval: normalizeWorkflowApproval(metadata?.approval),
  decisions: normalizeWorkflowDecisions(metadata?.decisions),
  timeline: normalizeTimelineItems(metadata?.timeline),
});

const buildFallbackWorkflowMetadata = (project?: Project | null, plan?: Plan | null, tasks: PlanTask[] = []): WorkflowMetadata => {
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
};

const sortReplayItems = (items: WorkflowReplayItem[]) => [...items].sort((a, b) => {
  const aTime = a.timestamp ?? 0;
  const bTime = b.timestamp ?? 0;
  return aTime - bTime;
});

const mergeWorkflowTimeline = (
  metadataItems: WorkflowReplayItem[] = [],
  trace?: CallTrace,
  rawArchive: RawEntry[] = [],
  auditEntries: AuditLogEntry[] = [],
): WorkflowReplayItem[] => {
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
};

const buildWorkflowReplayData = (
  title: string,
  metadata: WorkflowMetadata,
  timeline: WorkflowReplayItem[],
): WorkflowReplayData => {
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
};

// --- Shared Components ---

const ToastContainer = ({ toasts }: { toasts: ToastMsg[] }) => (
  <div className="fixed top-4 left-1/2 -translate-x-1/2 z-[100] flex flex-col gap-2 w-full max-w-sm px-4 pointer-events-none">
    {toasts.map(toast => (
      <div key={toast.id} className="bg-slate-800 text-white px-4 py-3 rounded-xl shadow-lg flex items-center gap-3 animate-in slide-in-from-top-2 fade-in duration-300">
        <div className={`size-2 rounded-full ${toast.type === 'success' ? 'bg-green-400' : 'bg-blue-400'}`}></div>
        <span className="text-sm font-medium">{toast.message}</span>
      </div>
    ))}
  </div>
);

const Sidebar = ({
  activeMode,
  setMode,
  userName,
}: {
  activeMode: ViewMode,
  setMode: (m: ViewMode) => void,
  userName: string,
}) => {
  const navItems: NavItem[] = [
    { id: ViewMode.HOME, label: 'Home', icon: <Home size={20} /> },
    { id: ViewMode.PROJECTS, label: 'Projects', icon: <Folder size={20} /> },
    { id: ViewMode.SESSIONS, label: 'Sessions', icon: <MessageSquare size={20} /> },
    { id: ViewMode.PLAN, label: 'Plan', icon: <LayoutGrid size={20} /> },
    { id: ViewMode.AGENTS, label: 'Agents', icon: <Users size={20} /> },
    { id: ViewMode.MEMORY, label: 'Memory', icon: <Database size={20} /> },
  ];

  return (
    <aside className="hidden md:flex w-64 bg-white/90 backdrop-blur-xl border-r border-slate-200 flex-col py-6 z-40 shrink-0 h-full relative shadow-sm transition-all duration-300">
      <div className="px-6 mb-8 flex items-center gap-3">
        <div className="flex items-center justify-center size-8 bg-slate-900 rounded-lg text-white shadow-md hover:scale-105 transition-transform">
          <Terminal size={18} />
        </div>
        <h2 className="text-slate-800 text-lg font-bold leading-tight tracking-tight cursor-default">CodeFlow</h2>
      </div>

      <nav className="flex flex-col gap-1.5 w-full px-3 flex-1">
        {navItems.map((item) => (
          <button
            key={item.id}
            onClick={() => setMode(item.id)}
            className={`flex items-center gap-3 px-4 py-3 rounded-xl transition-all group duration-200 ${
              activeMode === item.id
                ? 'bg-blue-50 text-blue-600 shadow-sm border border-blue-100 translate-x-1'
                : 'text-slate-500 hover:text-slate-900 hover:bg-slate-50 hover:translate-x-1'
            }`}
          >
            <span className={`transition-colors duration-200 ${activeMode === item.id ? 'text-blue-600' : 'text-slate-400 group-hover:text-slate-600'}`}>
              {item.icon}
            </span>
            <span className="font-medium text-sm">{item.label}</span>
            {activeMode === item.id && (
              <span className="ml-auto size-1.5 bg-blue-500 rounded-full shadow-[0_0_8px_rgba(59,130,246,0.6)]"></span>
            )}
          </button>
        ))}
      </nav>

      <div className="mt-auto w-full px-3">
        <button 
            onClick={() => setMode(ViewMode.SETTINGS)}
            className={`flex w-full items-center gap-3 px-4 py-3 rounded-xl transition-all group ${
              activeMode === ViewMode.SETTINGS
              ? 'bg-blue-50 text-blue-600 shadow-sm border border-blue-100'
              : 'text-slate-500 hover:text-slate-900 hover:bg-slate-50'
            }`}
        >
          <Settings size={20} className={activeMode === ViewMode.SETTINGS ? 'text-blue-600' : 'text-slate-400 group-hover:text-slate-600'} />
          <span className="font-medium text-sm">Settings</span>
        </button>
        <div 
            onClick={() => setMode(ViewMode.SETTINGS)}
            className="mt-4 pt-4 border-t border-slate-100 flex items-center gap-3 px-2 cursor-pointer hover:bg-slate-50 p-2 rounded-xl transition-colors"
        >
          <div className="size-8 rounded-full bg-gradient-to-tr from-indigo-500 to-purple-500 ring-2 ring-white shadow-sm"></div>
          <div className="flex flex-col text-left">
            <span className="text-xs font-semibold text-slate-700">{userName || 'Workspace User'}</span>
            <span className="text-[10px] text-slate-400">Local Profile</span>
          </div>
        </div>
      </div>
    </aside>
  );
};

const MobileNav = ({ activeMode, setMode }: { activeMode: ViewMode, setMode: (m: ViewMode) => void }) => {
  const navItems: NavItem[] = [
    { id: ViewMode.HOME, label: 'Home', icon: <Home size={20} /> },
    { id: ViewMode.PROJECTS, label: 'Projects', icon: <Folder size={20} /> },
    { id: ViewMode.PLAN, label: 'Plan', icon: <LayoutGrid size={20} /> },
    { id: ViewMode.AGENTS, label: 'Agents', icon: <Users size={20} /> },
    { id: ViewMode.SETTINGS, label: 'Settings', icon: <Settings size={20} /> },
  ];

  return (
    <div className="md:hidden fixed bottom-0 left-0 right-0 bg-white border-t border-slate-200 px-4 py-2 flex justify-between items-center z-50 safe-area-bottom shadow-[0_-5px_20px_rgba(0,0,0,0.05)]">
      {navItems.map((item) => (
        <button
          key={item.id}
          onClick={() => setMode(item.id)}
          className={`flex flex-col items-center gap-1 p-2 rounded-lg transition-all ${
            activeMode === item.id ? 'text-blue-600' : 'text-slate-400 hover:text-slate-600'
          }`}
        >
          <div className={`${activeMode === item.id ? 'transform -translate-y-1' : ''} transition-transform duration-200`}>
             {item.icon}
          </div>
          <span className="text-[10px] font-medium">{item.label}</span>
        </button>
      ))}
    </div>
  );
};

// --- View Components ---

const HomeView = ({ onNavigate, showToast }: { onNavigate: (mode: ViewMode) => void, showToast: (msg: string) => void }) => {
  const [input, setInput] = useState('');

  const handleSubmit = () => {
    if (!input.trim()) return;
    showToast("Analyzing intent...");
    setTimeout(() => {
        onNavigate(ViewMode.PLAN);
    }, 800);
  };

  return (
    <main className="flex-1 flex flex-col relative min-w-0 z-10 overflow-y-auto items-center justify-center bg-slate-50/50 pb-20 md:pb-0">
        <div className="absolute inset-0 overflow-hidden pointer-events-none">
        <div className="absolute top-[20%] left-[20%] w-64 h-64 md:w-96 md:h-96 bg-blue-200/20 rounded-full blur-3xl animate-float"></div>
        <div className="absolute bottom-[20%] right-[20%] w-64 h-64 md:w-96 md:h-96 bg-purple-200/20 rounded-full blur-3xl animate-float" style={{ animationDelay: '2s' }}></div>
        </div>

        <div className="max-w-4xl w-full px-6 flex flex-col items-center relative z-20">
        <div className="mb-8 md:mb-10 relative group cursor-pointer animate-float">
            <ConnectionStatus />
        </div>

        <h1 className="text-3xl md:text-5xl font-bold text-slate-900 mb-8 md:mb-12 text-center tracking-tight leading-tight">
            What would you like to build?
        </h1>

        <div className="flex flex-wrap items-center justify-center gap-3 mb-8 w-full">
            {['Codex', 'Claude', 'Gemini', 'Cowork'].map((name, i) => (
            <button key={name} className={`
                group flex items-center gap-2.5 px-5 py-2.5 md:px-6 md:py-3 rounded-full border transition-all duration-300 transform hover:-translate-y-0.5
                ${i === 0 
                ? 'bg-white border-indigo-100 text-indigo-600 shadow-[0_0_20px_rgba(99,102,241,0.15)] ring-1 ring-indigo-500/20' 
                : 'bg-white/60 border-slate-200/60 text-slate-500 hover:bg-white hover:border-indigo-100 hover:text-indigo-600 hover:shadow-lg hover:shadow-indigo-500/5'}
            `}>
                {i === 0 && (
                <span className="relative flex h-2 w-2">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-indigo-500"></span>
                </span>
                )}
                <span className="text-sm font-bold tracking-wide">{name}</span>
            </button>
            ))}
        </div>

        <div className="w-full relative group max-w-2xl">
            <div className="absolute -inset-1 bg-gradient-to-r from-blue-200/40 via-purple-200/40 to-blue-200/40 rounded-[2rem] opacity-0 group-focus-within:opacity-100 transition duration-700 blur-xl"></div>
            <div className="relative bg-white/80 backdrop-blur-xl rounded-[24px] shadow-xl shadow-slate-200/50 border border-white ring-1 ring-slate-200/50 flex flex-col p-2 group-focus-within:ring-2 group-focus-within:ring-blue-500/20 transition-all duration-300">
            <textarea 
                className="w-full bg-transparent border-none focus:ring-0 text-base md:text-lg text-slate-800 placeholder:text-slate-400 px-5 py-4 resize-none font-medium h-20 leading-relaxed" 
                placeholder="Describe your intent or search context..." 
                rows={1}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault();
                        handleSubmit();
                    }
                }}
            ></textarea>
            <div className="flex items-center justify-between px-3 pb-2">
                <div className="flex items-center gap-1">
                <button className="p-2 rounded-lg text-slate-400 hover:text-blue-600 hover:bg-blue-50 transition-colors">
                    <Paperclip size={20} />
                </button>
                <button className="p-2 rounded-lg text-slate-400 hover:text-blue-600 hover:bg-blue-50 transition-colors">
                    <Database size={20} />
                </button>
                <button className="p-2 rounded-lg text-slate-400 hover:text-blue-600 hover:bg-blue-50 transition-colors">
                    <Mic size={20} />
                </button>
                </div>
                <button 
                    onClick={handleSubmit}
                    className="flex items-center justify-center size-10 rounded-full bg-gradient-to-tr from-indigo-600 to-blue-500 text-white shadow-lg shadow-blue-500/20 transition-all duration-300 hover:scale-110 hover:shadow-blue-500/50">
                    <ArrowUp size={22} />
                </button>
            </div>
            </div>
        </div>

        {/* Quick Links for Project - Optional Discovery */}
        <div className="mt-12 flex gap-4 text-sm text-slate-400">
             <button onClick={() => onNavigate(ViewMode.PROJECTS)} className="hover:text-blue-600 transition-colors">View All Projects</button>
             <span>•</span>
             <button onClick={() => onNavigate(ViewMode.AGENTS)} className="hover:text-blue-600 transition-colors">Team Presets</button>
        </div>
        </div>
    </main>
    );
};

type ProjectWorkflowDraft = {
  blueprint: string;
  template: string;
  workflowTitle: string;
  replaySessionId: string;
};

const ProjectsView = ({ onNavigate, showToast, onProjectContext }: { onNavigate: (mode: ViewMode) => void, showToast: (msg: string) => void, onProjectContext: (project: Project) => void }) => {
    const [searchQuery, setSearchQuery] = useState('');
    const [debouncedSearch, setDebouncedSearch] = useState('');
    const [showCreateForm, setShowCreateForm] = useState(false);
    const [newTitle, setNewTitle] = useState('');
    const [newDesc, setNewDesc] = useState('');
    const [workflowDraft, setWorkflowDraft] = useState<ProjectWorkflowDraft>({
        blueprint: 'Governed Delivery Blueprint',
        template: 'workflow-template-v1',
        workflowTitle: 'Template to audit workflow',
        replaySessionId: '',
    });

    // Debounce search
    useEffect(() => {
        const timer = setTimeout(() => setDebouncedSearch(searchQuery), 300);
        return () => clearTimeout(timer);
    }, [searchQuery]);

    const { data, loading, error, refetch } = useApi<ProjectListResponse>(
        (signal) => listProjects({ search: debouncedSearch || undefined }, signal),
        [debouncedSearch],
    );

    const createMutation = useMutation<ProjectCreateInput, Project>(
        (input, signal) => createProject(input, signal),
    );

    const handleCreate = async () => {
        if (!newTitle.trim()) return;
        try {
            await createMutation.execute({
                title: newTitle.trim(),
                description: newDesc.trim() || undefined,
                metadata: {
                    blueprint: workflowDraft.blueprint.trim() || undefined,
                    template: workflowDraft.template.trim() || undefined,
                    workflow_title: workflowDraft.workflowTitle.trim() || undefined,
                    replay_session_id: workflowDraft.replaySessionId.trim() || undefined,
                },
            });
            setNewTitle('');
            setNewDesc('');
            setWorkflowDraft({
                blueprint: 'Governed Delivery Blueprint',
                template: 'workflow-template-v1',
                workflowTitle: 'Template to audit workflow',
                replaySessionId: '',
            });
            setShowCreateForm(false);
            showToast('Project created');
            refetch();
        } catch {
            showToast('Failed to create project');
        }
    };

    const handleProjectClick = (project: Project) => {
        const workflow = extractWorkflowMetadata(project.metadata);
        onProjectContext(project);
        showToast(`Opening ${workflow.blueprint || workflow.template || project.title}...`);
        setTimeout(() => onNavigate(ViewMode.PLAN), 600);
    };

    const getStatusColor = (status: string) => {
        switch(status) {
            case 'active': return 'bg-emerald-100 text-emerald-700 border-emerald-200';
            case 'completed': return 'bg-blue-100 text-blue-700 border-blue-200';
            case 'paused': return 'bg-amber-100 text-amber-700 border-amber-200';
            default: return 'bg-slate-100 text-slate-600 border-slate-200';
        }
    };

    const formatTime = (ts: number) => {
        if (!ts) return 'Unknown';
        const diff = Math.floor((Date.now() / 1000) - ts);
        if (diff < 60) return 'Just now';
        if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
        if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
        return `${Math.floor(diff / 86400)}d ago`;
    };

    const projects = data?.projects ?? [];

    return (
        <div className="flex-1 flex flex-col h-full bg-slate-50 relative overflow-hidden pb-16 md:pb-0">
            <div className="h-20 border-b border-slate-200 bg-white/80 backdrop-blur shrink-0 px-6 md:px-10 flex items-center justify-between z-20">
                <div className="flex flex-col">
                    <h1 className="text-2xl font-bold text-slate-900 tracking-tight">Projects</h1>
                    <span className="text-xs text-slate-500 font-medium">Manage your active workspaces</span>
                </div>
                <div className="flex items-center gap-3">
                    <div className="hidden md:flex relative group">
                        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 group-focus-within:text-blue-500 transition-colors" />
                        <input
                            type="text"
                            placeholder="Search projects..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            className="pl-9 pr-4 py-2 bg-slate-100 border-transparent focus:bg-white focus:border-blue-200 focus:ring-2 focus:ring-blue-500/10 rounded-xl text-sm w-64 transition-all"
                        />
                    </div>
                    <button
                        onClick={() => setShowCreateForm(true)}
                        className="flex items-center gap-2 bg-slate-900 hover:bg-slate-800 text-white px-4 py-2 rounded-xl text-sm font-bold shadow-lg shadow-slate-900/10 transition-transform active:scale-95"
                    >
                        <Plus size={18} />
                        <span className="hidden sm:inline">New Project</span>
                    </button>
                </div>
            </div>

            {showCreateForm && (
                <div className="fixed inset-0 bg-black/30 backdrop-blur-sm z-50 flex items-center justify-center p-4" onClick={() => setShowCreateForm(false)}>
                    <div className="bg-white rounded-2xl p-6 w-full max-w-md shadow-2xl" onClick={e => e.stopPropagation()}>
                        <h2 className="text-lg font-bold text-slate-800 mb-4">New Project</h2>
                        <input
                            type="text"
                            placeholder="Project title *"
                            value={newTitle}
                            onChange={e => setNewTitle(e.target.value)}
                            className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm mb-3 focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none"
                            autoFocus
                        />
                        <textarea
                            placeholder="Description (optional)"
                            value={newDesc}
                            onChange={e => setNewDesc(e.target.value)}
                            className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm mb-3 focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none resize-none h-20"
                        />
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-4">
                            <input
                                type="text"
                                placeholder="Blueprint"
                                value={workflowDraft.blueprint}
                                onChange={(e) => setWorkflowDraft(prev => ({ ...prev, blueprint: e.target.value }))}
                                className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none"
                            />
                            <input
                                type="text"
                                placeholder="Template"
                                value={workflowDraft.template}
                                onChange={(e) => setWorkflowDraft(prev => ({ ...prev, template: e.target.value }))}
                                className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none"
                            />
                            <input
                                type="text"
                                placeholder="Workflow title"
                                value={workflowDraft.workflowTitle}
                                onChange={(e) => setWorkflowDraft(prev => ({ ...prev, workflowTitle: e.target.value }))}
                                className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none md:col-span-2"
                            />
                            <input
                                type="text"
                                placeholder="Replay session id (optional)"
                                value={workflowDraft.replaySessionId}
                                onChange={(e) => setWorkflowDraft(prev => ({ ...prev, replaySessionId: e.target.value }))}
                                className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none md:col-span-2"
                            />
                        </div>
                        <div className="flex justify-end gap-2">
                            <button onClick={() => setShowCreateForm(false)} className="px-4 py-2 text-sm text-slate-500 hover:bg-slate-100 rounded-lg">Cancel</button>
                            <button
                                onClick={handleCreate}
                                disabled={!newTitle.trim() || createMutation.loading}
                                className="px-4 py-2 bg-blue-500 text-white text-sm font-medium rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors"
                            >
                                {createMutation.loading ? 'Creating...' : 'Create'}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            <div className="flex-1 overflow-y-auto p-6 md:p-10">
                {loading ? (
                    <LoadingSkeleton variant="card" count={6} />
                ) : error ? (
                    <ErrorState message={error.message} onRetry={refetch} />
                ) : projects.length === 0 ? (
                    <EmptyState
                        icon={<Folder size={48} />}
                        title="No projects yet"
                        description="Create your first project to get started"
                        action={{ label: 'New Project', onClick: () => setShowCreateForm(true) }}
                    />
                ) : (
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 max-w-7xl mx-auto">
                        {projects.map((project) => {
                            const workflow = extractWorkflowMetadata(project.metadata);
                            return (
                                <div
                                    key={project.id}
                                    onClick={() => handleProjectClick(project)}
                                    className="bg-white rounded-2xl p-6 border border-slate-200 hover:border-blue-300 hover:shadow-xl hover:shadow-blue-500/5 transition-all duration-300 group cursor-pointer relative overflow-hidden"
                                >
                                    <div className="flex justify-between items-start mb-4">
                                        <div className={`px-2.5 py-1 rounded-full text-[10px] font-bold uppercase tracking-wide border ${getStatusColor(project.status)}`}>
                                            {project.status}
                                        </div>
                                        <button className="p-1 hover:bg-slate-100 rounded-lg text-slate-400 hover:text-slate-600 transition-colors">
                                            <MoreVertical size={16} />
                                        </button>
                                    </div>

                                    <h3 className="text-lg font-bold text-slate-800 mb-2 group-hover:text-blue-600 transition-colors">{project.title}</h3>
                                    <p className="text-sm text-slate-500 mb-4 line-clamp-2 h-10">{project.description || 'No description provided'}</p>

                                    {(workflow.blueprint || workflow.template || workflow.workflow_title) && (
                                        <div className="mb-4 rounded-xl border border-indigo-100 bg-indigo-50/60 p-3 space-y-2">
                                            <div className="flex items-center gap-2 text-[10px] uppercase tracking-widest font-bold text-indigo-600">
                                                <GitBranch size={12} /> Workflow seed
                                            </div>
                                            {workflow.workflow_title && <p className="text-sm font-semibold text-slate-700">{workflow.workflow_title}</p>}
                                            <div className="flex flex-wrap gap-2 text-[11px] text-slate-500">
                                                {workflow.blueprint && <span className="px-2 py-1 rounded-full bg-white border border-indigo-100">Blueprint: {workflow.blueprint}</span>}
                                                {workflow.template && <span className="px-2 py-1 rounded-full bg-white border border-indigo-100">Template: {workflow.template}</span>}
                                            </div>
                                        </div>
                                    )}

                                    <div className="mb-6">
                                        <div className="flex justify-between text-[11px] font-bold text-slate-400 mb-1.5">
                                            <span>{project.progress === 0 ? 'Not started' : project.progress === 100 ? 'Completed' : 'Progress'}</span>
                                            <span>{project.progress}%</span>
                                        </div>
                                        <div className="h-1.5 w-full bg-slate-100 rounded-full overflow-hidden">
                                            <div
                                                className={`h-full rounded-full transition-all duration-1000 ${project.progress === 0 ? 'bg-slate-200' : project.progress === 100 ? 'bg-emerald-500' : 'bg-gradient-to-r from-blue-500 to-indigo-500'}`}
                                                style={{ width: `${Math.max(project.progress, project.progress === 0 ? 0 : 2)}%` }}
                                            ></div>
                                        </div>
                                    </div>

                                    <div className="flex items-center justify-between pt-4 border-t border-slate-50">
                                        <div className="flex gap-2 flex-wrap">
                                            {(project.tags ?? []).map(tag => (
                                                <span key={tag} className="px-2 py-1 bg-slate-100 text-slate-500 rounded-md text-[10px] font-bold">{tag}</span>
                                            ))}
                                            {workflow.replay_session_id && (
                                                <span className="px-2 py-1 bg-amber-50 text-amber-600 rounded-md text-[10px] font-bold border border-amber-200">Replay linked</span>
                                            )}
                                            {(!project.tags || project.tags.length === 0) && !workflow.replay_session_id && (
                                                <span className="text-[10px] text-slate-300">No tags</span>
                                            )}
                                        </div>
                                        <div className="flex items-center gap-1.5 text-[11px] text-slate-400 font-medium">
                                            <Clock size={12} />
                                            {formatTime(project.last_active)}
                                        </div>
                                    </div>
                                </div>
                            );
                        })}

                        <button
                            onClick={() => setShowCreateForm(true)}
                            className="border-2 border-dashed border-slate-200 rounded-2xl p-6 flex flex-col items-center justify-center gap-4 hover:border-blue-300 hover:bg-blue-50/50 transition-all group min-h-[260px]"
                        >
                            <div className="size-14 rounded-full bg-slate-50 border border-slate-200 flex items-center justify-center text-slate-400 group-hover:scale-110 group-hover:border-blue-200 group-hover:text-blue-500 transition-all">
                                <Plus size={24} />
                            </div>
                            <div className="text-center">
                                <h3 className="font-bold text-slate-700 group-hover:text-blue-600 transition-colors">Create New Project</h3>
                                <p className="text-sm text-slate-400 mt-1">Start from scratch or seed from blueprint/template</p>
                            </div>
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
};

type AppWorkflowContext = {
  project: Project | null;
  overview: WorkflowOverview | null;
  timeline: WorkflowTimelineResponse | null;
  replay: WorkflowReplayResponse | null;
};

const toTimelineLane = (lane?: string): WorkflowReplayItem['lane'] => {
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
};

const workflowTimelineEventToItem = (event: WorkflowTimelineEvent): WorkflowReplayItem => ({
  id: event.id,
  lane: toTimelineLane(event.lane),
  title: event.title,
  message: event.detail || event.title,
  timestamp: event.timestamp,
  status: event.status,
  actor: event.source,
  evidence: [event.session_id, event.agent_id].filter(Boolean).join(' · ') || undefined,
  sourceId: event.trace_id || event.audit_id || event.task_id || event.plan_id || event.project_id,
});

const workflowReplayEventToItem = (event: WorkflowReplayEvent): WorkflowReplayItem => ({
  id: event.id,
  lane: toTimelineLane(event.lane),
  title: event.speaker || event.type,
  message: event.message,
  timestamp: event.timestamp,
  status: event.status,
  actor: event.speaker,
  evidence: event.evidence,
  sourceId: event.trace_id || event.audit_id || event.task_id || event.plan_id || event.project_id,
});

type SettingsProfile = {
  name: string;
  email: string;
  role: string;
};

const SettingsView = ({
  showToast,
  profile,
  onProfileChange,
}: {
  showToast: (msg: string) => void,
  profile: SettingsProfile,
  onProfileChange: (profile: SettingsProfile) => void,
}) => {
  const [darkMode, setDarkMode] = useState(false);
  const [configError, setConfigError] = useState(false);
  const [profileName, setProfileName] = useState(profile.name);
  const [profileEmail, setProfileEmail] = useState(profile.email);
  const [profileRole, setProfileRole] = useState(profile.role);

  const { data: config, loading, error, refetch } = useApi<GlobalConfig>(
    (signal) => getGlobalConfig(signal), [],
  );
  const { data: resolvedConfig, error: resolvedConfigError } = useApi<ResolvedConfig | null>(
    (signal) => resolveConfig(undefined, signal),
    [],
  );
  const { data: backendHealth } = useApi<{ status: string }>(
    (signal) => healthCheck(signal), [],
  );
  const { data: hookData } = useApi<{ hooks: { name: string; enabled: boolean; type: string }[] }>(
    (signal) => listHooks(signal), [],
  );

  const [model, setModel] = useState('');
  const [temp, setTemp] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState('');

  // Sync from API data
  useEffect(() => {
    if (config) {
      setModel(config.default_model || '');
      // temperature not in GlobalConfig, keep local default
    } else if (error) {
      setConfigError(true);
    }
  }, [config, error]);

  useEffect(() => {
    setTemp(resolvedConfig?.temperature ?? 0.7);
    setMaxTokens(
      resolvedConfig?.max_tokens !== undefined
        ? String(resolvedConfig.max_tokens)
        : '',
    );
  }, [resolvedConfig]);

  useEffect(() => {
    setProfileName(profile.name || '');
    setProfileEmail(profile.email || '');
    setProfileRole(profile.role || '');
  }, [profile]);

  const handleSave = async () => {
    try {
      await updateGlobalConfig({ default_model: model });
      onProfileChange({
        name: profileName.trim(),
        email: profileEmail.trim(),
        role: profileRole.trim(),
      });
      refetch();
      showToast("Global settings saved. Runtime values are shown read-only until Settings is bound to a stable session scope.");
    } catch {
      showToast("Failed to save settings");
    }
  };

  const modelOptions = Array.from(
    new Set(
      [
        ...(config?.api_pool?.map(ch => ch.name) ?? []),
        config?.default_model,
      ].filter((v): v is string => Boolean(v && v.trim())),
    ),
  );
  const backendConnected = (backendHealth?.status ?? '').toLowerCase() === 'healthy';
  const hooks = hookData?.hooks ?? [];
  const enabledHooks = hooks.filter(h => h.enabled).length;
  const profileInitials = profileName
    .split(/\s+/)
    .filter(Boolean)
    .map(part => part[0]?.toUpperCase() ?? '')
    .slice(0, 2)
    .join('') || 'U';
  const apiChannels = config?.api_pool ?? [];
  const runtimeScopeLabel = 'Not bound to a runtime session';
  const runtimeLoadError = Boolean(resolvedConfigError);

  return (
    <div className="flex-1 flex flex-col h-full bg-slate-50 relative overflow-hidden pb-16 md:pb-0">
       {/* Header */}
       <div className="h-20 border-b border-slate-200 bg-white/80 backdrop-blur shrink-0 px-6 md:px-10 flex items-center justify-between z-20">
          <div className="flex flex-col">
              <h1 className="text-2xl font-bold text-slate-900 tracking-tight">Settings</h1>
              <span className="text-xs text-slate-500 font-medium">Manage preferences and configurations</span>
          </div>
          <button 
            onClick={handleSave}
            className="flex items-center gap-2 bg-slate-900 hover:bg-slate-800 text-white px-6 py-2.5 rounded-xl text-sm font-bold shadow-lg shadow-slate-900/10 transition-transform active:scale-95"
          >
             Save Changes
          </button>
       </div>

       <div className="flex-1 overflow-y-auto p-6 md:p-10">
          <div className="max-w-4xl mx-auto space-y-8">
             
             {/* Profile Section */}
             <section className="bg-white rounded-2xl p-6 md:p-8 border border-slate-200 shadow-sm">
                <div className="flex items-start gap-4 mb-8">
                    <div className="p-2 bg-blue-50 text-blue-600 rounded-xl">
                        <User size={24} />
                    </div>
                    <div>
                        <h2 className="text-lg font-bold text-slate-900">Profile & Account</h2>
                        <p className="text-sm text-slate-500">Manage your personal information and role.</p>
                    </div>
                </div>
                
                <div className="flex flex-col md:flex-row gap-8 items-start">
                    <div className="flex flex-col items-center gap-3">
                         <div className="size-24 rounded-full bg-gradient-to-tr from-indigo-500 to-purple-500 ring-4 ring-slate-50 shadow-inner flex items-center justify-center text-white text-2xl font-bold">
                           {profileInitials}
                         </div>
                         <button className="text-xs font-bold text-blue-600 hover:underline">Change Avatar</button>
                    </div>
                    <div className="flex-1 w-full grid grid-cols-1 md:grid-cols-2 gap-6">
                        <div className="space-y-2">
                             <label className="text-xs font-bold text-slate-700 uppercase">Full Name</label>
                             <input
                               type="text"
                               value={profileName}
                               onChange={(e) => setProfileName(e.target.value)}
                               className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-400 transition-all"
                             />
                        </div>
                        <div className="space-y-2">
                             <label className="text-xs font-bold text-slate-700 uppercase">Email Address</label>
                             <input
                               type="email"
                               value={profileEmail}
                               onChange={(e) => setProfileEmail(e.target.value)}
                               className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-400 transition-all"
                             />
                        </div>
                        <div className="space-y-2">
                             <label className="text-xs font-bold text-slate-700 uppercase">Role</label>
                             <input
                               type="text"
                               value={profileRole}
                               onChange={(e) => setProfileRole(e.target.value)}
                               className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-400 transition-all"
                             />
                        </div>
                    </div>
                </div>
             </section>

             {/* Intelligence / AI */}
             <section className="bg-white rounded-2xl p-6 md:p-8 border border-slate-200 shadow-sm">
                <div className="flex items-start gap-4 mb-8">
                    <div className="p-2 bg-purple-50 text-purple-600 rounded-xl">
                        <Zap size={24} />
                    </div>
                    <div>
                        <h2 className="text-lg font-bold text-slate-900">Intelligence</h2>
                        <p className="text-sm text-slate-500">Configure default LLM parameters and model selection.</p>
                    </div>
                </div>

                <div className="space-y-6">
                    {configError && (
                        <div className="p-3 bg-amber-50 border border-amber-200 rounded-xl text-sm text-amber-700">
                            Could not load config from server. Using defaults.
                        </div>
                    )}
                    <div className={`p-4 border rounded-xl text-sm ${runtimeLoadError ? 'border-amber-200 bg-amber-50 text-amber-800' : 'border-slate-200 bg-slate-50 text-slate-700'}`}>
                      <div className="font-semibold">Runtime scope</div>
                      <div className="mt-1 break-all">{runtimeScopeLabel}</div>
                      {runtimeLoadError ? (
                        <div className="mt-2 text-xs text-amber-700">Runtime defaults could not be loaded from the server. Values below may fall back to local defaults.</div>
                      ) : (
                        <div className="mt-2 text-xs text-slate-500">Temperature and max tokens reflect current resolved defaults only. Settings is not bound to a stable runtime session yet, so these values are read-only here.</div>
                      )}
                    </div>
                    <div className="space-y-3">
                         <label className="text-xs font-bold text-slate-700 uppercase">Default Model</label>
                         {modelOptions.length === 0 ? (
                           <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl text-sm text-slate-500">
                             No model channels configured yet.
                           </div>
                         ) : (
                           <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                              {modelOptions.map(m => (
                                  <button
                                      key={m}
                                      onClick={() => setModel(m)}
                                      className={`px-4 py-3 rounded-xl border text-sm font-bold text-left transition-all ${model === m ? 'bg-indigo-50 border-indigo-200 text-indigo-700 shadow-sm ring-1 ring-indigo-500/20' : 'bg-white border-slate-200 text-slate-600 hover:border-indigo-100'}`}
                                  >
                                      {m}
                                  </button>
                              ))}
                           </div>
                         )}
                    </div>

                    <div className="space-y-3">
                         <div className="flex justify-between">
                            <label className="text-xs font-bold text-slate-700 uppercase">Temperature (Creativity): {temp}</label>
                         </div>
                         <input
                            type="range"
                            min="0" max="1" step="0.1"
                            value={temp}
                            onChange={(e) => setTemp(parseFloat(e.target.value))}
                            disabled={true}
                            className="w-full h-2 bg-slate-100 rounded-lg appearance-none cursor-pointer accent-indigo-600 disabled:cursor-not-allowed disabled:opacity-50"
                         />
                         <div className="flex justify-between text-[10px] text-slate-400 font-medium">
                            <span>Precise</span>
                            <span>Balanced</span>
                            <span>Creative</span>
                         </div>
                    </div>

                    <div className="space-y-2">
                      <label className="text-xs font-bold text-slate-700 uppercase">Max Tokens</label>
                      <input
                        type="number"
                        min="1"
                        step="1"
                        value={maxTokens}
                        onChange={(e) => setMaxTokens(e.target.value)}
                        disabled={true}
                        placeholder="Read-only until Settings is bound to a stable runtime session"
                        className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 transition-all disabled:cursor-not-allowed disabled:opacity-50"
                      />
                      <p className="text-xs text-slate-500">
                        Read-only preview of resolved runtime defaults.
                      </p>
                    </div>
                </div>
             </section>

             {/* Connections */}
             <section className="bg-white rounded-2xl p-6 md:p-8 border border-slate-200 shadow-sm">
                 <div className="flex items-start gap-4 mb-6">
                    <div className="p-2 bg-emerald-50 text-emerald-600 rounded-xl">
                        <Globe size={24} />
                    </div>
                    <div>
                        <h2 className="text-lg font-bold text-slate-900">Connections</h2>
                        <p className="text-sm text-slate-500">Manage third-party integrations and API keys.</p>
                    </div>
                 </div>

                 <div className="space-y-3">
                     <div className={`flex items-center justify-between p-4 border rounded-xl ${backendConnected ? 'border-emerald-100 bg-emerald-50/40' : 'border-amber-100 bg-amber-50/40'}`}>
                        <div className="flex items-center gap-3">
                            <Globe size={20} />
                            <span className="font-bold text-slate-700">Backend API</span>
                        </div>
                        <span className={`text-xs font-bold px-2 py-1 rounded ${backendConnected ? 'text-emerald-700 bg-emerald-100' : 'text-amber-700 bg-amber-100'}`}>
                          {backendConnected ? 'Healthy' : 'Unavailable'}
                        </span>
                     </div>

                     <div className="flex items-center justify-between p-4 border border-slate-100 rounded-xl bg-slate-50">
                        <div className="flex items-center gap-3">
                            <Key size={20} />
                            <span className="font-bold text-slate-700">CLI Hooks</span>
                        </div>
                        <span className={`text-xs font-bold px-2 py-1 rounded ${hooks.length > 0 ? 'text-blue-700 bg-blue-100' : 'text-slate-600 bg-slate-200'}`}>
                          {hooks.length > 0 ? `${enabledHooks}/${hooks.length} enabled` : 'No hooks'}
                        </span>
                     </div>

                     {loading ? (
                       <div className="p-4 border border-slate-100 rounded-xl bg-white text-sm text-slate-400">Loading API channels...</div>
                     ) : apiChannels.length === 0 ? (
                       <div className="p-4 border border-slate-100 rounded-xl bg-white text-sm text-slate-500">
                         No API channels configured. Add providers in global config.
                       </div>
                     ) : (
                       apiChannels.map(channel => {
                         const isEnabled = Boolean(channel.enabled);
                         return (
                           <div key={channel.id} className="flex items-center justify-between p-4 border border-slate-100 rounded-xl bg-white">
                             <div className="flex items-center gap-3">
                               <div className="size-5 rounded-full bg-slate-900 text-white flex items-center justify-center font-bold text-[10px]">
                                 {channel.provider?.charAt(0).toUpperCase() || 'A'}
                               </div>
                               <div className="flex flex-col">
                                 <span className="font-bold text-slate-700">{channel.name}</span>
                                 <span className="text-[11px] text-slate-400">{channel.provider}{channel.base_url ? ` - ${channel.base_url}` : ''}</span>
                               </div>
                             </div>
                             <span className={`text-xs font-bold px-2 py-1 rounded ${isEnabled ? 'text-emerald-700 bg-emerald-100' : 'text-slate-600 bg-slate-200'}`}>
                               {isEnabled ? 'Enabled' : 'Disabled'}
                             </span>
                           </div>
                         );
                       })
                     )}
                 </div>
             </section>

             {/* Interface */}
             <section className="bg-white rounded-2xl p-6 md:p-8 border border-slate-200 shadow-sm mb-12">
                 <div className="flex items-start gap-4 mb-6">
                    <div className="p-2 bg-slate-100 text-slate-600 rounded-xl">
                        <Monitor size={24} />
                    </div>
                    <div>
                        <h2 className="text-lg font-bold text-slate-900">Interface</h2>
                        <p className="text-sm text-slate-500">Customize your workspace appearance.</p>
                    </div>
                 </div>

                 <div className="flex items-center justify-between">
                     <div className="flex items-center gap-3">
                         <div className="p-2 bg-slate-50 rounded-lg text-slate-600"><Moon size={18} /></div>
                         <span className="font-medium text-slate-700">Dark Mode</span>
                     </div>
                     <button onClick={() => setDarkMode(!darkMode)} className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${darkMode ? 'bg-slate-900' : 'bg-slate-200'}`}>
                        <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${darkMode ? 'translate-x-6' : 'translate-x-1'}`} />
                     </button>
                 </div>
             </section>

          </div>
       </div>
    </div>
  )
}

const SessionsView = ({ onOpenReplay, onReplayData }: { onOpenReplay: () => void; onReplayData: (replay: WorkflowReplayData) => void }) => {
  const [mobileView, setMobileView] = useState<'list' | 'chat'>('list');
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);

  const { data: agents, loading, error, refetch } = useApi<Agent[]>(
    (signal) => listAgents(signal), [],
  );

  const { data: traceData } = useApi<ConversationTraceResponse | null>(
    (signal) => selectedSessionId
      ? getConversationTrace(selectedSessionId, signal)
      : Promise.resolve(null),
    [selectedSessionId],
    { enabled: !!selectedSessionId },
  );

  const { data: rawArchiveData } = useApi<RawArchiveListResponse | null>(
    (signal) => selectedSessionId
      ? listRawArchive({ session_id: selectedSessionId, limit: 20 }, signal)
      : Promise.resolve(null),
    [selectedSessionId],
    { enabled: !!selectedSessionId },
  );

  const { data: auditData } = useApi<AuditLogListResponse | null>(
    (signal) => selectedSessionId
      ? listAuditLogs({ limit: 20 }, signal)
      : Promise.resolve(null),
    [selectedSessionId],
    { enabled: !!selectedSessionId },
  );

  const handleSelectAgent = (agent: Agent) => {
    setSelectedAgentId(agent.id);
    setSelectedSessionId(agent.session_id || agent.id);
    setMobileView('chat');
  };

  const selectedAgent = agents?.find(a => a.id === selectedAgentId);
  const trace = traceData?.trace;
  const agentList = agents ?? [];
  const workflowTimeline = mergeWorkflowTimeline([], trace, rawArchiveData?.entries ?? [], (auditData?.entries ?? []).filter(entry => !selectedSessionId || entry.trace?.session_id === selectedSessionId || entry.actor.session_id === selectedSessionId));
  const replayData = buildWorkflowReplayData(selectedAgent?.name || 'Session workflow', {
    workflow_title: selectedAgent?.name,
    replay_session_id: selectedSessionId || undefined,
    approval: [],
    decisions: [],
    timeline: workflowTimeline,
  }, workflowTimeline);

  const handleOpenReplay = () => {
    onReplayData(replayData);
    onOpenReplay();
  };

  const SidebarContent = () => (
    <>
      <div className="p-5 border-b border-slate-100 bg-slate-50/50 shrink-0">
        <h3 className="text-[10px] font-bold text-slate-400 uppercase tracking-widest">Active Sessions</h3>
      </div>
      <div className="flex-1 overflow-y-auto p-3 space-y-2">
        {loading ? <LoadingSkeleton variant="list" count={4} /> :
         error ? <ErrorState message={error.message} onRetry={refetch} /> :
         agentList.length === 0 ? <div className="text-center py-8 text-sm text-slate-400">No active sessions</div> :
         agentList.map(agent => (
          <div
            key={agent.id}
            onClick={() => handleSelectAgent(agent)}
            className={`p-3 border rounded-xl cursor-pointer transition-all ${selectedAgentId === agent.id ? 'bg-blue-50/50 border-blue-100' : 'bg-white border-slate-200 hover:border-blue-200'}`}
          >
            <div className="flex justify-between items-center mb-1">
              <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${agent.status === 'running' ? 'text-blue-600 bg-blue-50' : 'text-slate-500 bg-slate-100'}`}>{agent.role}</span>
              <span className="text-[10px] text-slate-400">{agent.status}</span>
            </div>
            <h4 className="text-sm font-bold text-slate-800 mb-1">{agent.name}</h4>
            <div className="flex items-center gap-2">
              <span className="text-[11px] text-slate-500">{agent.model} • {agent.task_count} tasks</span>
            </div>
          </div>
        ))}
      </div>
    </>
  );

  return (
    <div className="flex flex-1 h-full overflow-hidden relative pb-16 md:pb-0">
      <aside className="hidden md:flex w-80 bg-white border-r border-slate-200 flex-col shrink-0 z-10">
        <SidebarContent />
      </aside>
      <div className={`md:hidden flex flex-col w-full h-full absolute inset-0 bg-white z-20 transition-transform duration-300 ${mobileView === 'list' ? 'translate-x-0' : '-translate-x-full'}`}>
        <SidebarContent />
      </div>
      <div className={`flex-1 flex flex-col bg-white relative w-full h-full transition-transform duration-300 md:translate-x-0 ${mobileView === 'chat' ? 'translate-x-0 absolute inset-0 z-30' : 'translate-x-full md:translate-x-0'}`}>
        <header className="h-16 border-b border-slate-100 flex items-center justify-between px-4 md:px-6 bg-white/80 backdrop-blur z-20 shrink-0">
          <div className="flex items-center gap-3">
            <button onClick={() => setMobileView('list')} className="md:hidden p-2 -ml-2 text-slate-500 hover:bg-slate-100 rounded-full">
              <ChevronLeft size={20} />
            </button>
            <div>
              <h2 className="font-bold text-slate-800 text-sm md:text-base">{selectedAgent?.name || 'Select a session'}</h2>
              {selectedAgent && <span className="text-[11px] text-slate-400">{selectedAgent.model} • {selectedAgent.status}</span>}
            </div>
          </div>
          {selectedAgent && (
            <div className="flex items-center gap-2">
              <button onClick={handleOpenReplay} className="px-3 py-1.5 text-xs font-medium text-indigo-600 hover:bg-indigo-50 rounded-lg">Replay</button>
              <button onClick={() => selectedSessionId && stopConversation(selectedSessionId)} className="px-3 py-1.5 text-xs font-medium text-red-500 hover:bg-red-50 rounded-lg">Stop</button>
              <button onClick={() => selectedSessionId && retryConversation(selectedSessionId)} className="px-3 py-1.5 text-xs font-medium text-blue-500 hover:bg-blue-50 rounded-lg">Retry</button>
            </div>
          )}
        </header>
        <main className="flex-1 overflow-y-auto p-4 md:p-8 space-y-6 bg-slate-50/30 pb-24 md:pb-8">
          {!selectedAgent ? (
            <EmptyState icon={<MessageSquare size={48} />} title="No session selected" description="Select a session from the sidebar to view conversation" />
          ) : !trace ? (
            <div className="text-center py-8 text-sm text-slate-400">No conversation trace available</div>
          ) : (
            <>
              <div className="rounded-2xl border border-slate-200 bg-white p-4 space-y-3">
                <div className="flex items-center justify-between gap-3 flex-wrap">
                  <div>
                    <h3 className="text-sm font-bold text-slate-800">Workflow timeline</h3>
                    <p className="text-xs text-slate-500">Trace, raw archive, and audit evidence are merged into one replay surface.</p>
                  </div>
                  <button onClick={handleOpenReplay} className="px-3 py-1.5 rounded-lg bg-slate-900 text-white text-xs font-bold">Open replay</button>
                </div>
                <div className="space-y-3">
                  {workflowTimeline.slice(0, 6).map((item) => (
                    <div key={item.id} className="flex gap-3 text-sm">
                      <div className="w-20 shrink-0 text-[10px] uppercase tracking-widest text-slate-400">{item.lane}</div>
                      <div className="flex-1 rounded-xl border border-slate-100 bg-slate-50 px-3 py-2">
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-semibold text-slate-700">{item.title}</span>
                          {item.status && <span className="text-[10px] text-slate-400 uppercase">{item.status}</span>}
                        </div>
                        <p className="text-xs text-slate-500 mt-1">{item.message}</p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
              <div className="space-y-4">
                {trace.children?.map((child, i) => (
                  <div key={child.id || i} className={`flex ${child.agent_role === 'orchestrator' ? 'justify-end' : 'justify-start'}`}>
                    <div className={`rounded-2xl px-4 py-3 max-w-[90%] md:max-w-2xl shadow-sm ${child.agent_role === 'orchestrator' ? 'bg-blue-600 text-white rounded-br-sm' : 'bg-white border border-slate-200 rounded-tl-sm'}`}>
                      <p className="text-xs font-bold mb-1 opacity-70">{child.agent_role} • {child.tool_name}</p>
                      <p className="text-sm leading-relaxed">{child.output || 'Processing...'}</p>
                    </div>
                  </div>
                )) ?? <div className="text-center py-4 text-sm text-slate-400">Empty trace</div>}
              </div>
            </>
          )}
        </main>
        <div className="p-4 md:p-6 bg-white border-t border-slate-100 absolute bottom-0 md:static w-full">
          <div className="relative bg-slate-50 border border-slate-200 rounded-2xl p-2 focus-within:ring-2 focus-within:ring-blue-100 transition-all">
            <textarea className="w-full bg-transparent border-none text-sm resize-none focus:ring-0 px-3 py-2 h-12" placeholder="Reply to CodeFlow..."></textarea>
            <div className="flex justify-between items-center px-2 pb-1">
              <div className="flex gap-2 text-slate-400">
                <Paperclip size={18} className="hover:text-blue-500 cursor-pointer" />
                <Terminal size={18} className="hover:text-blue-500 cursor-pointer" />
              </div>
              <button className="bg-blue-600 text-white rounded-full p-2 hover:bg-blue-700 transition-colors">
                <ArrowUp size={16} />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

const tierConfig: Record<MemoryTier, { icon: React.ReactNode; color: string; bg: string; label: string }> = {
  hot:  { icon: <Flame size={14} />,    color: 'text-red-500',  bg: 'bg-red-50 border-red-200',    label: '🔴 Hot' },
  warm: { icon: <Zap size={14} />,      color: 'text-amber-500', bg: 'bg-amber-50 border-amber-200', label: '🟡 Warm' },
  cold: { icon: <Snowflake size={14} />, color: 'text-blue-400', bg: 'bg-blue-50 border-blue-200',   label: '🔵 Cold' },
};

const MemoryView = () => {
  const [query, setQuery] = useState('');
  const [sessionId, setSessionId] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const [debouncedSessionId, setDebouncedSessionId] = useState('');
  const [selectedSourceKey, setSelectedSourceKey] = useState<string | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(query), 250);
    return () => clearTimeout(timer);
  }, [query]);

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSessionId(sessionId), 250);
    return () => clearTimeout(timer);
  }, [sessionId]);

  const normalizedQuery = debouncedQuery.trim();
  const normalizedSessionId = debouncedSessionId.trim();
  const shouldRetrieve = normalizedQuery.length > 0;
  const shouldContext = normalizedQuery.length > 0 || normalizedSessionId.length > 0;

  const {
    data: retrieveData,
    loading: retrieveLoading,
    error: retrieveError,
    refetch: refetchRetrieve,
  } = useApi<MemoryAgentRetrieveResult>(
    (signal) => retrieveMemoryAgent(
      {
        query: normalizedQuery,
        session_id: normalizedSessionId || undefined,
        max_results: 10,
      },
      signal,
    ),
    [normalizedQuery, normalizedSessionId],
    { enabled: shouldRetrieve },
  );

  const {
    data: contextData,
    loading: contextLoading,
    error: contextError,
    refetch: refetchContext,
  } = useApi<MemoryAgentContextResult>(
    (signal) => buildMemoryAgentContext(
      {
        session_id: normalizedSessionId,
        query: normalizedQuery || undefined,
        max_tokens: 600,
      },
      signal,
    ),
    [normalizedQuery, normalizedSessionId],
    { enabled: shouldContext },
  );

  const buildSourceKey = (source: MemoryAgentSource) => [
    source.kind,
    source.id,
    source.node_id ?? '',
    source.source_id ?? '',
  ].join(':');

  const mergeAtomicMemories = (
    primary: MemoryAgentRetrieveResult['atomic_memories'] = [],
    secondary: NonNullable<MemoryAgentContextResult['atomic_memories']> = [],
  ) => {
    const merged = [...primary];
    const seen = new Set(primary.map((memory) => memory.id));
    for (const memory of secondary) {
      if (seen.has(memory.id)) {
        continue;
      }
      merged.push(memory);
      seen.add(memory.id);
    }
    return merged;
  };

  const mergeSamgNodes = (
    primary: NonNullable<MemoryAgentRetrieveResult['samg_nodes']> = [],
    secondary: NonNullable<MemoryAgentContextResult['samg_nodes']> = [],
  ) => {
    const merged = [...primary];
    const seen = new Set(primary.map((node) => node.id));
    for (const node of secondary) {
      if (seen.has(node.id)) {
        continue;
      }
      merged.push(node);
      seen.add(node.id);
    }
    return merged;
  };

  const mergeSources = (
    primary: NonNullable<MemoryAgentRetrieveResult['sources']> = [],
    secondary: NonNullable<MemoryAgentContextResult['sources']> = [],
  ) => {
    const merged = [...primary];
    const seen = new Set(primary.map((source) => buildSourceKey(source)));
    for (const source of secondary) {
      const key = buildSourceKey(source);
      if (seen.has(key)) {
        continue;
      }
      merged.push(source);
      seen.add(key);
    }
    return merged;
  };

  const atomicMemories = mergeAtomicMemories(
    retrieveData?.atomic_memories ?? [],
    contextData?.atomic_memories ?? [],
  );
  const samgNodes = mergeSamgNodes(
    retrieveData?.samg_nodes ?? [],
    contextData?.samg_nodes ?? [],
  );
  const sources = mergeSources(
    retrieveData?.sources ?? [],
    contextData?.sources ?? [],
  );

  useEffect(() => {
    if (sources.length === 0) {
      setSelectedSourceKey(null);
      return;
    }
    if (!selectedSourceKey || !sources.some((source) => buildSourceKey(source) === selectedSourceKey)) {
      setSelectedSourceKey(buildSourceKey(sources[0]));
    }
  }, [selectedSourceKey, sources]);

  const selectedSource = sources.find((source) => buildSourceKey(source) === selectedSourceKey) ?? null;
  const loading = (shouldRetrieve && retrieveLoading) || (shouldContext && contextLoading);
  const error = retrieveError ?? contextError;
  const totalFound = retrieveData?.total_found ?? atomicMemories.length + samgNodes.length;
  const sourceCount = contextData?.source_count ?? sources.length;
  const contextBlock = contextData?.context_block?.trim() ?? '';

  const refetchAll = () => {
    if (shouldRetrieve) {
      refetchRetrieve();
    }
    if (shouldContext) {
      refetchContext();
    }
  };

  const getSourceKindMeta = (kind: MemoryAgentSource['kind']) => {
    switch (kind) {
      case 'atomic_memory':
        return {
          label: 'Atomic Memory',
          icon: <Layers size={14} />,
          badge: 'bg-red-50 text-red-600 border-red-200',
        };
      case 'samg_pointer':
        return {
          label: 'SAMG Pointer',
          icon: <Activity size={14} />,
          badge: 'bg-violet-50 text-violet-600 border-violet-200',
        };
      default:
        return {
          label: 'Raw Archive',
          icon: <Archive size={14} />,
          badge: 'bg-slate-100 text-slate-600 border-slate-200',
        };
    }
  };

  const getSourceTitle = (source: MemoryAgentSource) => {
    return source.title || source.node_label || source.summary || source.source_id || source.id;
  };

  const getSourcePreview = (source: MemoryAgentSource) => {
    const preview = source.summary || source.content || source.source_id || source.id;
    return preview.length > 120 ? `${preview.slice(0, 120)}...` : preview;
  };

  return (
    <div className="flex-1 flex flex-col h-full bg-slate-50 pb-16 md:pb-0 overflow-hidden">
      <div className="border-b border-slate-200 bg-white shrink-0">
        <div className="px-4 py-4 md:px-6 space-y-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <h1 className="text-xl font-bold text-slate-900">MemoryAgent</h1>
              <p className="text-sm text-slate-500">统一检索 Atomic Memory、SAMG Pointer 和 Raw Archive。</p>
            </div>
            <button
              onClick={refetchAll}
              disabled={!shouldRetrieve && !shouldContext}
              className="inline-flex items-center gap-2 px-3 py-2 rounded-xl border border-slate-200 text-sm text-slate-600 hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              <RefreshCw size={14} />
              刷新结果
            </button>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)] gap-3">
            <label className="flex flex-col gap-2">
              <span className="text-xs font-medium text-slate-500 uppercase tracking-wider">Query</span>
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="输入问题、文件名或实现线索"
                className="w-full px-4 py-3 rounded-2xl border border-slate-200 bg-slate-50 text-sm text-slate-700 outline-none focus:border-blue-400 focus:bg-white"
              />
            </label>
            <label className="flex flex-col gap-2">
              <span className="text-xs font-medium text-slate-500 uppercase tracking-wider">Session ID</span>
              <input
                value={sessionId}
                onChange={(event) => setSessionId(event.target.value)}
                placeholder="可选，用于限定上下文和 recent hot memory"
                className="w-full px-4 py-3 rounded-2xl border border-slate-200 bg-slate-50 text-sm text-slate-700 outline-none focus:border-blue-400 focus:bg-white"
              />
            </label>
          </div>

          <div className="grid grid-cols-2 xl:grid-cols-4 gap-3">
            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3">
              <div className="text-[11px] font-medium uppercase tracking-wider text-slate-400">Retrieve hits</div>
              <div className="mt-2 text-2xl font-bold text-slate-800">{totalFound}</div>
            </div>
            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3">
              <div className="text-[11px] font-medium uppercase tracking-wider text-slate-400">Context sources</div>
              <div className="mt-2 text-2xl font-bold text-slate-800">{sourceCount}</div>
            </div>
            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3">
              <div className="text-[11px] font-medium uppercase tracking-wider text-slate-400">Atomic memories</div>
              <div className="mt-2 text-2xl font-bold text-slate-800">{atomicMemories.length}</div>
            </div>
            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3">
              <div className="text-[11px] font-medium uppercase tracking-wider text-slate-400">SAMG nodes</div>
              <div className="mt-2 text-2xl font-bold text-slate-800">{samgNodes.length}</div>
            </div>
          </div>

          <p className="text-xs text-slate-400">填 query 会同时走 retrieve 和 context。只填 session_id 时，会优先展示 context 里的 recent hot memory 与来源。</p>
        </div>
      </div>

      <div className="flex-1 overflow-hidden">
        {!shouldRetrieve && !shouldContext ? (
          <EmptyState
            icon={<Database size={48} />}
            title="MemoryAgent 已接管默认入口"
            description="输入 query 检索统一记忆结果，或只填 session_id 查看上下文组装。"
          />
        ) : error ? (
          <ErrorState title="MemoryAgent 请求失败" message={error.message} onRetry={refetchAll} />
        ) : loading ? (
          <div className="p-4 md:p-6">
            <LoadingSkeleton variant="text" count={10} />
          </div>
        ) : (
          <div className="h-full grid grid-cols-1 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.9fr)] overflow-hidden">
            <div className="overflow-y-auto p-4 md:p-6 space-y-4">
              <section className="rounded-[28px] border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex items-center justify-between gap-3 mb-3">
                  <div>
                    <h2 className="text-base font-semibold text-slate-900">Context block</h2>
                    <p className="text-xs text-slate-400">来自 /api/v1/memory/agent/context 的统一上下文。</p>
                  </div>
                  <span className="text-xs text-slate-400">{contextBlock ? `${contextBlock.length} chars` : 'empty'}</span>
                </div>
                {contextBlock ? (
                  <pre className="whitespace-pre-wrap break-words text-xs leading-6 text-slate-600 bg-slate-50 rounded-2xl p-4 overflow-x-auto">{contextBlock}</pre>
                ) : (
                  <EmptyState icon={<FileText size={32} />} title="暂无上下文块" description="当前输入还没有组装出 memory_context。" />
                )}
              </section>

              <section className="rounded-[28px] border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex items-center justify-between gap-3 mb-4">
                  <div>
                    <h2 className="text-base font-semibold text-slate-900">Atomic memories</h2>
                    <p className="text-xs text-slate-400">统一入口返回的原子记忆结果。</p>
                  </div>
                  <span className="text-xs text-slate-400">{atomicMemories.length} items</span>
                </div>
                {atomicMemories.length === 0 ? (
                  <EmptyState icon={<Layers size={32} />} title="没有 atomic 命中" description="当前 query 没有命中原子记忆。" />
                ) : (
                  <div className="space-y-3">
                    {atomicMemories.map((memory) => {
                      const config = tierConfig[memory.tier] ?? tierConfig.hot;
                      return (
                        <div key={memory.id} className={`rounded-2xl border p-4 ${config.bg}`}>
                          <div className="flex items-start gap-3">
                            <div className={`mt-0.5 ${config.color}`}>{config.icon}</div>
                            <div className="min-w-0 flex-1">
                              <p className="text-sm leading-6 text-slate-700 whitespace-pre-wrap break-words">{memory.content}</p>
                              <div className="flex flex-wrap items-center gap-2 mt-3 text-[11px] text-slate-400">
                                <span className={`font-semibold uppercase ${config.color}`}>{memory.tier}</span>
                                <span>heat {memory.heat.toFixed(2)}</span>
                                <span>surprise {memory.surprise.toFixed(2)}</span>
                                <span>{memory.source}</span>
                                <span>{new Date(memory.timestamp * 1000).toLocaleString()}</span>
                                {memory.session_id && <span>session {memory.session_id}</span>}
                                {memory.tags.length > 0 && <span>{memory.tags.join(', ')}</span>}
                              </div>
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}
              </section>

              <section className="rounded-[28px] border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex items-center justify-between gap-3 mb-4">
                  <div>
                    <h2 className="text-base font-semibold text-slate-900">SAMG nodes</h2>
                    <p className="text-xs text-slate-400">统一入口解析出的图谱节点与 pointer。</p>
                  </div>
                  <span className="text-xs text-slate-400">{samgNodes.length} nodes</span>
                </div>
                {samgNodes.length === 0 ? (
                  <EmptyState icon={<Activity size={32} />} title="没有 SAMG 命中" description="当前 query 没有返回图谱节点。" />
                ) : (
                  <div className="space-y-3">
                    {samgNodes.map((node) => (
                      <div key={node.id} className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <div className="flex flex-wrap items-center gap-2 mb-3">
                          <span className="text-sm font-semibold text-slate-800">{node.label}</span>
                          <span className="px-2 py-0.5 rounded-full border border-violet-200 bg-violet-50 text-[10px] font-medium text-violet-600">hop {node.hop}</span>
                          <span className="px-2 py-0.5 rounded-full border border-slate-200 bg-white text-[10px] font-medium text-slate-500">activation {node.activation.toFixed(2)}</span>
                        </div>
                        {node.pointers && node.pointers.length > 0 ? (
                          <div className="space-y-2">
                            {node.pointers.map((pointer, index) => (
                              <div key={`${node.id}-${pointer.source_id}-${index}`} className="rounded-xl border border-slate-200 bg-white p-3">
                                <div className="flex items-center gap-2 text-xs text-slate-500">
                                  <ExternalLink size={12} className="text-violet-400" />
                                  <span className="font-medium text-slate-700">{pointer.summary || pointer.source_id}</span>
                                </div>
                                <div className="flex flex-wrap items-center gap-2 mt-2 text-[11px] text-slate-400">
                                  <span>{pointer.source_type}</span>
                                  <span>relevance {pointer.relevance.toFixed(2)}</span>
                                  {pointer.file_path && <span>{pointer.file_path}{pointer.line_range ? `:${pointer.line_range}` : ''}</span>}
                                  {pointer.session_id && <span>session {pointer.session_id}</span>}
                                </div>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <p className="text-sm text-slate-400">这个节点暂时没有可展示的 pointer。</p>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </section>
            </div>

            <aside className="border-t xl:border-t-0 xl:border-l border-slate-200 bg-white/70 backdrop-blur-sm overflow-hidden flex flex-col">
              <div className="p-4 border-b border-slate-200 shrink-0">
                <h2 className="text-base font-semibold text-slate-900">Sources</h2>
                <p className="text-xs text-slate-400 mt-1">统一来源卡片。点选后可查看可追溯内容。</p>
              </div>

              {sources.length === 0 ? (
                <div className="flex-1 overflow-y-auto">
                  <EmptyState icon={<Archive size={32} />} title="没有来源卡片" description="当前输入还没有返回可追溯来源。" />
                </div>
              ) : (
                <>
                  <div className="max-h-[40%] overflow-y-auto p-3 space-y-2 border-b border-slate-200 shrink-0">
                    {sources.map((source) => {
                      const meta = getSourceKindMeta(source.kind);
                      const sourceKey = buildSourceKey(source);
                      const active = sourceKey === selectedSourceKey;
                      return (
                        <button
                          key={sourceKey}
                          onClick={() => setSelectedSourceKey(sourceKey)}
                          className={`w-full text-left rounded-2xl border p-3 transition-colors ${
                            active ? 'border-blue-200 bg-blue-50' : 'border-slate-200 bg-white hover:bg-slate-50'
                          }`}
                        >
                          <div className="flex items-center gap-2 mb-2">
                            <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.badge}`}>
                              {meta.icon}
                              {meta.label}
                            </span>
                          </div>
                          <div className="text-sm font-medium text-slate-800 truncate">{getSourceTitle(source)}</div>
                          <div className="text-xs text-slate-400 mt-1 line-clamp-2">{getSourcePreview(source)}</div>
                        </button>
                      );
                    })}
                  </div>

                  <div className="flex-1 overflow-y-auto p-4">
                    {selectedSource ? (
                      <div className="space-y-4">
                        <div>
                          <div className="flex flex-wrap items-center gap-2 mb-3">
                            {(() => {
                              const meta = getSourceKindMeta(selectedSource.kind);
                              return (
                                <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.badge}`}>
                                  {meta.icon}
                                  {meta.label}
                                </span>
                              );
                            })()}
                          </div>
                          <h3 className="text-lg font-semibold text-slate-900 break-words">{getSourceTitle(selectedSource)}</h3>
                          <div className="flex flex-wrap items-center gap-2 mt-3 text-[11px] text-slate-400">
                            <span>{selectedSource.id}</span>
                            {selectedSource.session_id && <span>session {selectedSource.session_id}</span>}
                            {selectedSource.timestamp && <span>{new Date(selectedSource.timestamp * 1000).toLocaleString()}</span>}
                            {typeof selectedSource.relevance === 'number' && <span>relevance {selectedSource.relevance.toFixed(2)}</span>}
                          </div>
                        </div>

                        {(selectedSource.file_path || selectedSource.line_range || selectedSource.source_type) && (
                          <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 space-y-2 text-sm text-slate-600">
                            {selectedSource.source_type && <div><span className="text-slate-400">source_type</span> {selectedSource.source_type}</div>}
                            {selectedSource.file_path && <div><span className="text-slate-400">file</span> {selectedSource.file_path}</div>}
                            {selectedSource.line_range && <div><span className="text-slate-400">line</span> {selectedSource.line_range}</div>}
                            {selectedSource.node_label && <div><span className="text-slate-400">node</span> {selectedSource.node_label}</div>}
                          </div>
                        )}

                        {selectedSource.summary && (
                          <div>
                            <div className="text-xs font-medium uppercase tracking-wider text-slate-400 mb-2">Summary</div>
                            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-6 text-slate-600 whitespace-pre-wrap break-words">{selectedSource.summary}</div>
                          </div>
                        )}

                        <div>
                          <div className="text-xs font-medium uppercase tracking-wider text-slate-400 mb-2">Content</div>
                          <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-6 text-slate-600 whitespace-pre-wrap break-words min-h-32">
                            {selectedSource.content || selectedSource.summary || '这个来源暂时没有展开内容。'}
                          </div>
                        </div>
                      </div>
                    ) : (
                      <EmptyState icon={<ExternalLink size={32} />} title="选择来源卡片" description="右侧会展示来源详情和可追溯内容。" />
                    )}
                  </div>
                </>
              )}
            </aside>
          </div>
        )}
      </div>
    </div>
  );
};

const AgentsView = ({ showToast, onNavigate }: { showToast: (msg: string) => void, onNavigate: (m: ViewMode) => void }) => {
    const { data: agents, loading, error, refetch } = useApi<Agent[]>(
        (signal) => listAgents(signal), [],
    );

    const agentList = agents ?? [];

    const getRoleColor = (role: string) => {
        const hash = role.split('').reduce((a, c) => a + c.charCodeAt(0), 0);
        const colors = ['blue', 'emerald', 'amber', 'pink', 'violet', 'indigo'];
        return colors[hash % colors.length];
    };

    const getStatusBadge = (status: string) => {
        switch (status) {
            case 'running': return 'text-blue-600 bg-blue-50 border-blue-100';
            case 'idle': return 'text-slate-500 bg-slate-50 border-slate-100';
            case 'error': return 'text-red-600 bg-red-50 border-red-100';
            default: return 'text-slate-400 bg-slate-50 border-slate-100';
        }
    };

    return (
        <div className="flex-1 p-6 md:p-12 overflow-y-auto bg-slate-50 relative pb-24 md:pb-12">
            <div className="absolute inset-0 opacity-5" style={{ backgroundImage: 'radial-gradient(#000 1px, transparent 1px)', backgroundSize: '30px 30px' }}></div>
            <div className="max-w-6xl mx-auto relative z-10">
                <div className="text-center mb-10 md:mb-16">
                    <h1 className="text-3xl md:text-4xl font-bold text-slate-900 mb-4">Agents</h1>
                    <p className="text-sm md:text-base text-slate-500 max-w-2xl mx-auto">Active AI agents in your workspace</p>
                </div>
                {loading ? <LoadingSkeleton variant="card" count={4} /> :
                 error ? <ErrorState message={error.message} onRetry={refetch} /> :
                 agentList.length === 0 ? (
                    <EmptyState icon={<Users size={48} />} title="No agents" description="No agents are currently active" />
                 ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 md:gap-8">
                    {agentList.map(agent => {
                        const color = getRoleColor(agent.role);
                        return (
                            <div key={agent.id} className="bg-white/70 backdrop-blur-md border border-white p-6 md:p-8 rounded-[32px] shadow-xl shadow-slate-200/50 hover:-translate-y-2 transition-all duration-300 group relative overflow-hidden">
                                <div className="flex justify-between items-start mb-6">
                                    <div className={`size-10 rounded-full bg-${color}-200 flex items-center justify-center text-${color}-700 font-bold text-sm`}>
                                        {agent.name.charAt(0).toUpperCase()}
                                    </div>
                                    <span className={`px-3 py-1 text-[10px] font-bold uppercase tracking-widest rounded-full border ${getStatusBadge(agent.status)}`}>
                                        {agent.status}
                                    </span>
                                </div>
                                <h3 className="text-xl font-bold text-slate-800 mb-2">{agent.name}</h3>
                                <p className="text-sm text-slate-500 mb-4">{agent.model} • {agent.role}</p>
                                <div className="flex gap-4 text-xs text-slate-400">
                                    <span>{agent.task_count} tasks</span>
                                    <span>{agent.tokens_used.toLocaleString()} tokens</span>
                                    {agent.error_count > 0 && <span className="text-red-400">{agent.error_count} errors</span>}
                                </div>
                            </div>
                        );
                    })}
                </div>
                 )}
            </div>
        </div>
    );
}

const PlanView = ({
  workflowContext,
  onOpenModal,
  showToast,
  onReplayData,
}: {
  workflowContext?: AppWorkflowContext | null,
  onOpenModal: () => void,
  showToast: (m: string) => void,
  onReplayData: (replay: WorkflowReplayData) => void,
}) => {
    const [mobileTab, setMobileTab] = useState<'editor' | 'status'>('editor');
    const workflowProject = workflowContext?.project ?? null;
    const workflowOverview = workflowContext?.overview ?? null;
    const workflowTimelineResponse = workflowContext?.timeline ?? null;
    const workflowReplayResponse = workflowContext?.replay ?? null;
    const [selectedPlanId, setSelectedPlanId] = useState<string | null>(workflowOverview?.plans?.[0]?.id ?? null);
    const [knowledgeScenario, setKnowledgeScenario] = useState<'startup' | 'debug' | 'reuse'>('startup');
    const [selectedSourceKey, setSelectedSourceKey] = useState<string | null>(null);
    const [graphSourceNodeId, setGraphSourceNodeId] = useState<string | null>(null);
    const [selectedGraphNodeId, setSelectedGraphNodeId] = useState<string | null>(null);
    const [packDraft, setPackDraft] = useState('');
    const [exportedPack, setExportedPack] = useState<SAMGGraph | null>(null);
    const [lastImportResult, setLastImportResult] = useState<SAMGGraphImportResult | null>(null);
    const sourceCardsRef = useRef<HTMLDivElement | null>(null);
    const graphJumpRef = useRef<HTMLDivElement | null>(null);
    const knowledgePackRef = useRef<HTMLDivElement | null>(null);

    const { data: plansResponse, loading: plansLoading, error: plansError, refetch: refetchPlans } = useApi<PlanListResponse>(
        (signal) => workflowProject
            ? Promise.resolve({ plans: workflowOverview?.plans ?? [], total: workflowOverview?.plans?.length ?? 0, has_more: false })
            : listPlans(signal),
        [workflowProject?.id, workflowOverview?.plans?.length ?? 0],
    );
    const plans = plansResponse?.plans ?? [];

    const { data: tasksResponse, loading: tasksLoading, refetch: refetchTasks } = useApi<PlanTaskListResponse>(
        (signal) => {
            if (workflowProject) {
                const workflowTasks = !selectedPlanId
                    ? (workflowOverview?.tasks ?? [])
                    : (workflowOverview?.tasks ?? []).filter(task => task.plan_id === selectedPlanId);
                return Promise.resolve({ tasks: workflowTasks, total: workflowTasks.length });
            }
            return selectedPlanId ? getPlanTasks(selectedPlanId, signal) : Promise.resolve({ tasks: [], total: 0 });
        },
        [workflowProject?.id, selectedPlanId, workflowOverview?.tasks?.length ?? 0],
        { enabled: workflowProject ? Boolean(workflowOverview) : !!selectedPlanId },
    );
    const tasks = tasksResponse?.tasks ?? [];

    const selectedPlan = workflowProject
        ? (workflowOverview?.plans.find(p => p.id === selectedPlanId) ?? workflowOverview?.plans?.[0] ?? null)
        : (plans.find(p => p.id === selectedPlanId) ?? plans[0] ?? null);

    useEffect(() => {
        if (workflowOverview?.plans?.length) {
            setSelectedPlanId(prev => prev ?? workflowOverview.plans[0].id);
        }
    }, [workflowOverview]);

    useEffect(() => {
        if (!workflowProject && plans.length > 0 && !selectedPlanId) {
            setSelectedPlanId(plans[0].id);
        }
    }, [workflowProject, plans, selectedPlanId]);

    const handleCreatePlan = async () => {
        if (workflowProject) {
            showToast('Workflow-backed project is read-only in this MVP');
            return;
        }
        try {
            const plan = await createPlan({ title: 'New Plan', description: '' });
            showToast('Plan created');
            refetchPlans();
            setSelectedPlanId(plan.id);
        } catch { showToast('Failed to create plan'); }
    };

    const handleTaskStatusToggle = async (task: PlanTask) => {
        if (!selectedPlanId || workflowProject) return;
        const nextStatus = task.status === 'completed' ? 'pending' : task.status === 'pending' ? 'in_progress' : 'completed';
        try {
            await updatePlanTask(selectedPlanId, task.id, { status: nextStatus } as Partial<PlanTask>);
            refetchTasks();
        } catch { showToast('Failed to update task'); }
    };

    const getTaskStatusStyle = (status: string) => {
        switch (status) {
            case 'completed': return { bg: 'bg-green-50', border: 'border-green-100', badge: 'text-green-600 bg-green-50', icon: <Check size={16} className="text-green-500" />, label: 'DONE' };
            case 'in_progress': return { bg: 'bg-blue-50/50', border: 'border-blue-100', badge: 'text-blue-600 bg-blue-100 animate-pulse', icon: <Code size={16} className="text-blue-600" />, label: 'RUNNING' };
            case 'failed': return { bg: 'bg-red-50', border: 'border-red-100', badge: 'text-red-600 bg-red-50', icon: <X size={16} className="text-red-500" />, label: 'FAILED' };
            case 'blocked': return { bg: 'bg-amber-50', border: 'border-amber-100', badge: 'text-amber-600 bg-amber-50', icon: <Shield size={16} className="text-amber-500" />, label: 'BLOCKED' };
            default: return { bg: 'bg-white', border: 'border-slate-200', badge: 'text-slate-400 bg-slate-50', icon: <Clock size={16} className="text-slate-400" />, label: 'PENDING' };
        }
    };

    const taskList = tasks ?? [];
    const completedCount = taskList.filter(t => t.status === 'completed').length;
    const workflow = selectedPlan ? extractWorkflowMetadata(selectedPlan.metadata) : { approval: [], decisions: [], timeline: [] } as WorkflowMetadata;
    const backendProjectWorkflow = extractWorkflowMetadata(workflowProject?.metadata);
    const overviewDecisions = workflowOverview?.plans.map((plan, index) => ({
        id: `overview-plan-${plan.id}`,
        summary: `Plan ${index + 1}: ${plan.title}`,
        owner: 'Workflow overview',
        reason: plan.description || `${plan.completed_count}/${plan.task_count} tasks completed`,
        timestamp: plan.updated_at,
    })) ?? [];
    const backendTimeline = workflowTimelineResponse?.events.map(workflowTimelineEventToItem) ?? [];
    const replayTimeline = workflowReplayResponse?.events.map(workflowReplayEventToItem) ?? [];
    const latestAuditTimeline = workflowOverview?.latest_audit ? [{
        id: `audit-${workflowOverview.latest_audit.id}`,
        lane: 'audit' as const,
        title: workflowOverview.latest_audit.action || workflowOverview.latest_audit.event_type,
        message: String(workflowOverview.latest_audit.resource?.name || workflowOverview.latest_audit.resource?.id || 'Audit evidence'),
        timestamp: workflowOverview.latest_audit.timestamp,
        status: workflowOverview.latest_audit.outcome,
        actor: workflowOverview.latest_audit.actor?.name || workflowOverview.latest_audit.actor?.id,
        evidence: workflowOverview.latest_audit.trace?.path,
        sourceId: workflowOverview.latest_audit.id,
    }] : [];
    const fallbackWorkflow = buildFallbackWorkflowMetadata(workflowProject, selectedPlan, taskList);
    const mergedWorkflow: WorkflowMetadata = {
        ...fallbackWorkflow,
        ...backendProjectWorkflow,
        ...workflow,
        workflow_id: workflowProject?.id ?? backendProjectWorkflow.workflow_id ?? workflow.workflow_id ?? fallbackWorkflow.workflow_id,
        workflow_title: backendProjectWorkflow.workflow_title ?? workflow.workflow_title ?? fallbackWorkflow.workflow_title,
        blueprint: backendProjectWorkflow.blueprint ?? workflow.blueprint ?? fallbackWorkflow.blueprint,
        template: backendProjectWorkflow.template ?? workflow.template ?? fallbackWorkflow.template,
        replay_session_id: workflowReplayResponse?.session_id ?? backendProjectWorkflow.replay_session_id ?? workflow.replay_session_id ?? fallbackWorkflow.replay_session_id,
        approval: backendProjectWorkflow.approval?.length
            ? backendProjectWorkflow.approval
            : workflow.approval && workflow.approval.length > 0
                ? workflow.approval
                : fallbackWorkflow.approval,
        decisions: [
            ...(backendProjectWorkflow.decisions ?? []),
            ...(workflow.decisions ?? []),
            ...overviewDecisions,
            ...(fallbackWorkflow.decisions ?? []),
        ].filter((item, index, arr) => arr.findIndex(candidate => candidate.id === item.id) === index),
        timeline: [
            ...(workflow.timeline ?? []),
            ...backendTimeline,
            ...replayTimeline,
            ...latestAuditTimeline,
            ...(fallbackWorkflow.timeline ?? []),
        ],
    };
    const timeline = sortReplayItems(mergedWorkflow.timeline ?? []);
    const dependencyRows = taskList.filter(task => (task.dependencies?.length ?? 0) > 0);
    const activeTask = taskList.find((task) => task.status === 'in_progress') ?? taskList[0] ?? null;
    const blockedTasks = taskList.filter((task) => task.status === 'blocked');
    const latestTimelineItem = timeline[timeline.length - 1] ?? null;
    const knowledgeSessionId = workflowReplayResponse?.session_id
        ?? backendProjectWorkflow.replay_session_id
        ?? workflow.replay_session_id
        ?? fallbackWorkflow.replay_session_id
        ?? undefined;
    const scenarioMeta = {
        startup: {
            label: '任务启动',
            caption: '自动召回来源卡片、图谱跳转与知识包规则。',
            query: `启动任务 ${activeTask?.title ?? selectedPlan?.title ?? '当前计划'} 所需的背景、依赖、来源与图谱关系`,
        },
        debug: {
            label: '问题排查',
            caption: '聚焦阻塞、异常、依赖链与证据回放。',
            query: `排查 ${activeTask?.title ?? selectedPlan?.title ?? '当前计划'} 的阻塞、历史问题、依赖与证据`,
        },
        reuse: {
            label: '历史复用',
            caption: '汇总可迁移经验、实现线索与 pack 边界。',
            query: `复用 ${selectedPlan?.title ?? workflowProject?.title ?? '当前计划'} 相关的历史实现、知识包与可追溯来源`,
        },
    } as const;
    const activeScenario = scenarioMeta[knowledgeScenario];
    const knowledgeQuery = activeScenario.query.trim();

    const buildSourceKey = (source: MemoryAgentSource) => [
        source.kind,
        source.id,
        source.node_id ?? '',
        source.source_id ?? '',
    ].join(':');

    const mergeSources = (
        primary: MemoryAgentSource[] = [],
        secondary: MemoryAgentSource[] = [],
    ) => {
        const merged = [...primary];
        const seen = new Set(primary.map((source) => buildSourceKey(source)));
        for (const source of secondary) {
            const key = buildSourceKey(source);
            if (seen.has(key)) continue;
            merged.push(source);
            seen.add(key);
        }
        return merged;
    };

    const mergeGraphNodes = (
        primary: NonNullable<MemoryAgentRetrieveResult['samg_nodes']> = [],
        secondary: QueryMemoryNode[] = [],
    ) => {
        const merged = [...primary];
        const seen = new Set(primary.map((node) => node.id));
        for (const node of secondary) {
            if (seen.has(node.id)) continue;
            merged.push(node);
            seen.add(node.id);
        }
        return merged;
    };

    const getSourceKindMeta = (kind: MemoryAgentSource['kind']) => {
        switch (kind) {
            case 'atomic_memory':
                return {
                    label: 'Atomic Memory',
                    icon: <Layers size={14} />,
                    badge: 'bg-red-50 text-red-600 border-red-200',
                };
            case 'samg_pointer':
                return {
                    label: 'SAMG Pointer',
                    icon: <Activity size={14} />,
                    badge: 'bg-violet-50 text-violet-600 border-violet-200',
                };
            default:
                return {
                    label: 'Raw Archive',
                    icon: <Archive size={14} />,
                    badge: 'bg-slate-100 text-slate-600 border-slate-200',
                };
        }
    };

    const getSourceTitle = (source: MemoryAgentSource) => source.title || source.node_label || source.summary || source.source_id || source.id;
    const getSourcePreview = (source: MemoryAgentSource) => {
        const preview = source.summary || source.content || source.source_id || source.id;
        return preview.length > 120 ? `${preview.slice(0, 120)}...` : preview;
    };
    const formatNodeTypes = (types?: string[]) => types?.filter(Boolean).join(' · ') || 'Unclassified node';
    const formatNodeProperties = (properties?: Record<string, unknown>) => Object.entries(properties ?? {}).filter(([, value]) => value !== undefined && value !== null);
    const buildNodeSummary = (node: QueryMemoryNode | null) => {
        if (!node) return 'No node selected';
        return node.description || formatNodeTypes(node['@type']) || node.id;
    };
    const jumpToSection = (ref: React.RefObject<HTMLDivElement | null>) => {
        ref.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };
    const openSourceFromNode = (nodeId: string) => {
        const firstSource = knowledgeSources.find((source) => source.node_id === nodeId);
        if (!firstSource) return;
        setSelectedSourceKey(buildSourceKey(firstSource));
        jumpToSection(sourceCardsRef);
    };
    const focusGraphNode = (nodeId: string) => {
        if (!knowledgeNodes.some((node) => node.id === nodeId)) return;
        setSelectedGraphNodeId(nodeId);
        setGraphSourceNodeId((prev) => {
            if (prev && prev !== nodeId && knowledgeNodes.some((node) => node.id === prev)) {
                return prev;
            }
            return knowledgeNodes.find((node) => node.id !== nodeId)?.id ?? nodeId;
        });
        jumpToSection(graphJumpRef);
    };
    const parseEmbeddedWiki = (contextBlock: string) => {
        const lines = contextBlock
            .split('\n')
            .map((line) => line.trim())
            .filter(Boolean);
        return lines.map((line, index) => {
            const lower = line.toLowerCase();
            if (lower.startsWith('pack rule:')) {
                return {
                    id: `wiki-pack-${index}`,
                    label: 'Pack rule',
                    content: line.slice('pack rule:'.length).trim(),
                    actionLabel: '查看 pack',
                    onClick: () => jumpToSection(knowledgePackRef),
                };
            }
            if (lower.startsWith('latest evidence:')) {
                return {
                    id: `wiki-evidence-${index}`,
                    label: 'Latest evidence',
                    content: line.slice('latest evidence:'.length).trim(),
                    actionLabel: selectedSource ? '打开来源' : '查看来源',
                    onClick: () => jumpToSection(sourceCardsRef),
                };
            }
            if (lower.startsWith('dependencies:')) {
                return {
                    id: `wiki-graph-${index}`,
                    label: 'Dependencies',
                    content: line.slice('dependencies:'.length).trim(),
                    actionLabel: '查看 graph',
                    onClick: () => jumpToSection(graphJumpRef),
                };
            }
            if (line.startsWith('[atomic|') || line.startsWith('[samg|') || line.startsWith('[raw_archive|')) {
                const sourceIndex = knowledgeSources.findIndex((source) => {
                    const preview = source.content || source.summary || '';
                    return preview && line.includes(preview.slice(0, Math.min(preview.length, 24)));
                });
                return {
                    id: `wiki-source-${index}`,
                    label: line.startsWith('[samg|') ? 'Graph memory' : line.startsWith('[atomic|') ? 'Atomic memory' : 'Raw archive',
                    content: line.replace(/^\[[^\]]+\]\s*/, ''),
                    actionLabel: '打开来源',
                    onClick: () => {
                        if (sourceIndex >= 0) {
                            setSelectedSourceKey(buildSourceKey(knowledgeSources[sourceIndex]));
                        }
                        jumpToSection(sourceCardsRef);
                    },
                };
            }
            const matchedNode = knowledgeNodes.find((node) => line.includes(node.label) || line.includes(node.id));
            if (matchedNode) {
                return {
                    id: `wiki-node-${matchedNode.id}-${index}`,
                    label: matchedNode.label,
                    content: line,
                    actionLabel: '跳到 graph',
                    onClick: () => focusGraphNode(matchedNode.id),
                };
            }
            return {
                id: `wiki-line-${index}`,
                label: 'Context',
                content: line,
                actionLabel: '',
                onClick: undefined,
            };
        });
    };

    const { data: memoryRetrieve, loading: memoryRetrieveLoading, error: memoryRetrieveError, refetch: refetchMemoryRetrieve } = useApi<MemoryAgentRetrieveResult>(
        (signal) => retrieveMemoryAgent(
            {
                query: knowledgeQuery,
                session_id: knowledgeSessionId,
                max_results: 8,
            },
            signal,
        ),
        [knowledgeQuery, knowledgeSessionId],
        { enabled: knowledgeQuery.length > 0 },
    );

    const { data: memoryContext, loading: memoryContextLoading, error: memoryContextError, refetch: refetchMemoryContext } = useApi<MemoryAgentContextResult>(
        (signal) => buildMemoryAgentContext(
            {
                session_id: knowledgeSessionId!,
                query: knowledgeQuery,
                max_tokens: 700,
            },
            signal,
        ),
        [knowledgeQuery, knowledgeSessionId],
        { enabled: Boolean(knowledgeSessionId) },
    );

    const knowledgeNodes = mergeGraphNodes(
        memoryRetrieve?.samg_nodes ?? [],
        memoryContext?.samg_nodes ?? [],
    );
    const pointerSources = knowledgeNodes.flatMap((node) =>
        (node.pointers ?? []).map((pointer, index) => ({
            kind: 'samg_pointer' as const,
            id: `${node.id}:${pointer.source_id}:${pointer.line_range ?? index}`,
            title: node.label,
            summary: pointer.summary,
            content: pointer.resolved_content || pointer.summary,
            session_id: pointer.session_id,
            timestamp: undefined,
            node_id: node.id,
            node_label: node.label,
            source_id: pointer.source_id,
            source_type: pointer.source_type,
            file_path: pointer.file_path,
            line_range: pointer.line_range,
            relevance: pointer.relevance,
        })),
    );
    const knowledgeSources = mergeSources(
        mergeSources(memoryRetrieve?.sources ?? [], memoryContext?.sources ?? []),
        pointerSources,
    );
    const selectedSource = knowledgeSources.find((source) => buildSourceKey(source) === selectedSourceKey) ?? null;
    const knowledgeContextBlock = memoryContext?.context_block?.trim()
        || [
            `Scenario: ${activeScenario.label}`,
            `Focus task: ${activeTask?.title ?? selectedPlan?.title ?? 'No active task'}`,
            `Dependencies: ${dependencyRows.length}`,
            `Latest evidence: ${latestTimelineItem?.title ?? 'No timeline evidence yet'}`,
            `Pack rule: export/import works on SAMG graph; pointer-level details stay on source cards.`,
        ].join('\n');
    const embeddedWikiEntries = parseEmbeddedWiki(knowledgeContextBlock);

    useEffect(() => {
        if (knowledgeSources.length === 0) {
            setSelectedSourceKey(null);
            return;
        }
        if (!selectedSourceKey || !knowledgeSources.some((source) => buildSourceKey(source) === selectedSourceKey)) {
            setSelectedSourceKey(buildSourceKey(knowledgeSources[0]));
        }
    }, [knowledgeSources, selectedSourceKey]);

    useEffect(() => {
        if (knowledgeNodes.length === 0) {
            setGraphSourceNodeId(null);
            setSelectedGraphNodeId(null);
            return;
        }
        setGraphSourceNodeId((prev) => prev && knowledgeNodes.some((node) => node.id === prev) ? prev : knowledgeNodes[0].id);
        setSelectedGraphNodeId((prev) => {
            if (prev && knowledgeNodes.some((node) => node.id === prev)) return prev;
            return knowledgeNodes[1]?.id ?? knowledgeNodes[0].id;
        });
    }, [knowledgeNodes]);

    const shouldLoadPath = Boolean(graphSourceNodeId && selectedGraphNodeId && graphSourceNodeId !== selectedGraphNodeId);
    const { data: graphPath, loading: graphPathLoading, error: graphPathError, refetch: refetchGraphPath } = useApi<SAMGPathResponse>(
        (signal) => findPaths(
            {
                source_id: graphSourceNodeId!,
                target_id: selectedGraphNodeId!,
                max_hops: 4,
            },
            signal,
        ),
        [graphSourceNodeId, selectedGraphNodeId],
        { enabled: shouldLoadPath },
    );

    const exportPackMutation = useMutation<void, SAMGGraph>((_input, signal) => exportGraph(signal));
    const importPackMutation = useMutation<SAMGGraph, SAMGGraphImportResult>((graph, signal) => importGraph(graph, signal));

    const refetchKnowledgeRail = () => {
        refetchMemoryRetrieve();
        if (knowledgeSessionId) {
            refetchMemoryContext();
        }
        if (shouldLoadPath) {
            refetchGraphPath();
        }
    };

    const handleExportPack = async () => {
        try {
            const graph = await exportPackMutation.execute(undefined);
            setExportedPack(graph);
            setPackDraft(JSON.stringify(graph, null, 2));
            showToast(`Knowledge pack exported (${graph.metadata?.triple_count ?? graph['@graph']?.length ?? 0} triples)`);
        } catch {
            showToast('Failed to export knowledge pack');
        }
    };

    const handleImportPack = async () => {
        try {
            const parsed = JSON.parse(packDraft) as SAMGGraph;
            if (!parsed || typeof parsed !== 'object' || !Array.isArray(parsed['@graph'])) {
                throw new Error('Knowledge pack must be a SAMG JSON-LD graph');
            }
            const result = await importPackMutation.execute(parsed);
            setExportedPack(parsed);
            setLastImportResult(result);
            showToast(`Knowledge pack imported (${result.triple_count} triples · dedup ${result.deduplicated_count} · total ${result.total_triples})`);
            refetchKnowledgeRail();
        } catch (error) {
            showToast(error instanceof Error ? error.message : 'Failed to import knowledge pack');
        }
    };

    const graphSourceNode = knowledgeNodes.find((node) => node.id === graphSourceNodeId) ?? null;
    const selectedGraphNode = knowledgeNodes.find((node) => node.id === selectedGraphNodeId) ?? null;
    const knowledgeLoading = memoryRetrieveLoading || memoryContextLoading || graphPathLoading;
    const knowledgeError = memoryRetrieveError ?? memoryContextError ?? graphPathError;
    const graphPaths = graphPath?.paths ?? [];
    const packRules = [
        'Pack boundary is SAMG graph export/import, not a parallel knowledge store.',
        'Triple source metadata is carried with the graph and remains the main provenance anchor.',
        'Duplicate resolution stays backend-owned: higher-confidence triples win in memory and persisted triples are replaced by id.',
        'Pointer-level detail should still be verified from source cards and node details after import.',
    ];

    const handleOpenReplay = () => {
        onReplayData(buildWorkflowReplayData(selectedPlan?.title || workflowProject?.title || 'Plan replay', mergedWorkflow, timeline));
        onOpenModal();
        showToast(workflowProject ? 'Workflow replay opened' : 'Execution started');
    };

    return (
        <div className="flex-1 flex flex-col h-full bg-slate-50 relative z-0 overflow-hidden pb-16 md:pb-0">
            <header className="h-16 flex items-center justify-between px-4 md:px-8 bg-white border-b border-slate-100 z-20 shrink-0">
                <div className="flex items-center gap-3">
                    <div className="size-9 bg-gradient-to-br from-blue-500 to-indigo-600 rounded-xl flex items-center justify-center text-white shadow-md shadow-blue-500/20">
                         <LayoutGrid size={18} />
                    </div>
                    <h1 className="font-bold text-slate-800 text-lg hidden sm:block">CodeFlow Plan</h1>
                    {plans.length > 1 && (
                        <select
                            value={selectedPlanId ?? ''}
                            onChange={e => setSelectedPlanId(e.target.value)}
                            className="ml-2 text-sm border border-slate-200 rounded-lg px-2 py-1 bg-white"
                        >
                            {plans.map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
                        </select>
                    )}
                </div>

                <div className="flex items-center">
                   <div className="flex flex-col items-center group cursor-pointer">
                        <div className="size-8 rounded-full bg-blue-100 text-blue-600 flex items-center justify-center mb-1 shadow-sm">
                            <Lightbulb size={16} />
                        </div>
                        <span className="text-[10px] font-bold text-blue-600 uppercase">Intent</span>
                   </div>
                   <div className="w-8 md:w-16 h-0.5 bg-blue-200 mx-2 mb-4"></div>
                   <div className="flex flex-col items-center group cursor-pointer">
                        <div className="size-10 rounded-full bg-gradient-to-tr from-blue-600 to-indigo-600 text-white flex items-center justify-center mb-1 shadow-lg shadow-blue-500/30 scale-110">
                            <Zap size={20} />
                        </div>
                        <span className="text-[10px] font-bold text-indigo-600 uppercase">Plan</span>
                   </div>
                   <div className="w-8 md:w-16 h-0.5 bg-slate-200 mx-2 mb-4"></div>
                   <div className="flex flex-col items-center opacity-40">
                        <div className="size-8 rounded-full bg-white border border-slate-200 text-slate-400 flex items-center justify-center mb-1">
                            <Code size={16} />
                        </div>
                        <span className="text-[10px] font-bold text-slate-400 uppercase">Exec</span>
                   </div>
                </div>

                <div className="flex items-center gap-3">
                     <button className="size-9 rounded-full hover:bg-slate-100 flex items-center justify-center text-slate-500">
                        <Bell size={18} />
                     </button>
                     <div className="hidden sm:block h-8 w-px bg-slate-200"></div>
                     <div className="size-9 rounded-full bg-slate-200 overflow-hidden hidden sm:block">
                         <div className="w-full h-full bg-gradient-to-br from-slate-400 to-slate-500"></div>
                     </div>
                </div>
            </header>

            <div className="md:hidden flex border-b border-slate-200 bg-white">
                <button
                    className={`flex-1 py-3 text-sm font-bold ${mobileTab === 'editor' ? 'text-blue-600 border-b-2 border-blue-500' : 'text-slate-500'}`}
                    onClick={() => setMobileTab('editor')}
                >
                    Editor
                </button>
                <button
                    className={`flex-1 py-3 text-sm font-bold ${mobileTab === 'status' ? 'text-blue-600 border-b-2 border-blue-500' : 'text-slate-500'}`}
                    onClick={() => setMobileTab('status')}
                >
                    Runtime & Status
                </button>
            </div>

            <div className="flex-1 flex overflow-hidden relative">
                <div className={`flex-1 overflow-y-auto p-4 md:p-10 flex flex-col items-center transition-opacity duration-300 ${mobileTab === 'status' ? 'opacity-0 absolute pointer-events-none' : 'opacity-100'} md:opacity-100 md:static md:pointer-events-auto`}>
                    {plansLoading ? (
                        <LoadingSkeleton variant="text" count={3} />
                    ) : plansError ? (
                        <ErrorState message={plansError.message} onRetry={refetchPlans} />
                    ) : !selectedPlan ? (
                        <EmptyState
                            icon={<LayoutGrid size={48} />}
                            title="No plans yet"
                            description="Create a plan to get started"
                            action={{ label: 'New Plan', onClick: handleCreatePlan }}
                        />
                    ) : (
                    <div className="w-full max-w-4xl space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
                         <div className="flex items-start justify-between flex-wrap gap-2">
                            <div>
                                <h2 className="text-2xl md:text-3xl font-bold text-slate-900 tracking-tight mb-2">{selectedPlan.title}</h2>
                                <p className="text-sm md:text-base text-slate-500">{selectedPlan.description || 'No description provided'}</p>
                            </div>
                            <span className={`px-3 py-1 rounded-full text-xs font-bold flex items-center gap-2 ${
                                selectedPlan.status === 'active' ? 'bg-green-50 text-green-700 border border-green-200' :
                                selectedPlan.status === 'completed' ? 'bg-blue-50 text-blue-700 border border-blue-200' :
                                'bg-slate-50 text-slate-600 border border-slate-200'
                            }`}>
                                {selectedPlan.status === 'active' && <span className="size-2 bg-green-500 rounded-full animate-pulse"></span>}
                                {selectedPlan.status}
                            </span>
                         </div>

                         <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                            <div className="bg-white border border-slate-200 rounded-2xl shadow-sm overflow-hidden min-h-[320px] flex flex-col group focus-within:ring-4 focus-within:ring-blue-500/10 transition-all">
                                 <div className="bg-slate-50 border-b border-slate-100 p-2 flex gap-1">
                                    <button className="p-1.5 hover:bg-white rounded text-slate-400 hover:text-slate-700 transition-colors"><Edit3 size={16} /></button>
                                    <div className="w-px h-4 bg-slate-200 my-auto mx-1"></div>
                                    <button className="p-1.5 hover:bg-white rounded text-slate-400 hover:text-slate-700 transition-colors"><Grid size={16} /></button>
                                 </div>
                                 <textarea
                                    className="flex-1 w-full p-6 md:p-8 resize-none border-none focus:ring-0 text-slate-700 leading-relaxed font-mono text-xs md:text-sm"
                                    defaultValue={selectedPlan.description || '# Plan\nDescribe your plan here...'}
                                 />
                            </div>
                            <div className="space-y-4">
                                <div className="rounded-2xl border border-blue-200 bg-gradient-to-br from-blue-50 to-white p-5 space-y-4">
                                    <div className="flex items-start justify-between gap-3 flex-wrap">
                                        <div>
                                            <div className="flex items-center gap-2 text-[10px] font-bold uppercase tracking-[0.2em] text-blue-600 mb-2">
                                                <Database size={12} /> Knowledge rail
                                            </div>
                                            <h3 className="text-sm font-bold text-slate-800">{activeScenario.label}</h3>
                                            <p className="text-xs text-slate-500 mt-1">{activeScenario.caption}</p>
                                        </div>
                                        <button
                                            onClick={refetchKnowledgeRail}
                                            disabled={knowledgeLoading}
                                            className="px-3 py-1.5 rounded-lg border border-blue-200 bg-white text-xs font-bold text-blue-600 disabled:opacity-50"
                                        >
                                            {knowledgeLoading ? 'Loading...' : 'Refresh'}
                                        </button>
                                    </div>

                                    <div className="flex flex-wrap gap-2">
                                        {(Object.entries(scenarioMeta) as Array<[keyof typeof scenarioMeta, typeof activeScenario]>).map(([key, meta]) => {
                                            const active = key === knowledgeScenario;
                                            return (
                                                <button
                                                    key={key}
                                                    onClick={() => setKnowledgeScenario(key)}
                                                    className={`px-3 py-2 rounded-xl border text-xs font-semibold transition-colors ${
                                                        active
                                                            ? 'border-blue-200 bg-blue-600 text-white'
                                                            : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'
                                                    }`}
                                                >
                                                    {meta.label}
                                                </button>
                                            );
                                        })}
                                    </div>

                                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
                                        <div className="rounded-xl border border-blue-100 bg-white/80 p-3">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Sources</div>
                                            <div className="text-lg font-bold text-slate-800">{knowledgeSources.length}</div>
                                        </div>
                                        <div className="rounded-xl border border-blue-100 bg-white/80 p-3">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Graph nodes</div>
                                            <div className="text-lg font-bold text-slate-800">{knowledgeNodes.length}</div>
                                        </div>
                                        <div className="rounded-xl border border-blue-100 bg-white/80 p-3">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Blocked</div>
                                            <div className="text-lg font-bold text-slate-800">{blockedTasks.length}</div>
                                        </div>
                                        <div className="rounded-xl border border-blue-100 bg-white/80 p-3">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Replay session</div>
                                            <div className="text-xs font-semibold text-slate-700 break-all">{knowledgeSessionId || 'Unavailable'}</div>
                                        </div>
                                    </div>

                                    <div className="rounded-2xl border border-slate-200 bg-white p-4 space-y-3">
                                        <div className="flex items-center justify-between gap-2 flex-wrap">
                                            <div>
                                                <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Embedded wiki</div>
                                                <div className="text-sm font-semibold text-slate-800">Task-start context block</div>
                                            </div>
                                            <span className="text-[10px] px-2 py-0.5 rounded-full bg-slate-100 text-slate-500 border border-slate-200">{activeTask?.title ?? selectedPlan.title}</span>
                                        </div>
                                        <div className="grid gap-3">
                                            {embeddedWikiEntries.map((entry) => (
                                                <div key={entry.id} className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
                                                    <div className="flex items-start justify-between gap-3">
                                                        <div className="min-w-0">
                                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">{entry.label}</div>
                                                            <div className="leading-6 whitespace-pre-wrap break-words">{entry.content}</div>
                                                        </div>
                                                        {entry.onClick && entry.actionLabel ? (
                                                            <button
                                                                type="button"
                                                                onClick={entry.onClick}
                                                                className="shrink-0 inline-flex items-center gap-1 rounded-lg border border-blue-200 bg-white px-2.5 py-1 text-[11px] font-semibold text-blue-600 hover:bg-blue-50"
                                                            >
                                                                {entry.actionLabel}
                                                                <ExternalLink size={12} />
                                                            </button>
                                                        ) : null}
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                        {knowledgeError && (
                                            <div className="text-xs text-red-600 rounded-xl border border-red-200 bg-red-50 p-3">
                                                {knowledgeError.message}
                                            </div>
                                        )}
                                    </div>
                                </div>

                                <div ref={sourceCardsRef} className="rounded-2xl border border-slate-200 bg-white overflow-hidden">
                                    <div className="p-4 border-b border-slate-200">
                                        <h3 className="text-sm font-bold text-slate-800">Sources</h3>
                                        <p className="text-xs text-slate-500 mt-1">统一来源卡片，保留到原始材料的追溯入口。</p>
                                    </div>
                                    {knowledgeSources.length === 0 ? (
                                        <div className="p-6">
                                            <EmptyState icon={<Archive size={28} />} title="没有来源卡片" description="当前场景还没有返回可追溯来源。" />
                                        </div>
                                    ) : (
                                        <>
                                            <div className="max-h-56 overflow-y-auto p-3 space-y-2 border-b border-slate-200">
                                                {knowledgeSources.map((source) => {
                                                    const meta = getSourceKindMeta(source.kind);
                                                    const sourceKey = buildSourceKey(source);
                                                    const active = sourceKey === selectedSourceKey;
                                                    return (
                                                        <button
                                                            key={sourceKey}
                                                            onClick={() => setSelectedSourceKey(sourceKey)}
                                                            className={`w-full text-left rounded-2xl border p-3 transition-colors ${
                                                                active ? 'border-blue-200 bg-blue-50' : 'border-slate-200 bg-white hover:bg-slate-50'
                                                            }`}
                                                        >
                                                            <div className="flex items-center gap-2 mb-2">
                                                                <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.badge}`}>
                                                                    {meta.icon}
                                                                    {meta.label}
                                                                </span>
                                                            </div>
                                                            <div className="text-sm font-medium text-slate-800 truncate">{getSourceTitle(source)}</div>
                                                            <div className="text-xs text-slate-400 mt-1 line-clamp-2">{getSourcePreview(source)}</div>
                                                        </button>
                                                    );
                                                })}
                                            </div>

                                            <div className="p-4">
                                                {selectedSource ? (
                                                    <div className="space-y-4">
                                                        <div>
                                                            <div className="flex flex-wrap items-center gap-2 mb-3">
                                                                {(() => {
                                                                    const meta = getSourceKindMeta(selectedSource.kind);
                                                                    return (
                                                                        <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.badge}`}>
                                                                            {meta.icon}
                                                                            {meta.label}
                                                                        </span>
                                                                    );
                                                                })()}
                                                            </div>
                                                            <h3 className="text-lg font-semibold text-slate-900 break-words">{getSourceTitle(selectedSource)}</h3>
                                                            <div className="flex flex-wrap items-center gap-2 mt-3 text-[11px] text-slate-400">
                                                                <span>{selectedSource.id}</span>
                                                                {selectedSource.session_id && <span>session {selectedSource.session_id}</span>}
                                                                {typeof selectedSource.relevance === 'number' && <span>relevance {selectedSource.relevance.toFixed(2)}</span>}
                                                            </div>
                                                        </div>

                                                        {(selectedSource.file_path || selectedSource.line_range || selectedSource.source_type) && (
                                                            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 space-y-2 text-sm text-slate-600">
                                                                {selectedSource.source_type && <div><span className="text-slate-400">source_type</span> {selectedSource.source_type}</div>}
                                                                {selectedSource.file_path && <div><span className="text-slate-400">file</span> {selectedSource.file_path}</div>}
                                                                {selectedSource.line_range && <div><span className="text-slate-400">line</span> {selectedSource.line_range}</div>}
                                                                {selectedSource.node_label && <div><span className="text-slate-400">node</span> {selectedSource.node_label}</div>}
                                                            </div>
                                                        )}

                                                        {selectedSource.summary && (
                                                            <div>
                                                                <div className="text-xs font-medium uppercase tracking-wider text-slate-400 mb-2">Summary</div>
                                                                <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-6 text-slate-600 whitespace-pre-wrap break-words">{selectedSource.summary}</div>
                                                            </div>
                                                        )}

                                                        <div>
                                                            <div className="text-xs font-medium uppercase tracking-wider text-slate-400 mb-2">Content</div>
                                                            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-6 text-slate-600 whitespace-pre-wrap break-words min-h-24">
                                                                {selectedSource.content || selectedSource.summary || '这个来源暂时没有展开内容。'}
                                                            </div>
                                                        </div>
                                                    </div>
                                                ) : (
                                                    <EmptyState icon={<ExternalLink size={28} />} title="选择来源卡片" description="下方展示会保留 source type / file / line 等追溯信息。" />
                                                )}
                                            </div>
                                        </>
                                    )}
                                </div>

                                <div ref={graphJumpRef} className="rounded-2xl border border-slate-200 bg-white p-5 space-y-4">
                                    <div className="flex items-center justify-between gap-3 flex-wrap">
                                        <div>
                                            <h3 className="text-sm font-bold text-slate-800">Graph jumps</h3>
                                            <p className="text-xs text-slate-500">任务启动时可直接在 SAMG 节点之间跳转并查看 path。</p>
                                        </div>
                                        <span className="text-[10px] px-2 py-0.5 rounded-full bg-violet-50 text-violet-600 border border-violet-200">{graphPaths.length} paths</span>
                                    </div>

                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                                        <label className="text-xs text-slate-500 space-y-2">
                                            <span className="block uppercase tracking-widest">Source node</span>
                                            <select
                                                value={graphSourceNodeId ?? ''}
                                                onChange={(e) => setGraphSourceNodeId(e.target.value || null)}
                                                className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-700"
                                            >
                                                {knowledgeNodes.map((node) => (
                                                    <option key={node.id} value={node.id}>{node.label} · hop {node.hop}</option>
                                                ))}
                                            </select>
                                        </label>
                                        <label className="text-xs text-slate-500 space-y-2">
                                            <span className="block uppercase tracking-widest">Target node</span>
                                            <select
                                                value={selectedGraphNodeId ?? ''}
                                                onChange={(e) => setSelectedGraphNodeId(e.target.value || null)}
                                                className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-700"
                                            >
                                                {knowledgeNodes.map((node) => (
                                                    <option key={node.id} value={node.id}>{node.label} · hop {node.hop}</option>
                                                ))}
                                            </select>
                                        </label>
                                    </div>

                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                                        {[{ title: 'From', node: graphSourceNode }, { title: 'To', node: selectedGraphNode }].map(({ title, node }) => {
                                            const propertyEntries = formatNodeProperties(node?.properties);
                                            return (
                                                <div key={title} className="rounded-xl border border-slate-200 bg-slate-50 p-3 space-y-3">
                                                    <div>
                                                        <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">{title}</div>
                                                        <div className="font-semibold text-slate-800 break-words">{node?.label || 'No node selected'}</div>
                                                        {node && <div className="text-xs text-slate-400 mt-1 break-all">{node.id}</div>}
                                                    </div>
                                                    {node ? (
                                                        <>
                                                            <div className="text-xs text-slate-500">{buildNodeSummary(node)}</div>
                                                            <div className="flex flex-wrap gap-2 text-[11px]">
                                                                <span className="px-2 py-1 rounded-full border border-slate-200 bg-white text-slate-600">hop {node.hop}</span>
                                                                <span className="px-2 py-1 rounded-full border border-slate-200 bg-white text-slate-600">activation {node.activation.toFixed(2)}</span>
                                                                <span className="px-2 py-1 rounded-full border border-slate-200 bg-white text-slate-600">{formatNodeTypes(node['@type'])}</span>
                                                            </div>
                                                            {node.aliases && node.aliases.length > 0 && (
                                                                <div>
                                                                    <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Aliases</div>
                                                                    <div className="flex flex-wrap gap-2">
                                                                        {node.aliases.map((alias) => (
                                                                            <span key={alias} className="px-2 py-1 rounded-full border border-violet-200 bg-white text-[11px] text-violet-600">{alias}</span>
                                                                        ))}
                                                                    </div>
                                                                </div>
                                                            )}
                                                            {propertyEntries.length > 0 && (
                                                                <div>
                                                                    <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Properties</div>
                                                                    <div className="space-y-2">
                                                                        {propertyEntries.slice(0, 4).map(([key, value]) => (
                                                                            <div key={key} className="rounded-lg border border-white bg-white px-3 py-2 text-xs text-slate-600 break-words">
                                                                                <span className="text-slate-400">{key}</span> {typeof value === 'string' ? value : JSON.stringify(value)}
                                                                            </div>
                                                                        ))}
                                                                    </div>
                                                                </div>
                                                            )}
                                                            {node.pointers && node.pointers.length > 0 && (
                                                                <div>
                                                                    <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Pointers</div>
                                                                    <div className="space-y-2">
                                                                        {node.pointers.slice(0, 2).map((pointer, index) => (
                                                                            <button
                                                                                key={`${pointer.source_id}-${index}`}
                                                                                type="button"
                                                                                onClick={() => openSourceFromNode(node.id)}
                                                                                className="w-full rounded-lg border border-white bg-white px-3 py-2 text-left text-xs text-slate-600 hover:border-blue-200 hover:bg-blue-50"
                                                                            >
                                                                                <div className="font-medium text-slate-700">{pointer.summary || pointer.source_id}</div>
                                                                                <div className="text-slate-400 mt-1">{pointer.file_path || pointer.source_type || 'source'} {pointer.line_range ? `· ${pointer.line_range}` : ''}</div>
                                                                            </button>
                                                                        ))}
                                                                    </div>
                                                                </div>
                                                            )}
                                                        </>
                                                    ) : null}
                                                </div>
                                            );
                                        })}
                                    </div>

                                    <div className="rounded-xl border border-slate-200 bg-slate-50 p-3 space-y-2">
                                        <div className="text-[10px] uppercase tracking-widest text-slate-400">Paths</div>
                                        {graphSourceNodeId && selectedGraphNodeId && graphSourceNodeId === selectedGraphNodeId ? (
                                            <p className="text-sm text-slate-500">选择两个不同节点即可查看图谱跳转。</p>
                                        ) : graphPathLoading ? (
                                            <p className="text-sm text-slate-500">正在计算图谱路径...</p>
                                        ) : graphPaths.length === 0 ? (
                                            <p className="text-sm text-slate-500">当前节点组合没有返回 path。</p>
                                        ) : (
                                            <div className="space-y-2">
                                                {graphPaths.map((path, index) => (
                                                    <div key={`${path.join('>')}-${index}`} className="rounded-xl border border-white bg-white p-3 text-xs text-slate-600 break-words">
                                                        {path.join(' → ')}
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                </div>

                                <div ref={knowledgePackRef} className="rounded-2xl border border-slate-200 bg-white p-5 space-y-4">
                                    <div className="flex items-center justify-between gap-3 flex-wrap">
                                        <div>
                                            <h3 className="text-sm font-bold text-slate-800">Knowledge pack</h3>
                                            <p className="text-xs text-slate-500">跨项目复用通过现有 SAMG graph export/import 实现，不引入并行知识系统。</p>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <button
                                                onClick={handleExportPack}
                                                disabled={exportPackMutation.loading}
                                                className="px-3 py-1.5 rounded-lg border border-slate-200 bg-white text-xs font-bold text-slate-700 disabled:opacity-50"
                                            >
                                                {exportPackMutation.loading ? 'Exporting...' : 'Export'}
                                            </button>
                                            <button
                                                onClick={handleImportPack}
                                                disabled={importPackMutation.loading || !packDraft.trim()}
                                                className="px-3 py-1.5 rounded-lg bg-slate-900 text-white text-xs font-bold disabled:opacity-50"
                                            >
                                                {importPackMutation.loading ? 'Importing...' : 'Import'}
                                            </button>
                                        </div>
                                    </div>

                                    <div className="rounded-xl border border-slate-200 bg-slate-50 p-4">
                                        <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-3">Pack rules</div>
                                        <div className="space-y-2">
                                            {packRules.map((rule) => (
                                                <div key={rule} className="flex gap-2 text-sm text-slate-600">
                                                    <span className="mt-1 size-1.5 rounded-full bg-slate-300 shrink-0"></span>
                                                    <span>{rule}</span>
                                                </div>
                                            ))}
                                        </div>
                                    </div>

                                    {lastImportResult && (
                                        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
                                            <div className="rounded-xl border border-emerald-200 bg-emerald-50 p-3">
                                                <div className="text-[10px] uppercase tracking-widest text-emerald-600 mb-1">Imported</div>
                                                <div className="text-lg font-bold text-emerald-900">{lastImportResult.triple_count}</div>
                                            </div>
                                            <div className="rounded-xl border border-amber-200 bg-amber-50 p-3">
                                                <div className="text-[10px] uppercase tracking-widest text-amber-600 mb-1">Deduplicated</div>
                                                <div className="text-lg font-bold text-amber-900">{lastImportResult.deduplicated_count}</div>
                                            </div>
                                            <div className="rounded-xl border border-blue-200 bg-blue-50 p-3">
                                                <div className="text-[10px] uppercase tracking-widest text-blue-600 mb-1">Total triples</div>
                                                <div className="text-lg font-bold text-blue-900">{lastImportResult.total_triples}</div>
                                            </div>
                                        </div>
                                    )}

                                    {exportedPack && (
                                        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
                                            <div className="rounded-xl border border-slate-200 bg-slate-50 p-3">
                                                <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Exported triples</div>
                                                <div className="text-lg font-bold text-slate-900">{exportedPack.metadata?.triple_count ?? exportedPack['@graph']?.length ?? 0}</div>
                                            </div>
                                            <div className="rounded-xl border border-slate-200 bg-slate-50 p-3">
                                                <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Entities</div>
                                                <div className="text-lg font-bold text-slate-900">{exportedPack.metadata?.entity_count ?? 'n/a'}</div>
                                            </div>
                                            <div className="rounded-xl border border-slate-200 bg-slate-50 p-3">
                                                <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Version</div>
                                                <div className="text-sm font-semibold text-slate-700 break-all">{exportedPack.metadata?.version ?? 'unknown'}</div>
                                            </div>
                                        </div>
                                    )}

                                    <div className="space-y-2">
                                        <div className="flex items-center justify-between gap-2 flex-wrap">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400">Pack payload</div>
                                            <div className="text-[10px] text-slate-400">JSON-LD graph payload routed to backend import</div>
                                        </div>
                                        <textarea
                                            value={packDraft}
                                            onChange={(e) => setPackDraft(e.target.value)}
                                            placeholder="Export a pack or paste a SAMG JSON-LD graph here"
                                            className="min-h-48 w-full rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 font-mono text-xs text-slate-700 focus:border-blue-300 focus:outline-none focus:ring-4 focus:ring-blue-100"
                                        />
                                    </div>
                                </div>

                                <div className="rounded-2xl border border-slate-200 bg-white p-5 space-y-4">
                                    <div className="flex items-center justify-between gap-3 flex-wrap">
                                        <div>
                                            <h3 className="text-sm font-bold text-slate-800">Workflow details</h3>
                                            <p className="text-xs text-slate-500">Minimal approval, dependency, decision, and timeline surface attached to Plan / Task.</p>
                                        </div>
                                        <button onClick={handleOpenReplay} className="px-3 py-1.5 rounded-lg bg-slate-900 text-white text-xs font-bold">Replay</button>
                                    </div>
                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                                        <div className="rounded-xl bg-slate-50 border border-slate-100 p-3">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Approvals</div>
                                            <div className="space-y-2">
                                                {(mergedWorkflow.approval ?? []).map((item, index) => (
                                                    <div key={`${item.stage}-${index}`} className="flex items-start justify-between gap-2">
                                                        <div>
                                                            <p className="font-semibold text-slate-700">{item.stage}</p>
                                                            {item.owner && <p className="text-xs text-slate-400">{item.owner}</p>}
                                                        </div>
                                                        <span className="text-[10px] px-2 py-0.5 rounded-full bg-blue-50 text-blue-600 border border-blue-100 uppercase">{item.status}</span>
                                                    </div>
                                                ))}
                                            </div>
                                        </div>
                                        <div className="rounded-xl bg-slate-50 border border-slate-100 p-3">
                                            <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Decisions</div>
                                            <div className="space-y-2">
                                                {(mergedWorkflow.decisions ?? []).map((item) => (
                                                    <div key={item.id}>
                                                        <p className="font-semibold text-slate-700">{item.summary}</p>
                                                        {item.reason && <p className="text-xs text-slate-400 mt-1">{item.reason}</p>}
                                                    </div>
                                                ))}
                                            </div>
                                        </div>
                                    </div>
                                    <div className="rounded-xl bg-slate-50 border border-slate-100 p-3">
                                        <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Dependencies</div>
                                        {dependencyRows.length === 0 ? (
                                            <p className="text-xs text-slate-400">No explicit task dependencies</p>
                                        ) : (
                                            <div className="space-y-2">
                                                {dependencyRows.map((task) => (
                                                    <div key={task.id} className="text-sm text-slate-600">
                                                        <span className="font-semibold text-slate-700">{task.title}</span>
                                                        <span className="text-slate-400"> ← {task.dependencies?.join(', ')}</span>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                    <div className="rounded-xl bg-slate-50 border border-slate-100 p-3">
                                        <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Timeline</div>
                                        <div className="space-y-2">
                                            {timeline.slice(0, 4).map((item) => (
                                                <div key={item.id} className="flex gap-3 text-xs">
                                                    <span className="w-16 shrink-0 uppercase tracking-widest text-slate-400">{item.lane}</span>
                                                    <div>
                                                        <p className="font-semibold text-slate-700">{item.title}</p>
                                                        <p className="text-slate-500">{item.message}</p>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                </div>
                            </div>
                         </div>
                    </div>
                    )}
                </div>

                <aside className={`w-full md:w-96 bg-white/60 backdrop-blur-md border-l border-slate-200 flex flex-col z-10 shadow-[-5px_0_30px_rgba(0,0,0,0.02)] absolute inset-0 md:static transition-transform duration-300 ${mobileTab === 'status' ? 'translate-x-0 bg-white' : 'translate-x-full md:translate-x-0'}`}>
                    <div className="p-5 border-b border-slate-100 flex justify-between items-center bg-white/50">
                        <h3 className="font-bold text-slate-700 flex items-center gap-2 text-sm">
                            <Monitor size={16} className="text-blue-500" />
                            Tasks ({completedCount}/{taskList.length})
                        </h3>
                        <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full border ${
                            taskList.length > 0 && completedCount === taskList.length
                                ? 'text-green-600 bg-green-50 border-green-100'
                                : taskList.some(t => t.status === 'in_progress')
                                    ? 'text-blue-600 bg-blue-50 border-blue-100'
                                    : 'text-slate-400 bg-slate-50 border-slate-100'
                        }`}>{taskList.length > 0 && completedCount === taskList.length ? 'Complete' : taskList.some(t => t.status === 'in_progress') ? 'Active' : 'Idle'}</span>
                    </div>

                    <div className="flex-1 overflow-y-auto p-4 space-y-3">
                         {tasksLoading ? (
                             <LoadingSkeleton variant="list" count={3} />
                         ) : taskList.length === 0 ? (
                             <div className="text-center py-8 text-sm text-slate-400">No tasks yet</div>
                         ) : (
                             taskList.map(task => {
                                 const style = getTaskStatusStyle(task.status);
                                 return (
                                     <div
                                         key={task.id}
                                         onClick={() => handleTaskStatusToggle(task)}
                                         className={`p-4 ${style.bg} rounded-xl border ${style.border} shadow-sm cursor-pointer hover:shadow-md transition-all`}
                                     >
                                         <div className="flex justify-between items-center mb-2">
                                             <div className="flex items-center gap-2">
                                                 {style.icon}
                                                 <span className="text-sm font-bold text-slate-800">{task.title}</span>
                                             </div>
                                             <span className={`text-[10px] font-bold px-2 py-0.5 rounded ${style.badge}`}>{style.label}</span>
                                         </div>
                                         {task.description && (
                                             <p className="text-xs text-slate-500 pl-6 border-l-2 border-slate-100 ml-1.5 line-clamp-2">{task.description}</p>
                                         )}
                                         <div className="flex flex-wrap gap-2 mt-2 pl-6 ml-1.5 text-[10px] text-slate-400">
                                             {task.assignee && <span>{task.assignee}</span>}
                                             {task.dependencies && task.dependencies.length > 0 && <span>depends on {task.dependencies.join(', ')}</span>}
                                         </div>
                                     </div>
                                 );
                             })
                         )}
                    </div>

                    <div className="h-1/3 bg-slate-50 border-t border-slate-200 flex flex-col">
                        <div className="p-4 border-b border-slate-200 flex justify-between items-center">
                            <h3 className="font-bold text-slate-700 text-sm flex items-center gap-2">
                                <FileText size={16} className="text-blue-500" /> Progress
                            </h3>
                            <span className="text-[9px] font-bold text-slate-400 bg-white border border-slate-200 px-1.5 py-0.5 rounded">
                                {taskList.length > 0 ? `${Math.round((completedCount / taskList.length) * 100)}%` : '0%'}
                            </span>
                        </div>
                        <div className="flex-1 overflow-y-auto p-4 space-y-3">
                            {taskList.length > 0 && (
                                <div className="space-y-2">
                                    <div className="h-2 bg-slate-100 rounded-full overflow-hidden">
                                        <div
                                            className="h-full bg-gradient-to-r from-blue-500 to-indigo-500 rounded-full transition-all duration-500"
                                            style={{ width: `${taskList.length > 0 ? (completedCount / taskList.length) * 100 : 0}%` }}
                                        />
                                    </div>
                                    <p className="text-xs text-slate-400">{completedCount} of {taskList.length} tasks completed</p>
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="p-4 bg-white border-t border-slate-200">
                         <button onClick={handleOpenReplay} className="w-full py-3 bg-slate-900 hover:bg-slate-800 text-white font-bold rounded-xl shadow-lg transition-all flex items-center justify-center gap-2 active:scale-95">
                            <Zap size={18} /> Execute Plan
                         </button>
                    </div>
                </aside>
            </div>
        </div>
    );
};

// --- Main App Component ---

const App = () => {
  const [activeMode, setActiveMode] = useState<ViewMode>(ViewMode.HOME);
  const [isLogModalOpen, setIsLogModalOpen] = useState(false);
  const [replayData, setReplayData] = useState<WorkflowReplayData | null>(null);
  const [selectedProject, setSelectedProject] = useState<Project | null>(null);
  const [toasts, setToasts] = useState<ToastMsg[]>([]);
  const [profile, setProfile] = useState<SettingsProfile>(() => {
    try {
      const raw = window.localStorage.getItem('codeflow.profile');
      if (!raw) {
        return { name: 'Workspace User', email: '', role: 'Operator' };
      }
      const parsed = JSON.parse(raw) as Partial<SettingsProfile>;
      return {
        name: parsed.name?.trim() || 'Workspace User',
        email: parsed.email?.trim() || '',
        role: parsed.role?.trim() || 'Operator',
      };
    } catch {
      return { name: 'Workspace User', email: '', role: 'Operator' };
    }
  });

  useEffect(() => {
    try {
      window.localStorage.setItem('codeflow.profile', JSON.stringify(profile));
    } catch {
      // ignore localStorage failures
    }
  }, [profile]);

  const showToast = (message: string, type: 'success' | 'info' = 'success') => {
      const id = Date.now();
      setToasts(prev => [...prev, { id, message, type }]);
      setTimeout(() => {
          setToasts(prev => prev.filter(t => t.id !== id));
      }, 3000);
  };

  const handleProjectContext = (project: Project) => {
    setSelectedProject(project);
    setReplayData(null);
  };

  const workflowProjectId = selectedProject?.id;
  const workflowReplaySessionId = extractWorkflowMetadata(selectedProject?.metadata).replay_session_id;
  const { data: workflowOverview } = useApi<WorkflowOverview | null>(
    (signal) => workflowProjectId ? getWorkflowOverview(workflowProjectId, signal) : Promise.resolve(null),
    [workflowProjectId],
    { enabled: !!workflowProjectId },
  );
  const { data: workflowTimeline } = useApi<WorkflowTimelineResponse | null>(
    (signal) => workflowProjectId ? getWorkflowTimeline(workflowProjectId, signal) : Promise.resolve(null),
    [workflowProjectId],
    { enabled: !!workflowProjectId },
  );
  const { data: workflowReplay } = useApi<WorkflowReplayResponse | null>(
    (signal) => workflowProjectId ? getWorkflowReplay(workflowProjectId, { session_id: workflowReplaySessionId }, signal) : Promise.resolve(null),
    [workflowProjectId, workflowReplaySessionId],
    { enabled: !!workflowProjectId },
  );
  const workflowContext: AppWorkflowContext | null = workflowProjectId
    ? {
        project: selectedProject,
        overview: workflowOverview,
        timeline: workflowTimeline,
        replay: workflowReplay,
      }
    : null;

  const renderView = () => {
    switch (activeMode) {
      case ViewMode.HOME:
        return <HomeView onNavigate={setActiveMode} showToast={showToast} />;
      case ViewMode.PROJECTS:
        return <ProjectsView onNavigate={setActiveMode} showToast={showToast} onProjectContext={handleProjectContext} />;
      case ViewMode.SESSIONS:
        return (
          <SessionsView
            onOpenReplay={() => setIsLogModalOpen(true)}
            onReplayData={setReplayData}
          />
        );
      case ViewMode.MEMORY:
        return <MemoryView />;
      case ViewMode.AGENTS:
        return <AgentsView showToast={showToast} onNavigate={setActiveMode} />;
      case ViewMode.PLAN:
        return (
          <PlanView
            workflowContext={workflowContext}
            onOpenModal={() => setIsLogModalOpen(true)}
            showToast={showToast}
            onReplayData={setReplayData}
          />
        );
      case ViewMode.SETTINGS:
        return <SettingsView showToast={showToast} profile={profile} onProfileChange={setProfile} />;
      default:
        return <HomeView onNavigate={setActiveMode} showToast={showToast} />;
    }
  };

  return (
    <div className="flex h-screen w-full bg-slate-50 text-slate-900 font-sans selection:bg-blue-100 selection:text-blue-900 overflow-hidden">
      <Sidebar activeMode={activeMode} setMode={setActiveMode} userName={profile.name} />
      {renderView()}
      <MobileNav activeMode={activeMode} setMode={setActiveMode} />
      <LogModal
        isOpen={isLogModalOpen}
        onClose={() => setIsLogModalOpen(false)}
        replay={replayData}
      />
      <ToastContainer toasts={toasts} />
    </div>
  );
};

export default App;
