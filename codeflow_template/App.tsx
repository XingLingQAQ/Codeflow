import React, { useState, useEffect, useRef } from 'react';
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
import { ViewMode, NavItem, MemoryNode, AgentPreset } from './types';
import { LogModal } from './components/LogModal';

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
            <div className="absolute inset-0 bg-blue-100 rounded-full animate-ping opacity-20 duration-[3000ms]"></div>
            <div className="relative bg-white/80 backdrop-blur-sm px-4 py-1.5 rounded-full shadow-lg shadow-blue-500/5 border border-blue-100 flex items-center gap-2.5">
            <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
            </span>
            <span className="text-xs font-semibold text-slate-600 tracking-wide">SYSTEM ONLINE</span>
            </div>
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
    // Mock Project Data
    const projects = [
        { id: 1, title: 'Supabase Migration', desc: 'Migrating Auth and DB layer from Firebase to Supabase.', status: 'active', lastActive: '2m ago', tags: ['Backend', 'Postgres'], progress: 65 },
        { id: 2, title: 'Commerce Refactor', desc: 'Modernizing checkout flow with Stripe & optimized cart.', status: 'planning', lastActive: '1h ago', tags: ['Frontend', 'Stripe'], progress: 10 },
        { id: 3, title: 'Internal Dashboard', desc: 'Admin panel for analytics and user management.', status: 'paused', lastActive: '2d ago', tags: ['React', 'Tremor'], progress: 45 },
        { id: 4, title: 'Mobile App MVP', desc: 'React Native prototype for client demo.', status: 'completed', lastActive: '1w ago', tags: ['Mobile', 'RN'], progress: 100 },
        { id: 5, title: 'API Gateway', desc: 'Centralized API gateway with rate limiting.', status: 'active', lastActive: '3h ago', tags: ['DevOps', 'Go'], progress: 30 },
        { id: 6, title: 'Design System', desc: 'Unified UI component library.', status: 'planning', lastActive: '4d ago', tags: ['UI/UX', 'Figma'], progress: 5 },
    ];

    const handleProjectClick = (title: string) => {
        showToast(`Opening ${title}...`);
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
                            className="pl-9 pr-4 py-2 bg-slate-100 border-transparent focus:bg-white focus:border-blue-200 focus:ring-2 focus:ring-blue-500/10 rounded-xl text-sm w-64 transition-all"
                        />
                    </div>
                    <button className="flex items-center gap-2 bg-slate-900 hover:bg-slate-800 text-white px-4 py-2 rounded-xl text-sm font-bold shadow-lg shadow-slate-900/10 transition-transform active:scale-95">
                        <Plus size={18} />
                        <span className="hidden sm:inline">New Project</span>
                    </button>
                </div>
            </div>

            {/* Content Grid */}
            <div className="flex-1 overflow-y-auto p-6 md:p-10">
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 max-w-7xl mx-auto">
                    {projects.map((project) => (
                        <div 
                            key={project.id}
                            onClick={() => handleProjectClick(project.title)}
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
                            <p className="text-sm text-slate-500 mb-6 line-clamp-2 h-10">{project.desc}</p>
                            
                            {/* Progress Bar */}
                            <div className="mb-6">
                                <div className="flex justify-between text-[11px] font-bold text-slate-400 mb-1.5">
                                    <span>Progress</span>
                                    <span>{project.progress}%</span>
                                </div>
                                <div className="h-1.5 w-full bg-slate-100 rounded-full overflow-hidden">
                                    <div 
                                        className="h-full bg-gradient-to-r from-blue-500 to-indigo-500 rounded-full transition-all duration-1000" 
                                        style={{ width: `${project.progress}%` }}
                                    ></div>
                                </div>
                            </div>

                            <div className="flex items-center justify-between pt-4 border-t border-slate-50">
                                <div className="flex gap-2">
                                    {project.tags.map(tag => (
                                        <span key={tag} className="px-2 py-1 bg-slate-100 text-slate-500 rounded-md text-[10px] font-bold">{tag}</span>
                                    ))}
                                </div>
                                <div className="flex items-center gap-1.5 text-[11px] text-slate-400 font-medium">
                                    <Clock size={12} />
                                    {project.lastActive}
                                </div>
                            </div>
                        </div>
                    ))}
                    
                    {/* Add New Placeholer */}
                    <button className="border-2 border-dashed border-slate-200 rounded-2xl p-6 flex flex-col items-center justify-center gap-4 hover:border-blue-300 hover:bg-blue-50/50 transition-all group min-h-[260px]">
                        <div className="size-14 rounded-full bg-slate-50 border border-slate-200 flex items-center justify-center text-slate-400 group-hover:scale-110 group-hover:border-blue-200 group-hover:text-blue-500 transition-all">
                            <Plus size={24} />
                        </div>
                        <div className="text-center">
                            <h3 className="font-bold text-slate-700 group-hover:text-blue-600 transition-colors">Create New Project</h3>
                            <p className="text-sm text-slate-400 mt-1">Start from scratch or import repo</p>
                        </div>
                    </button>
                </div>
            </div>
        </div>
    );
};

const SettingsView = ({ showToast }: { showToast: (msg: string) => void }) => {
  const [model, setModel] = useState('Claude 3.5 Sonnet');
  const [temp, setTemp] = useState(0.7);
  const [darkMode, setDarkMode] = useState(false);

  const handleSave = () => {
      showToast("Settings saved successfully");
  };

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
                    <div className="space-y-3">
                         <label className="text-xs font-bold text-slate-700 uppercase">Default Model</label>
                         <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                            {['Claude 3.5 Sonnet', 'GPT-4o', 'Gemini 1.5 Pro'].map(m => (
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
  const [selectedSession, setSelectedSession] = useState<string | null>(null);

  const handleSelectSession = (id: string) => {
      setSelectedSession(id);
      setMobileView('chat');
  };

  const SidebarContent = () => (
      <>
        <div className="p-5 border-b border-slate-100 bg-slate-50/50 shrink-0">
            <div className="flex items-center justify-between mb-3">
                <h3 className="text-[10px] font-bold text-slate-400 uppercase tracking-widest">Active Projects</h3>
            </div>
            <div className="bg-white border border-slate-200 p-3 rounded-xl shadow-sm cursor-pointer hover:border-blue-300 transition-all group">
                <div className="flex items-center justify-between mb-1">
                    <div className="flex items-center gap-3">
                        <div className="size-8 rounded-lg bg-slate-800 text-white flex items-center justify-center text-xs font-bold">SM</div>
                        <span className="font-bold text-sm text-slate-800">Supabase Migration</span>
                    </div>
                    <div className="size-2 rounded-full bg-emerald-500 animate-pulse"></div>
                </div>
                <div className="text-[11px] text-slate-500 pl-11">3 active sessions • main branch</div>
            </div>
        </div>
        <div className="flex-1 overflow-y-auto p-3 space-y-2">
            <div 
                onClick={() => handleSelectSession('new')}
                className={`p-3 border rounded-xl cursor-pointer transition-all ${selectedSession === 'new' || selectedSession === null ? 'bg-blue-50/50 border-blue-100' : 'bg-white border-slate-200 hover:border-blue-200'}`}
            >
                <div className="flex justify-between items-center mb-1">
                    <span className="text-[10px] font-bold text-blue-600 bg-white px-1.5 py-0.5 rounded shadow-sm">REF-8821</span>
                    <span className="text-[10px] text-slate-400">Just now</span>
                </div>
                <h4 className="text-sm font-bold text-slate-800 mb-1">Refactor Auth</h4>
                <div className="flex items-center gap-2">
                        <div className="size-4 rounded-full bg-indigo-100 flex items-center justify-center">
                        <Zap size={10} className="text-indigo-600" />
                        </div>
                        <span className="text-[11px] text-slate-500">Generating service wrapper...</span>
                </div>
            </div>
            {['Database Schema', 'API Endpoints', 'Stripe Webhooks'].map((item, i) => (
                <div key={i} onClick={() => handleSelectSession(`prj-${i}`)} className="p-3 bg-white border border-slate-200 hover:border-blue-200 hover:shadow-sm rounded-xl cursor-pointer transition-all">
                    <div className="flex justify-between items-center mb-1">
                        <span className="text-[10px] font-bold text-slate-500 bg-slate-100 px-1.5 py-0.5 rounded">PRJ-{200+i}</span>
                        <span className="text-[10px] text-slate-400">2h ago</span>
                    </div>
                    <h4 className="text-sm font-bold text-slate-700 mb-1">{item}</h4>
                    <div className="flex items-center gap-2">
                            <span className="text-[11px] text-slate-400">Waiting for review</span>
                    </div>
                </div>
            ))}
        </div>
      </>
  );

  return (
    <div className="flex flex-1 h-full overflow-hidden relative pb-16 md:pb-0">
        {/* Desktop Sidebar */}
        <aside className="hidden md:flex w-80 bg-white border-r border-slate-200 flex-col shrink-0 z-10">
            <SidebarContent />
        </aside>

        {/* Mobile List View */}
        <div className={`md:hidden flex flex-col w-full h-full absolute inset-0 bg-white z-20 transition-transform duration-300 ${mobileView === 'list' ? 'translate-x-0' : '-translate-x-full'}`}>
             <SidebarContent />
        </div>

        {/* Chat Area (Desktop & Mobile) */}
        <div className={`flex-1 flex flex-col bg-white relative w-full h-full transition-transform duration-300 md:translate-x-0 ${mobileView === 'chat' ? 'translate-x-0 absolute inset-0 z-30' : 'translate-x-full md:translate-x-0'}`}>
            <header className="h-16 border-b border-slate-100 flex items-center justify-between px-4 md:px-6 bg-white/80 backdrop-blur z-20 shrink-0">
                <div className="flex items-center gap-3">
                    <button onClick={() => setMobileView('list')} className="md:hidden p-2 -ml-2 text-slate-500 hover:bg-slate-100 rounded-full">
                        <ChevronLeft size={20} />
                    </button>
                    <div>
                        <h2 className="font-bold text-slate-800 flex items-center gap-2 text-sm md:text-base">
                            Refactor Auth
                            <span className="px-2 py-0.5 bg-slate-100 text-slate-500 rounded-full text-[10px] font-bold hidden sm:inline-flex">#8821</span>
                        </h2>
                        <span className="text-[11px] text-slate-400 flex items-center gap-1">
                            <Code size={12} /> feature/supabase-migration
                        </span>
                    </div>
                </div>
                <div className="flex items-center gap-3">
                     <div className="flex items-center gap-2 px-3 py-1.5 bg-slate-50 border border-slate-200 rounded-full">
                        <div className="size-2 rounded-full bg-green-500"></div>
                        <span className="text-xs font-medium text-slate-600 hidden sm:inline">Claude 3.5 Sonnet</span>
                        <span className="text-xs font-medium text-slate-600 sm:hidden">Claude</span>
                     </div>
                </div>
            </header>
            <main className="flex-1 overflow-y-auto p-4 md:p-8 space-y-6 md:space-y-8 bg-slate-50/30 pb-24 md:pb-8">
                {/* User Message */}
                <div className="flex justify-end">
                    <div className="bg-blue-600 text-white rounded-2xl rounded-br-sm px-4 py-3 md:px-5 md:py-3.5 max-w-[90%] md:max-w-2xl shadow-md shadow-blue-500/10">
                        <p className="text-sm leading-relaxed">I want to migrate our current JWT implementation to use the Supabase SDK. Can you analyze the current auth controller?</p>
                    </div>
                </div>

                {/* AI Response */}
                <div className="flex justify-start">
                    <div className="bg-white border border-slate-200 rounded-2xl rounded-tl-sm px-1 py-1 max-w-[95%] md:max-w-2xl shadow-sm">
                        <div className="bg-slate-50/50 rounded-xl p-3 border-b border-slate-100 mb-2">
                            <div className="flex items-center gap-2 text-slate-500 mb-2">
                                 <Activity size={14} className="animate-pulse text-indigo-500" />
                                 <span className="text-xs font-medium">Scanning project files...</span>
                            </div>
                            <div className="space-y-1 pl-6">
                                <div className="flex items-center gap-2 text-[11px] text-slate-400">
                                    <Check size={12} className="text-emerald-500" /> Read src/controllers/auth.ts
                                </div>
                                <div className="flex items-center gap-2 text-[11px] text-slate-400">
                                    <Check size={12} className="text-emerald-500" /> Read src/middleware/jwt.ts
                                </div>
                            </div>
                        </div>
                        <div className="px-4 py-3 md:px-5">
                             <p className="text-sm text-slate-700 leading-relaxed mb-3">
                                 I've analyzed your current implementation. You are using a custom JWT signing mechanism. Here is a plan to migrate to Supabase:
                             </p>
                             <ol className="list-decimal list-inside text-sm text-slate-600 space-y-1 mb-4">
                                 <li>Install <code className="bg-slate-100 px-1 rounded text-xs">@supabase/supabase-js</code></li>
                                 <li>Initialize the Supabase client in a new service file.</li>
                                 <li>Replace login endpoint with <code className="bg-slate-100 px-1 rounded text-xs">signInWithPassword</code>.</li>
                             </ol>
                             <p className="text-sm text-slate-700">Shall I start by installing the SDK?</p>
                        </div>
                    </div>
                </div>
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
    // Mock Nodes
    const nodes: MemoryNode[] = [
        { id: '1', x: 50, y: 50, label: 'AuthService.ts', type: 'service', color: 'bg-blue-100 border-blue-300 text-blue-600', icon: 'hub' },
        { id: '2', x: 30, y: 30, label: 'UserDB', type: 'db', color: 'bg-emerald-100 border-emerald-300 text-emerald-600', icon: 'database' },
        { id: '3', x: 70, y: 35, label: 'JWT Strategy', type: 'auth', color: 'bg-violet-100 border-violet-300 text-violet-600', icon: 'shield' },
        { id: '4', x: 65, y: 70, label: '/config', type: 'config', color: 'bg-amber-100 border-amber-300 text-amber-600', icon: 'folder' },
    ];

    const [selectedNode, setSelectedNode] = useState<string>('1');

    const getNodeDetails = (id: string) => {
        const node = nodes.find(n => n.id === id);
        if (!node) return null;
        
        // Mock dynamic content based on selection
        return {
            name: node.label,
            type: node.type.toUpperCase(),
            desc: id === '1' ? '@core/services' : (id === '2' ? 'PostgreSQL Table' : 'Security Config'),
            relations: id === '1' ? [
                { name: 'User Schema', match: '98%', desc: 'Defines user table structure', icon: <Database size={16} />, color: 'emerald' },
                { name: 'JWT Strategy', match: '85%', desc: 'Passport.js configuration', icon: <Shield size={16} />, color: 'violet' }
            ] : [
                { name: 'AuthService', match: '98%', desc: 'Primary Consumer', icon: <Monitor size={16} />, color: 'blue' }
            ]
        };
    };

    const details = getNodeDetails(selectedNode);

    return (
        <div className="flex-1 flex flex-col md:flex-row h-full relative overflow-hidden bg-slate-50 pb-16 md:pb-0">
             {/* Graph Area */}
             <div className="flex-1 relative h-[60vh] md:h-auto border-b md:border-b-0 md:border-r border-slate-200">
                 <div className="absolute inset-0" style={{ backgroundImage: 'radial-gradient(#cbd5e1 1px, transparent 1px)', backgroundSize: '40px 40px', opacity: 0.5 }}></div>
                 
                 {/* Simulated Edges */}
                 <svg className="absolute inset-0 pointer-events-none w-full h-full">
                     <line x1="50%" y1="50%" x2="30%" y2="30%" stroke="#cbd5e1" strokeWidth="2" />
                     <line x1="50%" y1="50%" x2="70%" y2="35%" stroke="#cbd5e1" strokeWidth="2" />
                     <line x1="50%" y1="50%" x2="65%" y2="70%" stroke="#cbd5e1" strokeWidth="2" />
                 </svg>

                 {/* Nodes */}
                 {nodes.map(node => (
                     <div 
                        key={node.id}
                        onClick={() => setSelectedNode(node.id)}
                        className={`absolute flex flex-col items-center gap-2 cursor-pointer group hover:z-50 transition-all duration-300`}
                        style={{ left: `${node.x}%`, top: `${node.y}%`, transform: 'translate(-50%, -50%)' }}
                     >
                         <div className={`size-12 md:size-16 rounded-full border-4 border-white shadow-lg flex items-center justify-center ${node.color} ${selectedNode === node.id ? 'ring-4 ring-blue-500/20 scale-110' : ''} group-hover:scale-110 transition-all`}>
                            <div className="size-3 rounded-full bg-current opacity-50"></div>
                         </div>
                         <div className={`px-2 py-1 md:px-3 bg-white/90 backdrop-blur border border-slate-200 rounded-full text-[10px] md:text-xs font-bold text-slate-700 shadow-sm ${selectedNode === node.id ? 'text-blue-600 border-blue-200' : ''}`}>
                             {node.label}
                         </div>
                     </div>
                 ))}
             </div>

             {/* Details Panel */}
             <div className="h-[40vh] md:h-full w-full md:w-80 bg-white z-10 flex flex-col shadow-xl animate-in slide-in-from-bottom md:slide-in-from-right duration-300">
                 {details && (
                    <>
                        <div className="p-5 border-b border-slate-100 bg-slate-50/50">
                            <div className="flex items-start gap-3 mb-2">
                                <div className="size-10 rounded-lg bg-blue-100 text-blue-600 flex items-center justify-center">
                                    <Monitor size={20} />
                                </div>
                                <div>
                                    <h3 className="font-bold text-slate-800">{details.name}</h3>
                                    <span className="text-xs text-slate-500 font-mono">{details.desc}</span>
                                </div>
                            </div>
                            <div className="flex gap-2 mt-2">
                                <span className="text-[10px] font-bold bg-blue-50 text-blue-600 px-2 py-0.5 rounded border border-blue-100">TYPESCRIPT</span>
                                <span className="text-[10px] font-bold bg-slate-100 text-slate-500 px-2 py-0.5 rounded border border-slate-200">24KB</span>
                            </div>
                        </div>
                        <div className="p-5 flex-1 overflow-y-auto">
                            <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-4">Semantic Relations</h4>
                            <div className="space-y-3">
                                {details.relations.map((rel, i) => (
                                    <div key={i} className="flex items-center gap-3 p-2 bg-white border border-slate-100 rounded-xl hover:border-blue-200 transition-colors cursor-pointer shadow-sm">
                                        <div className={`size-8 rounded-lg bg-${rel.color}-50 text-${rel.color}-600 flex items-center justify-center`}>
                                            {rel.icon}
                                        </div>
                                        <div className="flex-1">
                                            <div className="flex justify-between">
                                                <span className="text-sm font-semibold text-slate-700">{rel.name}</span>
                                                {rel.match && <span className="text-[10px] text-emerald-600 bg-emerald-50 px-1 rounded font-bold">{rel.match}</span>}
                                            </div>
                                            <p className="text-[10px] text-slate-400 truncate">{rel.desc}</p>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    </>
                 )}
             </div>
        </div>
    );
};

const AgentsView = ({ showToast, onNavigate }: { showToast: (msg: string) => void, onNavigate: (m: ViewMode) => void }) => {
    const presets: AgentPreset[] = [
        { title: "Full-Stack Team", description: "Orchestrator lead with frontend/backend specialists.", count: 3, tags: ["Arch", "Dev"], avatars: ["bg-blue-200", "bg-indigo-200", "bg-purple-200"], color: "blue" },
        { title: "Security Audit", description: "Vulnerability scanner and senior security analyst.", count: 2, tags: ["Sec", "QA"], avatars: ["bg-emerald-200", "bg-teal-200"], color: "emerald" },
        { title: "Research Squad", description: "Synthesizes data into technical whitepapers.", count: 4, tags: ["Research", "Strategy"], avatars: ["bg-amber-200", "bg-orange-200", "bg-yellow-200", "bg-red-200"], color: "amber" },
        { title: "QA Lab", description: "Automates unit, integration, and E2E testing.", count: 3, tags: ["Test", "Auto"], avatars: ["bg-pink-200", "bg-rose-200", "bg-red-200"], color: "pink" },
    ];

    const handleUsePreset = (title: string) => {
        showToast(`Activated ${title} Swarm`);
        setTimeout(() => onNavigate(ViewMode.PLAN), 500);
    }

    return (
        <div className="flex-1 p-6 md:p-12 overflow-y-auto bg-slate-50 relative pb-24 md:pb-12">
            <div className="absolute inset-0 opacity-5" style={{ backgroundImage: 'radial-gradient(#000 1px, transparent 1px)', backgroundSize: '30px 30px' }}></div>
            <div className="max-w-6xl mx-auto relative z-10">
                <div className="text-center mb-10 md:mb-16">
                    <h1 className="text-3xl md:text-4xl font-bold text-slate-900 mb-4">Agent Swarm Presets</h1>
                    <p className="text-sm md:text-base text-slate-500 max-w-2xl mx-auto">Select a pre-configured architecture to jumpstart your workflow. Optimized hierarchies for specialized development teams.</p>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 md:gap-8">
                    {presets.map((preset, i) => (
                        <div key={i} className="bg-white/70 backdrop-blur-md border border-white p-6 md:p-8 rounded-[32px] shadow-xl shadow-slate-200/50 hover:-translate-y-2 transition-all duration-300 group relative overflow-hidden">
                            <div className="flex justify-between items-start mb-6">
                                <div className="flex -space-x-3">
                                    {preset.avatars.map((bg, idx) => (
                                        <div key={idx} className={`size-10 rounded-full border-2 border-white shadow-sm ${bg}`}></div>
                                    ))}
                                </div>
                                <span className={`px-3 py-1 bg-${preset.color}-50 text-${preset.color}-600 text-[10px] font-bold uppercase tracking-widest rounded-full`}>
                                    {preset.count} Agents
                                </span>
                            </div>
                            <h3 className="text-xl font-bold text-slate-800 mb-2">{preset.title}</h3>
                            <p className="text-sm text-slate-500 mb-8 h-10">{preset.description}</p>
                            <div className="flex gap-2">
                                {preset.tags.map(tag => (
                                    <span key={tag} className="px-2 py-1 bg-slate-100 text-slate-500 text-[10px] font-bold rounded-md">{tag}</span>
                                ))}
                            </div>
                            <div className="absolute inset-0 flex items-center justify-center bg-white/40 backdrop-blur-[2px] opacity-0 group-hover:opacity-100 transition-opacity duration-300">
                                <button 
                                    onClick={() => handleUsePreset(preset.title)}
                                    className="px-8 py-3 bg-indigo-600 text-white rounded-full font-bold shadow-xl shadow-indigo-200 hover:scale-105 transition-transform"
                                >
                                    Use Preset
                                </button>
                            </div>
                        </div>
                    ))}
                </div>
            </div>
        </div>
    );
}

const PlanView = ({ onOpenModal, showToast }: { onOpenModal: () => void, showToast: (m: string) => void }) => {
    const [mobileTab, setMobileTab] = useState<'editor' | 'status'>('editor');

    return (
        <div className="flex-1 flex flex-col h-full bg-slate-50 relative z-0 overflow-hidden pb-16 md:pb-0">
            <header className="h-16 flex items-center justify-between px-4 md:px-8 bg-white border-b border-slate-100 z-20 shrink-0">
                <div className="flex items-center gap-3">
                    <div className="size-9 bg-gradient-to-br from-blue-500 to-indigo-600 rounded-xl flex items-center justify-center text-white shadow-md shadow-blue-500/20">
                         <LayoutGrid size={18} />
                    </div>
                    <h1 className="font-bold text-slate-800 text-lg hidden sm:block">CodeFlow Plan</h1>
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
                    <div className="w-full max-w-3xl space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
                         <div className="flex items-start justify-between flex-wrap gap-2">
                            <div>
                                <h2 className="text-2xl md:text-3xl font-bold text-slate-900 tracking-tight mb-2">Project Vision: Commerce Refactor</h2>
                                <p className="text-sm md:text-base text-slate-500">Defining scope, objectives, and constraints for the migration agent.</p>
                            </div>
                            <span className="px-3 py-1 bg-green-50 text-green-700 border border-green-200 rounded-full text-xs font-bold flex items-center gap-2">
                                <span className="size-2 bg-green-500 rounded-full animate-pulse"></span>
                                Agents Active
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
                                defaultValue={`# Goal Statement
Migrate legacy checkout from custom PHP to Node.js Stripe integration.

## Core Requirements
- Support Apple Pay & Google Pay
- Preserve existing customer data structure
- Reduce checkout latency by 40%

## Technical Scope
1. Analyze current CheckoutController.php
2. Design new API schema in types/checkout.ts
3. Implement Stripe webhook handlers`}
                             />
                         </div>
                    </div>
                </div>

                {/* Status Sidebar (Desktop: Sidebar, Mobile: Full View) */}
                <aside className={`w-full md:w-96 bg-white/60 backdrop-blur-md border-l border-slate-200 flex flex-col z-10 shadow-[-5px_0_30px_rgba(0,0,0,0.02)] absolute inset-0 md:static transition-transform duration-300 ${mobileTab === 'status' ? 'translate-x-0 bg-white' : 'translate-x-full md:translate-x-0'}`}>
                    <div className="p-5 border-b border-slate-100 flex justify-between items-center bg-white/50">
                        <h3 className="font-bold text-slate-700 flex items-center gap-2 text-sm">
                            <Monitor size={16} className="text-blue-500" />
                            AI Runtime Status
                        </h3>
                        <span className="text-[10px] font-bold text-green-600 bg-green-50 px-2 py-0.5 rounded-full border border-green-100">Active</span>
                    </div>
                    
                    <div className="flex-1 overflow-y-auto p-4 space-y-3">
                         {/* Director Task */}
                         <div className="p-4 bg-white rounded-xl border border-slate-200 shadow-sm">
                            <div className="flex justify-between items-center mb-2">
                                <div className="flex items-center gap-2">
                                    <Check size={16} className="text-green-500" />
                                    <span className="text-sm font-bold text-slate-800">Director</span>
                                </div>
                                <span className="text-[10px] font-bold text-green-600 bg-green-50 px-2 py-0.5 rounded">DONE</span>
                            </div>
                            <p className="text-xs text-slate-500 pl-6 border-l-2 border-green-100 ml-1.5">Task decomposition complete. Assigned migration to Coder.</p>
                         </div>

                         {/* Coder Task (Active) */}
                         <div className="p-4 bg-blue-50/50 rounded-xl border border-blue-100 shadow-sm relative overflow-hidden group cursor-pointer hover:shadow-md transition-all" onClick={onOpenModal}>
                             <div className="absolute left-0 top-0 bottom-0 w-1 bg-blue-500"></div>
                             <div className="flex justify-between items-center mb-2">
                                <div className="flex items-center gap-2">
                                    <div className="relative flex items-center justify-center size-4">
                                        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75"></span>
                                        <Code size={16} className="text-blue-600 relative z-10" />
                                    </div>
                                    <span className="text-sm font-bold text-slate-800">Coder</span>
                                </div>
                                <div className="flex items-center gap-2">
                                    <span className="text-[10px] font-bold text-blue-600 bg-blue-100 px-2 py-0.5 rounded animate-pulse">RUNNING</span>
                                    <MoreHorizontal size={14} className="text-blue-400" />
                                </div>
                             </div>
                             <div className="pl-6 ml-1.5">
                                 <p className="text-xs font-medium text-slate-700 mb-2">Drafting API definitions...</p>
                                 <div className="w-full h-1.5 bg-blue-100 rounded-full overflow-hidden">
                                     <div className="h-full bg-blue-500 w-[60%] animate-[progress_2s_ease-in-out_infinite]"></div>
                                 </div>
                                 <p className="text-[10px] text-slate-400 font-mono mt-2">Create: types/checkout.ts</p>
                             </div>
                         </div>
                    </div>

                    {/* Artifacts */}
                    <div className="h-1/3 bg-slate-50 border-t border-slate-200 flex flex-col">
                        <div className="p-4 border-b border-slate-200 flex justify-between items-center">
                            <h3 className="font-bold text-slate-700 text-sm flex items-center gap-2">
                                <FileText size={16} className="text-blue-500" /> Generated Artifacts
                            </h3>
                            <span className="text-[9px] font-bold text-slate-400 bg-white border border-slate-200 px-1.5 py-0.5 rounded">FILTER: *.MD</span>
                        </div>
                        <div className="flex-1 overflow-y-auto p-4 space-y-3">
                            <div className="bg-white p-3 rounded-xl border border-slate-200 shadow-sm flex items-center justify-between group cursor-pointer hover:border-blue-300 transition-colors">
                                <div className="flex items-center gap-3">
                                    <div className="size-8 rounded-lg bg-slate-100 flex items-center justify-center text-slate-500 group-hover:bg-blue-50 group-hover:text-blue-600 transition-colors">
                                        <FileText size={16} />
                                    </div>
                                    <div>
                                        <h4 className="text-xs font-bold text-slate-800">technical_spec.md</h4>
                                        <p className="text-[10px] text-slate-400">Technical Specification</p>
                                    </div>
                                </div>
                                <Check size={14} className="text-green-500" />
                            </div>
                            <div className="bg-white p-3 rounded-xl border border-blue-200 shadow-sm flex items-center justify-between relative overflow-hidden">
                                 <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-blue-500 animate-[loading_1.5s_ease-in-out_infinite]"></div>
                                <div className="flex items-center gap-3">
                                    <div className="size-8 rounded-lg bg-blue-50 flex items-center justify-center text-blue-600">
                                        <Edit3 size={16} className="animate-pulse" />
                                    </div>
                                    <div>
                                        <h4 className="text-xs font-bold text-slate-800">api_docs.md</h4>
                                        <p className="text-[10px] text-slate-400">API Documentation</p>
                                    </div>
                                </div>
                                <span className="text-[9px] font-bold text-blue-600 bg-blue-50 px-1.5 py-0.5 rounded">GEN</span>
                            </div>
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