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
  LogOut, CreditCard, Key, Smartphone
} from 'lucide-react';
import { ViewMode, NavItem, MemoryNode, AgentPreset, Project, ProjectListResponse, Plan, PlanTask, Agent, CallTrace, SAMGEntity, SAMGTriple, MemoryItem, GlobalConfig } from './types';
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
import { getVisibleNodes, getTriples } from './services/samg';
import { getMemoryItems } from './services/memory';
import { getGlobalConfig, updateGlobalConfig } from './services/config';
import type { ProjectCreateInput } from './services/projects';

// --- Types & Constants ---
interface ToastMsg {
  id: number;
  message: string;
  type: 'success' | 'info';
}

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

const Sidebar = ({ activeMode, setMode }: { activeMode: ViewMode, setMode: (m: ViewMode) => void }) => {
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
            <span className="text-xs font-semibold text-slate-700">Alex Designer</span>
            <span className="text-[10px] text-slate-400">Pro Plan</span>
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

const ProjectsView = ({ onNavigate, showToast }: { onNavigate: (mode: ViewMode) => void, showToast: (msg: string) => void }) => {
    const [searchQuery, setSearchQuery] = useState('');
    const [debouncedSearch, setDebouncedSearch] = useState('');
    const [showCreateForm, setShowCreateForm] = useState(false);
    const [newTitle, setNewTitle] = useState('');
    const [newDesc, setNewDesc] = useState('');

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
            await createMutation.execute({ title: newTitle.trim(), description: newDesc.trim() || undefined });
            setNewTitle('');
            setNewDesc('');
            setShowCreateForm(false);
            showToast('Project created');
            refetch();
        } catch {
            showToast('Failed to create project');
        }
    };

    const handleProjectClick = (project: Project) => {
        showToast(`Opening ${project.title}...`);
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
            {/* Header */}
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

            {/* Create Form Modal */}
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
                            className="w-full px-4 py-2.5 border border-slate-200 rounded-xl text-sm mb-4 focus:border-blue-300 focus:ring-2 focus:ring-blue-500/10 outline-none resize-none h-20"
                        />
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

            {/* Content */}
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
                        {projects.map((project) => (
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
                                <p className="text-sm text-slate-500 mb-6 line-clamp-2 h-10">{project.description || 'No description provided'}</p>

                                {/* Progress Bar */}
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
                                    <div className="flex gap-2">
                                        {(project.tags ?? []).map(tag => (
                                            <span key={tag} className="px-2 py-1 bg-slate-100 text-slate-500 rounded-md text-[10px] font-bold">{tag}</span>
                                        ))}
                                        {(!project.tags || project.tags.length === 0) && (
                                            <span className="text-[10px] text-slate-300">No tags</span>
                                        )}
                                    </div>
                                    <div className="flex items-center gap-1.5 text-[11px] text-slate-400 font-medium">
                                        <Clock size={12} />
                                        {formatTime(project.last_active)}
                                    </div>
                                </div>
                            </div>
                        ))}

                        {/* Add New Placeholder */}
                        <button
                            onClick={() => setShowCreateForm(true)}
                            className="border-2 border-dashed border-slate-200 rounded-2xl p-6 flex flex-col items-center justify-center gap-4 hover:border-blue-300 hover:bg-blue-50/50 transition-all group min-h-[260px]"
                        >
                            <div className="size-14 rounded-full bg-slate-50 border border-slate-200 flex items-center justify-center text-slate-400 group-hover:scale-110 group-hover:border-blue-200 group-hover:text-blue-500 transition-all">
                                <Plus size={24} />
                            </div>
                            <div className="text-center">
                                <h3 className="font-bold text-slate-700 group-hover:text-blue-600 transition-colors">Create New Project</h3>
                                <p className="text-sm text-slate-400 mt-1">Start from scratch or import repo</p>
                            </div>
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
};

const SettingsView = ({ showToast }: { showToast: (msg: string) => void }) => {
  const [darkMode, setDarkMode] = useState(false);
  const [configError, setConfigError] = useState(false);

  const { data: config, loading, error, refetch } = useApi<GlobalConfig>(
    (signal) => getGlobalConfig(signal), [],
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

  const handleSave = async () => {
    try {
      await updateGlobalConfig({ default_model: model });
      showToast("Settings saved successfully");
    } catch {
      showToast("Failed to save settings");
    }
  };

  const modelOptions = config?.api_pool?.map(ch => ch.name) ?? ['Claude 3.5 Sonnet', 'GPT-4o', 'Gemini 1.5 Pro'];

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
                         <div className="size-24 rounded-full bg-gradient-to-tr from-indigo-500 to-purple-500 ring-4 ring-slate-50 shadow-inner flex items-center justify-center text-white text-2xl font-bold">AD</div>
                         <button className="text-xs font-bold text-blue-600 hover:underline">Change Avatar</button>
                    </div>
                    <div className="flex-1 w-full grid grid-cols-1 md:grid-cols-2 gap-6">
                        <div className="space-y-2">
                             <label className="text-xs font-bold text-slate-700 uppercase">Full Name</label>
                             <input type="text" defaultValue="Alex Designer" className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-400 transition-all" />
                        </div>
                        <div className="space-y-2">
                             <label className="text-xs font-bold text-slate-700 uppercase">Email Address</label>
                             <input type="email" defaultValue="alex@codeflow.ai" className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-400 transition-all" />
                        </div>
                        <div className="space-y-2">
                             <label className="text-xs font-bold text-slate-700 uppercase">Role</label>
                             <input type="text" defaultValue="Senior Architect" className="w-full px-4 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-400 transition-all" />
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
                     <div className="flex items-center justify-between p-4 border border-slate-100 rounded-xl bg-slate-50">
                        <div className="flex items-center gap-3">
                            <Github size={20} />
                            <span className="font-bold text-slate-700">GitHub</span>
                        </div>
                        <span className="text-xs font-bold text-green-600 bg-green-100 px-2 py-1 rounded">Connected</span>
                     </div>
                     <div className="flex items-center justify-between p-4 border border-slate-100 rounded-xl bg-white hover:border-blue-200 transition-colors cursor-pointer">
                        <div className="flex items-center gap-3">
                            <div className="size-5 rounded-full bg-black text-white flex items-center justify-center font-bold text-[10px]">V</div>
                            <span className="font-bold text-slate-700">Vercel</span>
                        </div>
                        <button className="text-xs font-bold text-slate-500 hover:text-blue-600">Connect</button>
                     </div>
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

const SessionsView = () => {
  const [mobileView, setMobileView] = useState<'list' | 'chat'>('list');
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

  const { data: agents, loading, error, refetch } = useApi<Agent[]>(
    (signal) => listAgents(signal), [],
  );

  const { data: trace } = useApi<CallTrace>(
    (signal) => selectedAgentId ? getConversationTrace(selectedAgentId, signal) : Promise.resolve(null as unknown as CallTrace),
    [selectedAgentId],
    { enabled: !!selectedAgentId },
  );

  const handleSelectAgent = (id: string) => {
    setSelectedAgentId(id);
    setMobileView('chat');
  };

  const selectedAgent = agents?.find(a => a.id === selectedAgentId);
  const agentList = agents ?? [];

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
            onClick={() => handleSelectAgent(agent.id)}
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
              <button onClick={() => selectedAgent && stopConversation(selectedAgent.session_id || selectedAgent.id)} className="px-3 py-1.5 text-xs font-medium text-red-500 hover:bg-red-50 rounded-lg">Stop</button>
              <button onClick={() => selectedAgent && retryConversation(selectedAgent.session_id || selectedAgent.id)} className="px-3 py-1.5 text-xs font-medium text-blue-500 hover:bg-blue-50 rounded-lg">Retry</button>
            </div>
          )}
        </header>
        <main className="flex-1 overflow-y-auto p-4 md:p-8 space-y-6 bg-slate-50/30 pb-24 md:pb-8">
          {!selectedAgent ? (
            <EmptyState icon={<MessageSquare size={48} />} title="No session selected" description="Select a session from the sidebar to view conversation" />
          ) : !trace ? (
            <div className="text-center py-8 text-sm text-slate-400">No conversation trace available</div>
          ) : (
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

const MemoryView = () => {
    const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

    const { data: entities, loading, error, refetch } = useApi<SAMGEntity[]>(
        (signal) => getVisibleNodes(signal), [],
    );

    const { data: triples } = useApi<SAMGTriple[]>(
        (signal) => getTriples(signal), [],
    );

    const { data: memoryItems } = useApi<MemoryItem[]>(
        (signal) => getMemoryItems(signal), [],
    );

    const nodeList = entities ?? [];
    const tripleList = triples ?? [];
    const selectedEntity = nodeList.find(e => e['@id'] === selectedNodeId);

    // Simple layout: distribute nodes in a circle
    const getNodePosition = (index: number, total: number) => {
        const angle = (2 * Math.PI * index) / Math.max(total, 1);
        const radius = 30;
        return { x: 50 + radius * Math.cos(angle), y: 50 + radius * Math.sin(angle) };
    };

    const typeColors: Record<string, string> = {
        service: 'bg-blue-100 border-blue-300 text-blue-600',
        entity: 'bg-emerald-100 border-emerald-300 text-emerald-600',
        concept: 'bg-violet-100 border-violet-300 text-violet-600',
        default: 'bg-slate-100 border-slate-300 text-slate-600',
    };

    return (
        <div className="flex-1 flex flex-col md:flex-row h-full relative overflow-hidden bg-slate-50 pb-16 md:pb-0">
             <div className="flex-1 relative h-[60vh] md:h-auto border-b md:border-b-0 md:border-r border-slate-200">
                 <div className="absolute inset-0" style={{ backgroundImage: 'radial-gradient(#cbd5e1 1px, transparent 1px)', backgroundSize: '40px 40px', opacity: 0.5 }}></div>

                 {loading ? <LoadingSkeleton variant="text" count={2} /> :
                  error ? <ErrorState message={error.message} onRetry={refetch} /> :
                  nodeList.length === 0 ? (
                    <div className="absolute inset-0 flex items-center justify-center">
                      <EmptyState icon={<Database size={48} />} title="No nodes yet" description="Semantic graph is empty" />
                    </div>
                  ) : (
                  <>
                    <svg className="absolute inset-0 pointer-events-none w-full h-full">
                      {tripleList.slice(0, 50).map((t, i) => {
                        const si = nodeList.findIndex(n => n['@id'] === t.subject['@id']);
                        const oi = nodeList.findIndex(n => n['@id'] === t.object.node?.['@id']);
                        if (si < 0 || oi < 0) return null;
                        const sp = getNodePosition(si, nodeList.length);
                        const op = getNodePosition(oi, nodeList.length);
                        return <line key={i} x1={`${sp.x}%`} y1={`${sp.y}%`} x2={`${op.x}%`} y2={`${op.y}%`} stroke="#cbd5e1" strokeWidth="2" />;
                      })}
                    </svg>
                    {nodeList.map((node, i) => {
                      const pos = getNodePosition(i, nodeList.length);
                      const color = typeColors[node['@type']?.[0] ?? 'default'] ?? typeColors.default;
                      return (
                        <div
                          key={node['@id']}
                          onClick={() => setSelectedNodeId(node['@id'])}
                          className="absolute flex flex-col items-center gap-2 cursor-pointer group hover:z-50 transition-all duration-300"
                          style={{ left: `${pos.x}%`, top: `${pos.y}%`, transform: 'translate(-50%, -50%)' }}
                        >
                          <div className={`size-12 md:size-16 rounded-full border-4 border-white shadow-lg flex items-center justify-center ${color} ${selectedNodeId === node['@id'] ? 'ring-4 ring-blue-500/20 scale-110' : ''} group-hover:scale-110 transition-all`}>
                            <div className="size-3 rounded-full bg-current opacity-50"></div>
                          </div>
                          <div className={`px-2 py-1 md:px-3 bg-white/90 backdrop-blur border border-slate-200 rounded-full text-[10px] md:text-xs font-bold text-slate-700 shadow-sm ${selectedNodeId === node['@id'] ? 'text-blue-600 border-blue-200' : ''}`}>
                            {node.label}
                          </div>
                        </div>
                      );
                    })}
                  </>
                  )}
             </div>

             <div className="h-[40vh] md:h-full w-full md:w-80 bg-white z-10 flex flex-col shadow-xl">
                 {selectedEntity ? (
                    <>
                        <div className="p-5 border-b border-slate-100 bg-slate-50/50">
                            <h3 className="font-bold text-slate-800">{selectedEntity.label}</h3>
                            <span className="text-xs text-slate-500">{selectedEntity['@type']?.join(', ') || 'Unknown type'}</span>
                            {selectedEntity.description && <p className="text-xs text-slate-400 mt-1">{selectedEntity.description}</p>}
                        </div>
                        <div className="p-5 flex-1 overflow-y-auto">
                            <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-4">Relations</h4>
                            <div className="space-y-3">
                                {tripleList.filter(t => t.subject['@id'] === selectedNodeId || t.object.node?.['@id'] === selectedNodeId).slice(0, 10).map((t, i) => (
                                    <div key={i} className="flex items-center gap-3 p-2 bg-white border border-slate-100 rounded-xl shadow-sm">
                                        <div className="size-8 rounded-lg bg-slate-50 text-slate-500 flex items-center justify-center"><Database size={16} /></div>
                                        <div className="flex-1">
                                            <span className="text-sm font-semibold text-slate-700">{t.predicate}</span>
                                            <p className="text-[10px] text-slate-400">{t.subject['@id'] === selectedNodeId ? t.object.node?.label ?? 'literal' : t.subject.label ?? t.subject['@id']}</p>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    </>
                 ) : (
                    <div className="flex-1 flex items-center justify-center text-sm text-slate-400">Select a node to view details</div>
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

const PlanView = ({ onOpenModal, showToast }: { onOpenModal: () => void, showToast: (m: string) => void }) => {
    const [mobileTab, setMobileTab] = useState<'editor' | 'status'>('editor');
    const [selectedPlanId, setSelectedPlanId] = useState<string | null>(null);

    // Fetch plans
    const { data: plans, loading: plansLoading, error: plansError, refetch: refetchPlans } = useApi<Plan[]>(
        (signal) => listPlans(signal),
        [],
    );

    // Fetch tasks for selected plan
    const { data: tasks, loading: tasksLoading, refetch: refetchTasks } = useApi<PlanTask[]>(
        (signal) => selectedPlanId ? getPlanTasks(selectedPlanId, signal) : Promise.resolve([]),
        [selectedPlanId],
        { enabled: !!selectedPlanId },
    );

    const selectedPlan = plans?.find(p => p.id === selectedPlanId) ?? plans?.[0] ?? null;

    // Auto-select first plan
    useEffect(() => {
        if (plans && plans.length > 0 && !selectedPlanId) {
            setSelectedPlanId(plans[0].id);
        }
    }, [plans, selectedPlanId]);

    const handleCreatePlan = async () => {
        try {
            const plan = await createPlan({ title: 'New Plan', description: '' });
            showToast('Plan created');
            refetchPlans();
            setSelectedPlanId(plan.id);
        } catch { showToast('Failed to create plan'); }
    };

    const handleTaskStatusToggle = async (task: PlanTask) => {
        if (!selectedPlanId) return;
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
            default: return { bg: 'bg-white', border: 'border-slate-200', badge: 'text-slate-400 bg-slate-50', icon: <Clock size={16} className="text-slate-400" />, label: 'PENDING' };
        }
    };

    const taskList = tasks ?? [];
    const completedCount = taskList.filter(t => t.status === 'completed').length;

    return (
        <div className="flex-1 flex flex-col h-full bg-slate-50 relative z-0 overflow-hidden pb-16 md:pb-0">
            <header className="h-16 flex items-center justify-between px-4 md:px-8 bg-white border-b border-slate-100 z-20 shrink-0">
                <div className="flex items-center gap-3">
                    <div className="size-9 bg-gradient-to-br from-blue-500 to-indigo-600 rounded-xl flex items-center justify-center text-white shadow-md shadow-blue-500/20">
                         <LayoutGrid size={18} />
                    </div>
                    <h1 className="font-bold text-slate-800 text-lg hidden sm:block">CodeFlow Plan</h1>
                    {plans && plans.length > 1 && (
                        <select
                            value={selectedPlanId ?? ''}
                            onChange={e => setSelectedPlanId(e.target.value)}
                            className="ml-2 text-sm border border-slate-200 rounded-lg px-2 py-1 bg-white"
                        >
                            {plans.map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
                        </select>
                    )}
                </div>

                {/* Stepper */}
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
            
            {/* Mobile Tab Switcher */}
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
                {/* Main Content (Editor) */}
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
                    <div className="w-full max-w-3xl space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
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

                         {/* Editor Card */}
                         <div className="bg-white border border-slate-200 rounded-2xl shadow-sm overflow-hidden min-h-[400px] flex flex-col group focus-within:ring-4 focus-within:ring-blue-500/10 transition-all">
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
                    </div>
                    )}
                </div>

                {/* Status Sidebar (Desktop: Sidebar, Mobile: Full View) */}
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
                                         {task.assignee && (
                                             <p className="text-[10px] text-slate-400 font-mono mt-2 pl-6 ml-1.5">{task.assignee}</p>
                                         )}
                                     </div>
                                 );
                             })
                         )}
                    </div>

                    {/* Progress Summary */}
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
                         <button onClick={() => { onOpenModal(); showToast("Execution started") }} className="w-full py-3 bg-slate-900 hover:bg-slate-800 text-white font-bold rounded-xl shadow-lg transition-all flex items-center justify-center gap-2 active:scale-95">
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
  const [toasts, setToasts] = useState<ToastMsg[]>([]);

  const showToast = (message: string, type: 'success' | 'info' = 'success') => {
      const id = Date.now();
      setToasts(prev => [...prev, { id, message, type }]);
      setTimeout(() => {
          setToasts(prev => prev.filter(t => t.id !== id));
      }, 3000);
  };

  // Helper to render the correct view
  const renderView = () => {
    switch (activeMode) {
      case ViewMode.HOME:
        return <HomeView onNavigate={setActiveMode} showToast={showToast} />;
      case ViewMode.PROJECTS:
        return <ProjectsView onNavigate={setActiveMode} showToast={showToast} />;
      case ViewMode.SESSIONS:
        return <SessionsView />;
      case ViewMode.MEMORY:
        return <MemoryView />;
      case ViewMode.AGENTS:
        return <AgentsView showToast={showToast} onNavigate={setActiveMode} />;
      case ViewMode.PLAN:
        return <PlanView onOpenModal={() => setIsLogModalOpen(true)} showToast={showToast} />;
      case ViewMode.SETTINGS:
        return <SettingsView showToast={showToast} />;
      default:
        return <HomeView onNavigate={setActiveMode} showToast={showToast} />;
    }
  };

  return (
    <div className="flex h-screen w-full bg-slate-50 text-slate-900 font-sans selection:bg-blue-100 selection:text-blue-900 overflow-hidden">
      <Sidebar activeMode={activeMode} setMode={setActiveMode} />
      {renderView()}
      <MobileNav activeMode={activeMode} setMode={setActiveMode} />
      <LogModal isOpen={isLogModalOpen} onClose={() => setIsLogModalOpen(false)} />
      <ToastContainer toasts={toasts} />
    </div>
  );
};

export default App;