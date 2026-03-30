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
  Flame, Snowflake, Archive, ExternalLink, RefreshCw, Puzzle
} from 'lucide-react';
import { ViewMode, NavItem, AgentPreset, Project, ProjectListResponse, Plan, PlanListResponse, PlanTask, PlanTaskListResponse, Agent, GlobalConfig, ConversationTraceResponse, MemoryAgentContextResult, MemoryAgentRetrieveResult, MemoryAgentSource, MemoryTier, WorkflowMetadata, WorkflowReplayData, WorkflowOverview, WorkflowTimelineResponse, WorkflowReplayResponse, RawArchiveListResponse, AuditLogListResponse, QueryMemoryNode, SAMGPathResponse, SAMGGraph, SAMGGraphImportResult } from './types';
import { LogModal } from './components/LogModal';
import { useApi, useMutation } from './hooks/useApi';
import { EmptyState } from './components/EmptyState';
import { LoadingSkeleton } from './components/LoadingSkeleton';
import { ErrorState } from './components/ErrorState';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ConnectionStatus } from './components/ConnectionStatus';
import { PluginsView } from './components/PluginsView';
import { MemorySourceList } from './components/MemorySourceList';
import { MemorySourceDetail } from './components/MemorySourceDetail';
import { StatCard } from './components/StatCard';
import { SectionCard } from './components/SectionCard';
import { SubsectionCard } from './components/SubsectionCard';
import { TaskStatusCard } from './components/TaskStatusCard';
import { SidebarPanelHeader } from './components/SidebarPanelHeader';
import { SidebarPanel } from './components/SidebarPanel';
import { KnowledgeNodeCard } from './components/KnowledgeNodeCard';
import { KnowledgeNodeSelect } from './components/KnowledgeNodeSelect';
import { SimpleInfoPanel } from './components/SimpleInfoPanel';
import { LabeledTextItem } from './components/LabeledTextItem';
import { HeaderStatusBadge } from './components/HeaderStatusBadge';
import { ProgressSummaryCard } from './components/ProgressSummaryCard';
import { ActionButton } from './components/ActionButton';
import { KnowledgeRailCard } from './components/KnowledgeRailCard';
import { ScenarioToggle } from './components/ScenarioToggle';
import { HeaderStepIndicator } from './components/HeaderStepIndicator';
import { IconButton } from './components/IconButton';
import { MobileTabButton } from './components/MobileTabButton';
import { PlanStatusBadge } from './components/PlanStatusBadge';
import { SimpleEmptyHint } from './components/SimpleEmptyHint';
import { InlineAlert } from './components/InlineAlert';
import { EmbeddedWikiCard } from './components/EmbeddedWikiCard';
import { SessionSummaryField } from './components/SessionSummaryField';
import { SessionSummaryGrid } from './components/SessionSummaryGrid';
import { WorkflowTimelineItem } from './components/WorkflowTimelineItem';
import { SessionsComposer } from './components/SessionsComposer';
import { SessionTraceList } from './components/SessionTraceList';
import { SessionHeaderActions } from './components/SessionHeaderActions';

import { listPlans, getPlanTasks, createPlan, updatePlanTask } from './services/plans';
import { listAgents } from './services/agents';
import { getConversationTrace, stopConversation, retryConversation } from './services/conversations';
import { listRawArchive } from './services/raw_archive';
import { getWorkflowOverview, getWorkflowTimeline, getWorkflowReplay } from './services/workflows';
import { listAuditLogs } from './services/audit';
import { retrieveMemoryAgent, buildMemoryAgentContext } from './services/memory_agent';
import { getGlobalConfig, updateGlobalConfig } from './services/config';
import { healthCheck } from './services/health';
import { listHooks } from './services/hooks';
import { findPaths, exportGraph, importGraph } from './services/samg';
import {
  extractProjectWorkflowMetadata,
  toProjectViewModel,
} from './adapters/projects';
import {
  toAgentViewModel,
  toAgentViewModels,
} from './adapters/agents';
import {
  toSessionViewModels,
} from './adapters/sessions';
import {
  mergeQueryMemoryNodes,
  mergeRecordsById,
} from './adapters/memory';
import {
  buildPlanEmbeddedWikiEntries,
  buildPlanKnowledgeContextBlock,
} from './adapters/knowledge';
import {
  buildFallbackWorkflowMetadata,
  buildWorkflowReplayData,
  extractWorkflowMetadata,
  mergeWorkflowTimeline,
  sortReplayItems,
  workflowReplayEventToItem,
  workflowTimelineEventToItem,
} from './adapters/workflows';
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
    { id: ViewMode.PLUGINS, label: 'Plugins', icon: <Puzzle size={20} /> },
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
    { id: ViewMode.PLUGINS, label: 'Plugins', icon: <Puzzle size={20} /> },
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
        const workflow = extractProjectWorkflowMetadata(project.metadata);
        onProjectContext(project);
        showToast(`Opening ${workflow.blueprint || workflow.template || project.title}...`);
        setTimeout(() => onNavigate(ViewMode.PLAN), 600);
    };

    const projects = data?.projects ?? [];
    const projectCardEntries = projects.map((project) => ({
        rawProject: project,
        projectCard: toProjectViewModel(project),
    }));

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
                ) : projectCardEntries.length === 0 ? (
                    <EmptyState
                        icon={<Folder size={48} />}
                        title="No projects yet"
                        description="Create your first project to get started"
                        action={{ label: 'New Project', onClick: () => setShowCreateForm(true) }}
                    />
                ) : (
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 max-w-7xl mx-auto">
                        {projectCardEntries.map(({ rawProject, projectCard }) => {
                            return (
                                <div
                                    key={projectCard.id}
                                    onClick={() => handleProjectClick(rawProject)}
                                    className="bg-white rounded-2xl p-6 border border-slate-200 hover:border-blue-300 hover:shadow-xl hover:shadow-blue-500/5 transition-all duration-300 group cursor-pointer relative overflow-hidden"
                                >
                                    <div className="flex justify-between items-start mb-4">
                                        <div className={`px-2.5 py-1 rounded-full text-[10px] font-bold uppercase tracking-wide border ${projectCard.statusTone}`}>
                                            {projectCard.status}
                                        </div>
                                        <button className="p-1 hover:bg-slate-100 rounded-lg text-slate-400 hover:text-slate-600 transition-colors">
                                            <MoreVertical size={16} />
                                        </button>
                                    </div>

                                    <h3 className="text-lg font-bold text-slate-800 mb-2 group-hover:text-blue-600 transition-colors">{projectCard.title}</h3>
                                    <p className="text-sm text-slate-500 mb-4 line-clamp-2 h-10">{projectCard.descriptionText}</p>

                                    {projectCard.workflowSummaryText && (
                                        <div className="mb-4 rounded-xl border border-indigo-100 bg-indigo-50/60 p-3 space-y-2">
                                            <div className="flex items-center gap-2 text-[10px] uppercase tracking-widest font-bold text-indigo-600">
                                                <GitBranch size={12} /> Workflow seed
                                            </div>
                                            {projectCard.workflow.workflow_title && <p className="text-sm font-semibold text-slate-700">{projectCard.workflow.workflow_title}</p>}
                                            <div className="flex flex-wrap gap-2 text-[11px] text-slate-500">
                                                {projectCard.workflow.blueprint && <span className="px-2 py-1 rounded-full bg-white border border-indigo-100">Blueprint: {projectCard.workflow.blueprint}</span>}
                                                {projectCard.workflow.template && <span className="px-2 py-1 rounded-full bg-white border border-indigo-100">Template: {projectCard.workflow.template}</span>}
                                            </div>
                                        </div>
                                    )}

                                    <div className="mb-6">
                                        <div className="flex justify-between text-[11px] font-bold text-slate-400 mb-1.5">
                                            <span>{projectCard.progressLabel}</span>
                                            <span>{projectCard.progress}%</span>
                                        </div>
                                        <div className="h-1.5 w-full bg-slate-100 rounded-full overflow-hidden">
                                            <div
                                                className={`h-full rounded-full transition-all duration-1000 ${projectCard.progressTone}`}
                                                style={{ width: `${Math.max(projectCard.progress, projectCard.progress === 0 ? 0 : 2)}%` }}
                                            ></div>
                                        </div>
                                    </div>

                                    <div className="flex items-center justify-between pt-4 border-t border-slate-50">
                                        <div className="flex gap-2 flex-wrap">
                                            {projectCard.tags.map(tag => (
                                                <span key={tag} className="px-2 py-1 bg-slate-100 text-slate-500 rounded-md text-[10px] font-bold">{tag}</span>
                                            ))}
                                            {projectCard.hasReplayLink && (
                                                <span className="px-2 py-1 bg-amber-50 text-amber-600 rounded-md text-[10px] font-bold border border-amber-200">Replay linked</span>
                                            )}
                                            {projectCard.tags.length === 0 && !projectCard.hasReplayLink && (
                                                <span className="text-[10px] text-slate-300">No tags</span>
                                            )}
                                        </div>
                                        <div className="flex items-center gap-1.5 text-[11px] text-slate-400 font-medium">
                                            <Clock size={12} />
                                            {projectCard.lastActiveText}
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
  const { data: backendHealth } = useApi<{ status: string }>(
    (signal) => healthCheck(signal), [],
  );
  const { data: hookData } = useApi<{ hooks: { name: string; enabled: boolean; type: string }[] }>(
    (signal) => listHooks(signal), [],
  );

  const [model, setModel] = useState('');
  const [temp, setTemp] = useState(0.7);

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
      showToast("Settings saved successfully");
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
                            className="w-full h-2 bg-slate-100 rounded-lg appearance-none cursor-pointer accent-indigo-600"
                         />
                         <div className="flex justify-between text-[10px] text-slate-400 font-medium">
                            <span>Precise</span>
                            <span>Balanced</span>
                            <span>Creative</span>
                         </div>
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

  const handleSelectSession = (sessionId: string, agentId: string) => {
    setSelectedAgentId(agentId);
    setSelectedSessionId(sessionId);
    setMobileView('chat');
  };

  const selectedAgent = agents?.find(a => a.id === selectedAgentId);
  const trace = traceData?.trace;
  const agentList = agents ?? [];
  const sessionCards = toSessionViewModels(agentList);
  const selectedSessionCard = sessionCards.find((item) => item.sessionId === selectedSessionId) ?? sessionCards.find((item) => item.agentId === selectedAgentId) ?? null;
  const workflowTimeline = mergeWorkflowTimeline([], trace, rawArchiveData?.entries ?? [], (auditData?.entries ?? []).filter(entry => !selectedSessionId || entry.trace?.session_id === selectedSessionId || entry.actor.session_id === selectedSessionId));
  const replayData = buildWorkflowReplayData(selectedSessionCard?.replayTitle || selectedAgent?.name || 'Session workflow', {
    workflow_title: selectedSessionCard?.title || selectedAgent?.name,
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
         sessionCards.length === 0 ? <div className="text-center py-8 text-sm text-slate-400">No active sessions</div> :
         sessionCards.map((session) => (
          <div
            key={session.id}
            onClick={() => handleSelectSession(session.sessionId, session.agentId)}
            className={`p-3 border rounded-xl cursor-pointer transition-all ${selectedSessionId === session.sessionId ? 'bg-blue-50/50 border-blue-100' : 'bg-white border-slate-200 hover:border-blue-200'}`}
          >
            <div className="flex justify-between items-center mb-1 gap-2">
              <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${session.roleTone}`}>{session.roleText}</span>
              <span className={`text-[10px] px-1.5 py-0.5 rounded border ${session.statusTone}`}>{session.statusText}</span>
            </div>
            <h4 className="text-sm font-bold text-slate-800 mb-1">{session.title}</h4>
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[11px] text-slate-500">{session.subtitleText}</span>
              <span className="text-[10px] text-slate-400">{session.sessionText}</span>
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
              <h2 className="font-bold text-slate-800 text-sm md:text-base">{selectedSessionCard?.title || 'Select a session'}</h2>
              {selectedSessionCard && <span className="text-[11px] text-slate-400">{selectedSessionCard.subtitleText} • {selectedSessionCard.statusText}</span>}
            </div>
          </div>
          {selectedAgent && (
            <SessionHeaderActions
              onReplay={handleOpenReplay}
              onStop={() => selectedSessionId && stopConversation(selectedSessionId)}
              onRetry={() => selectedSessionId && retryConversation(selectedSessionId)}
            />
          )}
        </header>
        <main className="flex-1 overflow-y-auto p-4 md:p-8 space-y-6 bg-slate-50/30 pb-24 md:pb-8">
          {!selectedAgent ? (
            <EmptyState icon={<MessageSquare size={48} />} title="No session selected" description="Select a session from the sidebar to view conversation" />
          ) : !trace ? (
            <SimpleEmptyHint message="No conversation trace available" />
          ) : (
            <>
              <SessionSummaryGrid
                sessionText={selectedSessionCard?.sessionText}
                statusText={selectedSessionCard?.statusText}
                statusTone={selectedSessionCard?.statusTone}
                taskCountText={selectedSessionCard?.taskCountText}
                lastActiveText={selectedSessionCard?.lastActiveText}
              />
              <SectionCard
                title="Workflow timeline"
                subtitle="Trace, raw archive, and audit evidence are merged into one replay surface."
                action={<ActionButton onClick={handleOpenReplay}>Open replay</ActionButton>}
                contentClassName="p-4 space-y-3"
              >
                <div className="space-y-3">
                  {workflowTimeline.slice(0, 6).map((item) => (
                    <WorkflowTimelineItem key={item.id} item={item} />
                  ))}
                </div>
              </SectionCard>
              <SessionTraceList traces={trace.children} />
            </>
          )}
        </main>
        <div className="p-4 md:p-6 bg-white border-t border-slate-100 absolute bottom-0 md:static w-full">
          <SessionsComposer />
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

  const atomicMemories = mergeRecordsById(
    retrieveData?.atomic_memories ?? [],
    contextData?.atomic_memories ?? [],
  );
  const samgNodes = mergeQueryMemoryNodes(
    retrieveData?.samg_nodes ?? [],
    contextData?.samg_nodes ?? [],
  );
  const sources = mergeMemorySources(
    retrieveData?.sources ?? [],
    contextData?.sources ?? [],
  );

  useEffect(() => {
    if (sources.length === 0) {
      setSelectedSourceKey(null);
      return;
    }
    if (!selectedSourceKey || !sources.some((source) => buildMemorySourceKey(source) === selectedSourceKey)) {
      setSelectedSourceKey(buildMemorySourceKey(sources[0]));
    }
  }, [selectedSourceKey, sources]);

  const selectedSource = sources.find((source) => buildMemorySourceKey(source) === selectedSourceKey) ?? null;
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
            <StatCard label="Retrieve hits" value={totalFound} size="lg" />
            <StatCard label="Context sources" value={sourceCount} size="lg" />
            <StatCard label="Atomic memories" value={atomicMemories.length} size="lg" />
            <StatCard label="SAMG nodes" value={samgNodes.length} size="lg" />
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
              <SectionCard
                title="Context block"
                subtitle="来自 /api/v1/memory/agent/context 的统一上下文。"
                action={<span className="text-xs text-slate-400">{contextBlock ? `${contextBlock.length} chars` : 'empty'}</span>}
              >
                {contextBlock ? (
                  <pre className="whitespace-pre-wrap break-words text-xs leading-6 text-slate-600 bg-slate-50 rounded-2xl p-4 overflow-x-auto">{contextBlock}</pre>
                ) : (
                  <EmptyState icon={<FileText size={32} />} title="暂无上下文块" description="当前输入还没有组装出 memory_context。" />
                )}
              </SectionCard>

              <SectionCard
                title="Atomic memories"
                subtitle="统一入口返回的原子记忆结果。"
                action={<span className="text-xs text-slate-400">{atomicMemories.length} items</span>}
              >
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
              </SectionCard>

              <SectionCard
                title="SAMG nodes"
                subtitle="统一入口解析出的图谱节点与 pointer。"
                action={<span className="text-xs text-slate-400">{samgNodes.length} nodes</span>}
              >
                {samgNodes.length === 0 ? (
                  <EmptyState icon={<Activity size={32} />} title="没有 SAMG 命中" description="当前 query 没有返回图谱节点。" />
                ) : (
                  <div className="space-y-3">
                    {samgNodes.map((node) => (
                      <div key={node.id} className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                        <div className="flex flex-wrap items-center gap-2 mb-3">
                          <span className="text-sm font-semibold text-slate-800">{node.label}</span>
                          <InfoChip label={`hop ${node.hop}`} tone="violet" />
                          <InfoChip label={`activation ${node.activation.toFixed(2)}`} />
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
              </SectionCard>
            </div>

            <aside className="border-t xl:border-t-0 xl:border-l border-slate-200 bg-white/70 backdrop-blur-sm overflow-hidden flex flex-col">
              <div className="p-4 border-b border-slate-200 shrink-0">
                <h2 className="text-base font-semibold text-slate-900">Sources</h2>
                <p className="text-xs text-slate-400 mt-1">统一来源卡片。点选后可查看可追溯内容。</p>
              </div>

              {sources.length === 0 ? (
                <div className="flex-1 overflow-y-auto">
                  <MemorySourceList
                    sources={sources}
                    selectedSourceKey={selectedSourceKey}
                    onSelect={setSelectedSourceKey}
                    emptyState={{
                      title: '没有来源卡片',
                      description: '当前输入还没有返回可追溯来源。',
                      iconSize: 32,
                    }}
                  />
                </div>
              ) : (
                <>
                  <div className="max-h-[40%] overflow-y-auto p-3 border-b border-slate-200 shrink-0">
                    <MemorySourceList
                      sources={sources}
                      selectedSourceKey={selectedSourceKey}
                      onSelect={setSelectedSourceKey}
                    />
                  </div>

                  <div className="flex-1 overflow-y-auto p-4">
                    <MemorySourceDetail
                      source={selectedSource}
                      showTimestamp
                      contentMinHeightClass="min-h-32"
                      emptyState={{
                        title: '选择来源卡片',
                        description: '右侧会展示来源详情和可追溯内容。',
                        iconSize: 32,
                      }}
                    />
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

    const agentCards = toAgentViewModels(agents ?? []);

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
                 agentCards.length === 0 ? (
                    <EmptyState icon={<Users size={48} />} title="No agents" description="No agents are currently active" />
                 ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 md:gap-8">
                    {agentCards.map(agent => (
                        <div key={agent.id} className="bg-white/70 backdrop-blur-md border border-white p-6 md:p-8 rounded-[32px] shadow-xl shadow-slate-200/50 hover:-translate-y-2 transition-all duration-300 group relative overflow-hidden">
                            <div className="flex justify-between items-start mb-6">
                                <div className={`size-10 rounded-full flex items-center justify-center font-bold text-sm ${agent.avatarTone}`}>
                                    {agent.initial}
                                </div>
                                <span className={`px-3 py-1 text-[10px] font-bold uppercase tracking-widest rounded-full border ${agent.statusTone}`}>
                                    {agent.statusText}
                                </span>
                            </div>
                            <h3 className="text-xl font-bold text-slate-800 mb-2">{agent.name}</h3>
                            <p className="text-sm text-slate-500 mb-4">{agent.summaryText}</p>
                            <div className="flex flex-wrap gap-2 mb-4 text-[10px] font-bold uppercase tracking-wide">
                                <span className={`px-2.5 py-1 rounded-full ${agent.roleTone}`}>{agent.roleText}</span>
                                {agent.sessionText && <span className="px-2.5 py-1 rounded-full bg-slate-100 text-slate-600">{agent.sessionText}</span>}
                            </div>
                            <div className="flex gap-4 flex-wrap text-xs text-slate-400">
                                <span>{agent.taskCountText}</span>
                                <span>{agent.tokenCountText}</span>
                                {agent.errorCountText && <span className="text-red-400">{agent.errorCountText}</span>}
                                <span>Active {agent.lastActiveText}</span>
                            </div>
                        </div>
                    ))}
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

    const jumpToSection = (ref: React.RefObject<HTMLDivElement | null>) => {
        ref.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };
    const openSourceFromNode = (nodeId: string) => {
        const firstSource = knowledgeSources.find((source) => source.node_id === nodeId);
        if (!firstSource) return;
        setSelectedSourceKey(buildMemorySourceKey(firstSource));
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
    const handleEmbeddedWikiAction = (entry: { action?: { type: string; sourceIndex?: number; nodeId?: string } }) => {
        const action = entry.action;
        if (!action) return;
        switch (action.type) {
            case 'open_pack':
                jumpToSection(knowledgePackRef);
                return;
            case 'open_sources':
                jumpToSection(sourceCardsRef);
                return;
            case 'open_graph':
                jumpToSection(graphJumpRef);
                return;
            case 'select_source':
                if (typeof action.sourceIndex === 'number' && knowledgeSources[action.sourceIndex]) {
                    setSelectedSourceKey(buildMemorySourceKey(knowledgeSources[action.sourceIndex]));
                }
                jumpToSection(sourceCardsRef);
                return;
            case 'focus_graph_node':
                if (action.nodeId) {
                    focusGraphNode(action.nodeId);
                }
                return;
            default:
                return;
        }
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

    const knowledgeNodes = mergeQueryMemoryNodes(
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
    const knowledgeSources = mergeMemorySources(
        mergeMemorySources(memoryRetrieve?.sources ?? [], memoryContext?.sources ?? []),
        pointerSources,
    );
    const selectedSource = knowledgeSources.find((source) => buildMemorySourceKey(source) === selectedSourceKey) ?? null;
    const knowledgeContextBlock = memoryContext?.context_block?.trim()
        || buildPlanKnowledgeContextBlock({
            scenarioLabel: activeScenario.label,
            activeTaskTitle: activeTask?.title,
            selectedPlanTitle: selectedPlan?.title,
            dependencyCount: dependencyRows.length,
            latestEvidenceTitle: latestTimelineItem?.title,
        });
    const embeddedWikiEntries = buildPlanEmbeddedWikiEntries({
        contextBlock: knowledgeContextBlock,
        knowledgeSources,
        knowledgeNodes,
        hasSelectedSource: Boolean(selectedSource),
    });

    useEffect(() => {
        if (knowledgeSources.length === 0) {
            setSelectedSourceKey(null);
            return;
        }
        if (!selectedSourceKey || !knowledgeSources.some((source) => buildMemorySourceKey(source) === selectedSourceKey)) {
            setSelectedSourceKey(buildMemorySourceKey(knowledgeSources[0]));
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
                    <HeaderStepIndicator icon={<Lightbulb size={16} />} label="Intent" completed />
                    <div className="w-8 md:w-16 h-0.5 bg-blue-200 mx-2 mb-4"></div>
                    <HeaderStepIndicator icon={<Zap size={20} />} label="Plan" active />
                    <div className="w-8 md:w-16 h-0.5 bg-slate-200 mx-2 mb-4"></div>
                    <HeaderStepIndicator icon={<Code size={16} />} label="Exec" />
                </div>


                <div className="flex items-center gap-3">
                     <IconButton icon={<Bell size={18} />} />
                     <div className="hidden sm:block h-8 w-px bg-slate-200"></div>
                     <div className="size-9 rounded-full bg-slate-200 overflow-hidden hidden sm:block">
                         <div className="w-full h-full bg-gradient-to-br from-slate-400 to-slate-500"></div>
                     </div>
                </div>
            </header>

            <div className="md:hidden flex border-b border-slate-200 bg-white">
                <MobileTabButton label="Editor" active={mobileTab === 'editor'} onClick={() => setMobileTab('editor')} />
                <MobileTabButton label="Runtime & Status" active={mobileTab === 'status'} onClick={() => setMobileTab('status')} />
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
                            <PlanStatusBadge status={selectedPlan.status} />
                         </div>

                         <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                            <div className="bg-white border border-slate-200 rounded-2xl shadow-sm overflow-hidden min-h-[320px] flex flex-col group focus-within:ring-4 focus-within:ring-blue-500/10 transition-all">
                                 <div className="bg-slate-50 border-b border-slate-100 p-2 flex gap-1">
                                    <IconButton icon={<Edit3 size={16} />} tone="toolbar" size="sm" />
                                    <div className="w-px h-4 bg-slate-200 my-auto mx-1"></div>
                                    <IconButton icon={<Grid size={16} />} tone="toolbar" size="sm" />
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
                                        <ActionButton
                                            onClick={refetchKnowledgeRail}
                                            disabled={knowledgeLoading}
                                            tone="accent"
                                            size="sm"
                                        >
                                            {knowledgeLoading ? 'Loading...' : 'Refresh'}
                                        </ActionButton>
                                    </div>

                                    <div className="flex flex-wrap gap-2">
                                        {(Object.entries(scenarioMeta) as Array<[keyof typeof scenarioMeta, typeof activeScenario]>).map(([key, meta]) => {
                                            const active = key === knowledgeScenario;
                                            return (
                                                <ScenarioToggle
                                                    key={key}
                                                    label={meta.label}
                                                    active={active}
                                                    onClick={() => setKnowledgeScenario(key)}
                                                />
                                            );
                                        })}
                                    </div>

                                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
                                        <KnowledgeRailCard label="Sources" value={knowledgeSources.length} />
                                        <KnowledgeRailCard label="Graph nodes" value={knowledgeNodes.length} />
                                        <KnowledgeRailCard label="Blocked" value={blockedTasks.length} />
                                        <KnowledgeRailCard
                                            label="Replay session"
                                            value={knowledgeSessionId || 'Unavailable'}
                                            valueClassName="text-xs font-semibold text-slate-700 break-all"
                                        />
                                    </div>

                                    <div className="rounded-2xl border border-slate-200 bg-white p-4 space-y-3">
                                        <div className="flex items-center justify-between gap-2 flex-wrap">
                                            <div>
                                                <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Embedded wiki</div>
                                                <div className="text-sm font-semibold text-slate-800">Task-start context block</div>
                                            </div>
                                            <InfoChip label={activeTask?.title ?? selectedPlan.title} className="bg-slate-100 text-slate-500 border-slate-200" />
                                        </div>
                                        <div className="grid gap-3">
                                            {embeddedWikiEntries.map((entry) => (
                                                <EmbeddedWikiCard
                                                    key={entry.id}
                                                    entry={entry}
                                                    onAction={handleEmbeddedWikiAction}
                                                />
                                            ))}
                                        </div>
                                        {knowledgeError && (
                                            <InlineAlert message={knowledgeError.message} />
                                        )}
                                    </div>
                                </div>

                                <SectionCard
                                    title="Sources"
                                    subtitle="统一来源卡片，保留到原始材料的追溯入口。"
                                    className="overflow-hidden"
                                    contentClassName="p-0"
                                >
                                    {knowledgeSources.length === 0 ? (
                                        <div className="p-6">
                                            <MemorySourceList
                                                sources={knowledgeSources}
                                                selectedSourceKey={selectedSourceKey}
                                                onSelect={setSelectedSourceKey}
                                                emptyState={{
                                                    title: '没有来源卡片',
                                                    description: '当前场景还没有返回可追溯来源。',
                                                    iconSize: 28,
                                                }}
                                            />
                                        </div>
                                    ) : (
                                        <>
                                            <div className="max-h-56 overflow-y-auto p-3 border-b border-slate-200">
                                                <MemorySourceList
                                                    sources={knowledgeSources}
                                                    selectedSourceKey={selectedSourceKey}
                                                    onSelect={setSelectedSourceKey}
                                                />
                                            </div>

                                            <div className="p-4">
                                                <MemorySourceDetail
                                                    source={selectedSource}
                                                    emptyState={{
                                                        title: '选择来源卡片',
                                                        description: '下方展示会保留 source type / file / line 等追溯信息。',
                                                        iconSize: 28,
                                                    }}
                                                />
                                            </div>
                                        </>
                                    )}
                                </SectionCard>


                                <SectionCard
                                    title="Graph jumps"
                                    subtitle="任务启动时可直接在 SAMG 节点之间跳转并查看 path。"
                                    action={<InfoChip label={`${graphPaths.length} paths`} tone="violet" />}
                                    className="overflow-hidden"
                                    contentClassName="p-5 space-y-4"
                                >

                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                                        <KnowledgeNodeSelect
                                            label="Source node"
                                            value={graphSourceNodeId ?? ''}
                                            nodes={knowledgeNodes}
                                            onChange={setGraphSourceNodeId}
                                        />
                                        <KnowledgeNodeSelect
                                            label="Target node"
                                            value={selectedGraphNodeId ?? ''}
                                            nodes={knowledgeNodes}
                                            onChange={setSelectedGraphNodeId}
                                        />
                                    </div>

                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                                        {[{ title: 'From', node: graphSourceNode }, { title: 'To', node: selectedGraphNode }].map(({ title, node }) => (
                                            <KnowledgeNodeCard
                                                key={title}
                                                title={title}
                                                node={node ?? null}
                                                onOpenSource={openSourceFromNode}
                                            />
                                        ))}
                                    </div>

                                    <SimpleInfoPanel title="Paths" contentClassName="mt-2 space-y-2">
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
                                    </SimpleInfoPanel>
                                </SectionCard>

                                <SectionCard
                                    title="Knowledge pack"
                                    subtitle="跨项目复用通过现有 SAMG graph export/import 实现，不引入并行知识系统。"
                                    action={(
                                        <div className="flex items-center gap-2">
                                            <ActionButton
                                                onClick={handleExportPack}
                                                disabled={exportPackMutation.loading}
                                                tone="secondary"
                                                size="sm"
                                            >
                                                {exportPackMutation.loading ? 'Exporting...' : 'Export'}
                                            </ActionButton>
                                            <ActionButton
                                                onClick={handleImportPack}
                                                disabled={importPackMutation.loading || !packDraft.trim()}
                                                tone="primary"
                                                size="sm"
                                            >
                                                {importPackMutation.loading ? 'Importing...' : 'Import'}
                                            </ActionButton>
                                        </div>
                                    )}
                                    className="space-y-4"
                                    contentClassName="p-5 space-y-4"
                                >

                                    <SimpleInfoPanel title="Pack rules" padding="lg" contentClassName="mt-3 space-y-2">
                                        {packRules.map((rule) => (
                                            <div key={rule} className="flex gap-2 text-sm text-slate-600">
                                                <span className="mt-1 size-1.5 rounded-full bg-slate-300 shrink-0"></span>
                                                <span>{rule}</span>
                                            </div>
                                        ))}
                                    </SimpleInfoPanel>

                                    {lastImportResult && (
                                        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
                                            <StatCard label="Imported" value={lastImportResult.triple_count} tone="emerald" />
                                            <StatCard label="Deduplicated" value={lastImportResult.deduplicated_count} tone="amber" />
                                            <StatCard label="Total triples" value={lastImportResult.total_triples} tone="blue" />
                                        </div>
                                    )}

                                    {exportedPack && (
                                        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
                                            <StatCard label="Exported triples" value={exportedPack.metadata?.triple_count ?? exportedPack['@graph']?.length ?? 0} />
                                            <StatCard label="Entities" value={exportedPack.metadata?.entity_count ?? 'n/a'} />
                                            <StatCard label="Version" value={exportedPack.metadata?.version ?? 'unknown'} valueClassName="text-sm font-semibold text-slate-700 break-all" />
                                        </div>
                                    )}

                                    <SimpleInfoPanel
                                        title="Pack payload"
                                        description="JSON-LD graph payload routed to backend import"
                                        contentClassName="mt-2"
                                    >
                                        <textarea
                                            value={packDraft}
                                            onChange={(e) => setPackDraft(e.target.value)}
                                            placeholder="Export a pack or paste a SAMG JSON-LD graph here"
                                            className="min-h-48 w-full rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 font-mono text-xs text-slate-700 focus:border-blue-300 focus:outline-none focus:ring-4 focus:ring-blue-100"
                                        />
                                    </SimpleInfoPanel>
                                    </SectionCard>

                                <SectionCard
                                    title="Workflow details"
                                    subtitle="Minimal approval, dependency, decision, and timeline surface attached to Plan / Task."
                                    action={<ActionButton onClick={handleOpenReplay} tone="primary" size="sm">Replay</ActionButton>}
                                    className="space-y-4"
                                    contentClassName="p-5 space-y-4"
                                >
                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
                                        <SubsectionCard title="Approvals">
                                            <div className="space-y-2">
                                                {(mergedWorkflow.approval ?? []).map((item, index) => (
                                                    <div key={`${item.stage}-${index}`} className="flex items-start justify-between gap-2">
                                                        <LabeledTextItem title={item.stage} description={item.owner} />
                                                        <span className="text-[10px] px-2 py-0.5 rounded-full bg-blue-50 text-blue-600 border border-blue-100 uppercase">{item.status}</span>
                                                    </div>
                                                ))}
                                            </div>
                                        </SubsectionCard>
                                        <SubsectionCard title="Decisions">
                                            <div className="space-y-2">
                                                {(mergedWorkflow.decisions ?? []).map((item) => (
                                                    <LabeledTextItem key={item.id} title={item.summary} description={item.reason} />
                                                ))}
                                            </div>
                                        </SubsectionCard>
                                    </div>
                                    <SubsectionCard title="Dependencies">
                                        {dependencyRows.length === 0 ? (
                                            <p className="text-xs text-slate-400">No explicit task dependencies</p>
                                        ) : (
                                            <div className="space-y-2">
                                                {dependencyRows.map((task) => (
                                                    <LabeledTextItem
                                                        key={task.id}
                                                        title={task.title}
                                                        description={`← ${task.dependencies?.join(', ')}`}
                                                        className="text-sm"
                                                        descriptionClassName="text-sm text-slate-400"
                                                    />
                                                ))}
                                            </div>
                                        )}
                                    </SubsectionCard>
                                    <SubsectionCard title="Timeline">
                                        <div className="space-y-2">
                                            {timeline.slice(0, 4).map((item) => (
                                                <div key={item.id} className="flex gap-3 text-xs">
                                                    <span className="w-16 shrink-0 uppercase tracking-widest text-slate-400">{item.lane}</span>
                                                    <LabeledTextItem
                                                        title={item.title}
                                                        description={item.message}
                                                        className="text-xs"
                                                        descriptionClassName="text-slate-500"
                                                    />
                                                </div>
                                            ))}
                                        </div>
                                    </SubsectionCard>
                                </SectionCard>
                            </div>
                         </div>
                    </div>
                    )}
                </div>

                <aside className={`w-full md:w-96 bg-white/60 backdrop-blur-md border-l border-slate-200 flex flex-col z-10 shadow-[-5px_0_30px_rgba(0,0,0,0.02)] absolute inset-0 md:static transition-transform duration-300 ${mobileTab === 'status' ? 'translate-x-0 bg-white' : 'translate-x-full md:translate-x-0'}`}>
                    <div className="p-5 border-b border-slate-100 bg-white/50">
                        <SidebarPanelHeader
                            icon={<Monitor size={16} className="text-blue-500" />}
                            title={`Tasks (${completedCount}/${taskList.length})`}
                            status={(
                                <HeaderStatusBadge
                                    label={taskList.length > 0 && completedCount === taskList.length ? 'Complete' : taskList.some(t => t.status === 'in_progress') ? 'Active' : 'Idle'}
                                    className={taskList.length > 0 && completedCount === taskList.length
                                        ? 'text-green-600 bg-green-50 border-green-100'
                                        : taskList.some(t => t.status === 'in_progress')
                                            ? 'text-blue-600 bg-blue-50 border-blue-100'
                                            : 'text-slate-400 bg-slate-50 border-slate-100'}
                                />
                            )}
                        />
                    </div>

                    <div className="flex-1 overflow-y-auto p-4 space-y-3">
                         {tasksLoading ? (
                             <LoadingSkeleton variant="list" count={3} />
                         ) : taskList.length === 0 ? (
                             <SimpleEmptyHint message="No tasks yet" />
                         ) : (
                             taskList.map(task => {
                                 const style = getTaskStatusStyle(task.status);
                                 return (
                                     <TaskStatusCard
                                         key={task.id}
                                         onClick={() => handleTaskStatusToggle(task)}
                                         toneClassName={style.bg}
                                         borderClassName={style.border}
                                         icon={style.icon}
                                         title={task.title}
                                         badgeLabel={style.label}
                                         badgeClassName={style.badge}
                                         description={task.description}
                                         meta={(
                                             <>
                                                 {task.assignee && <span>{task.assignee}</span>}
                                                 {task.dependencies && task.dependencies.length > 0 && <span>depends on {task.dependencies.join(', ')}</span>}
                                             </>
                                         )}
                                     />
                                 );
                             })
                         )}
                    </div>

                    <SidebarPanel className="h-1/3 bg-slate-50 border-t border-slate-200">
                        <div className="p-4 border-b border-slate-200">
                            <SidebarPanelHeader
                                icon={<FileText size={16} className="text-blue-500" />}
                                title="Progress"
                                status={(
                                <HeaderStatusBadge
                                    label={taskList.length > 0 ? `${Math.round((completedCount / taskList.length) * 100)}%` : '0%'}
                                    size="xs"
                                    className="text-slate-400 bg-white border-slate-200"
                                />
                            )}
                            />
                        </div>
                        <div className="flex-1 overflow-y-auto p-4 space-y-3">
                            {taskList.length > 0 && (
                                <ProgressSummaryCard completedCount={completedCount} totalCount={taskList.length} />
                            )}
                        </div>
                    </SidebarPanel>

                    <SidebarPanel className="p-4 bg-white border-t border-slate-200">
                         <ActionButton onClick={handleOpenReplay} size="lg" className="w-full shadow-lg flex items-center justify-center gap-2 active:scale-95">
                            <Zap size={18} /> Execute Plan
                         </ActionButton>
                    </SidebarPanel>
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
      case ViewMode.PLUGINS:
        return <PluginsView showToast={showToast} onNavigate={setActiveMode} />;
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
