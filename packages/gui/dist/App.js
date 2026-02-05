import { jsx as _jsx } from "react/jsx-runtime";
/**
 * App - 主入口组件
 * 路由状态管理、全局 Context、视图切换
 */
import { createContext, useContext, useState, useCallback, useMemo } from 'react';
import { AppLayout, ViewMode, ToastProvider, useToast, HomeView, SessionsView, ProjectsView, MemoryView, AgentsView, PlanView, SettingsView, } from './components';
const NavigationContext = createContext(null);
export const useNavigation = () => {
    const context = useContext(NavigationContext);
    if (!context) {
        throw new Error('useNavigation must be used within NavigationProvider');
    }
    return context;
};
const ThemeContext = createContext(null);
export const useTheme = () => {
    const context = useContext(ThemeContext);
    if (!context) {
        throw new Error('useTheme must be used within ThemeProvider');
    }
    return context;
};
// Navigation Provider
const NavigationProvider = ({ children }) => {
    const [currentView, setCurrentView] = useState(ViewMode.HOME);
    const [history, setHistory] = useState([ViewMode.HOME]);
    const navigate = useCallback((view) => {
        setCurrentView(view);
        setHistory(prev => [...prev, view]);
    }, []);
    const goBack = useCallback(() => {
        if (history.length > 1) {
            const newHistory = history.slice(0, -1);
            setHistory(newHistory);
            setCurrentView(newHistory[newHistory.length - 1]);
        }
    }, [history]);
    const value = useMemo(() => ({
        currentView,
        navigate,
        goBack,
        history,
    }), [currentView, navigate, goBack, history]);
    return (_jsx(NavigationContext.Provider, { value: value, children: children }));
};
// Theme Provider
const ThemeProvider = ({ children }) => {
    const [theme, setTheme] = useState('light');
    const isDark = useMemo(() => {
        if (theme === 'system') {
            return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
        }
        return theme === 'dark';
    }, [theme]);
    const value = useMemo(() => ({
        theme,
        setTheme,
        isDark,
    }), [theme, isDark]);
    return (_jsx(ThemeContext.Provider, { value: value, children: children }));
};
// View Renderer
const ViewRenderer = () => {
    const { currentView } = useNavigation();
    const { addToast } = useToast();
    const handleAction = useCallback((action) => {
        addToast('info', `Action: ${action}`);
    }, [addToast]);
    switch (currentView) {
        case ViewMode.HOME:
            return (_jsx(HomeView, { onStartSession: (prompt, model) => handleAction(`Start: ${prompt} with ${model}`) }));
        case ViewMode.SESSIONS:
            return (_jsx(SessionsView, { sessions: [], onSelectSession: (id) => handleAction(`Select session: ${id}`), onNewSession: () => handleAction('New session') }));
        case ViewMode.PROJECTS:
            return (_jsx(ProjectsView, { projects: [], onSelectProject: (id) => handleAction(`Select project: ${id}`), onCreateProject: () => handleAction('Create project') }));
        case ViewMode.MEMORY:
            return (_jsx(MemoryView, { nodes: [], onSelectNode: (id) => handleAction(`Select node: ${id}`) }));
        case ViewMode.AGENTS:
            return (_jsx(AgentsView, { presets: [], onUsePreset: (id) => handleAction(`Use preset: ${id}`), onCreatePreset: () => handleAction('Create preset') }));
        case ViewMode.PLAN:
            return (_jsx(PlanView, { steps: [], goal: "", onGoalChange: (goal) => handleAction(`Goal: ${goal}`), onStartPlan: () => handleAction('Start plan'), onStopPlan: () => handleAction('Stop plan') }));
        case ViewMode.SETTINGS:
            return (_jsx(SettingsView, { profile: { name: '', email: '', role: 'user' }, connections: [
                    { name: 'API', status: 'disconnected' },
                    { name: 'WebSocket', status: 'disconnected' },
                ], preferences: { darkMode: false, compactMode: false, showTokenCount: true, autoSave: true }, onProfileChange: (profile) => handleAction(`Profile: ${profile.name}`), onPreferencesChange: (prefs) => handleAction(`Preferences: ${prefs.darkMode}`), onReconnect: (name) => handleAction(`Reconnect: ${name}`) }));
        default:
            return _jsx(HomeView, {});
    }
};
// Main App Content
const AppContent = () => {
    const { currentView, navigate } = useNavigation();
    return (_jsx(AppLayout, { activeMode: currentView, onNavigate: navigate, children: _jsx(ViewRenderer, {}) }));
};
export const App = () => {
    return (_jsx(ThemeProvider, { children: _jsx(ToastProvider, { children: _jsx(NavigationProvider, { children: _jsx(AppContent, {}) }) }) }));
};
export default App;
//# sourceMappingURL=App.js.map