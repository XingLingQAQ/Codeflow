/**
 * App - 主入口组件
 * 路由状态管理、全局 Context、视图切换
 */

import React, { createContext, useContext, useState, useCallback, useMemo } from 'react';
import {
  AppLayout,
  ViewMode,
  ToastProvider,
  useToast,
  HomeView,
  SessionsView,
  ProjectsView,
  MemoryView,
  AgentsView,
  PlanView,
  SettingsView,
  type UserProfile,
  type ConnectionStatus,
  type InterfacePreferences,
} from './components';

// Theme types
export type ThemeMode = 'light' | 'dark' | 'system';

// Navigation Context
interface NavigationContextValue {
  currentView: ViewMode;
  navigate: (view: ViewMode) => void;
  goBack: () => void;
  history: ViewMode[];
}

const NavigationContext = createContext<NavigationContextValue | null>(null);

export const useNavigation = () => {
  const context = useContext(NavigationContext);
  if (!context) {
    throw new Error('useNavigation must be used within NavigationProvider');
  }
  return context;
};

// Theme Context
interface ThemeContextValue {
  theme: ThemeMode;
  setTheme: (theme: ThemeMode) => void;
  isDark: boolean;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider');
  }
  return context;
};

// Navigation Provider
const NavigationProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [currentView, setCurrentView] = useState<ViewMode>(ViewMode.HOME);
  const [history, setHistory] = useState<ViewMode[]>([ViewMode.HOME]);

  const navigate = useCallback((view: ViewMode) => {
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

  return (
    <NavigationContext.Provider value={value}>
      {children}
    </NavigationContext.Provider>
  );
};

// Theme Provider
const ThemeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [theme, setTheme] = useState<ThemeMode>('light');

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

  return (
    <ThemeContext.Provider value={value}>
      {children}
    </ThemeContext.Provider>
  );
};

// View Renderer
const ViewRenderer: React.FC = () => {
  const { currentView } = useNavigation();
  const { addToast } = useToast();

  const handleAction = useCallback((action: string) => {
    addToast('info', `Action: ${action}`);
  }, [addToast]);

  switch (currentView) {
    case ViewMode.HOME:
      return (
        <HomeView
          onStartSession={(prompt: string, model: string) => handleAction(`Start: ${prompt} with ${model}`)}
        />
      );
    case ViewMode.SESSIONS:
      return (
        <SessionsView
          sessions={[]}
          onSelectSession={(id: string) => handleAction(`Select session: ${id}`)}
          onNewSession={() => handleAction('New session')}
        />
      );
    case ViewMode.PROJECTS:
      return (
        <ProjectsView
          projects={[]}
          onSelectProject={(id: string) => handleAction(`Select project: ${id}`)}
          onCreateProject={() => handleAction('Create project')}
        />
      );
    case ViewMode.MEMORY:
      return (
        <MemoryView
          nodes={[]}
          onSelectNode={(id: string) => handleAction(`Select node: ${id}`)}
        />
      );
    case ViewMode.AGENTS:
      return (
        <AgentsView
          presets={[]}
          onUsePreset={(id: string) => handleAction(`Use preset: ${id}`)}
          onCreatePreset={() => handleAction('Create preset')}
        />
      );
    case ViewMode.PLAN:
      return (
        <PlanView
          steps={[]}
          goal=""
          onGoalChange={(goal: string) => handleAction(`Goal: ${goal}`)}
          onStartPlan={() => handleAction('Start plan')}
          onStopPlan={() => handleAction('Stop plan')}
        />
      );
    case ViewMode.SETTINGS:
      return (
        <SettingsView
          profile={{ name: '', email: '', role: 'user' } as UserProfile}
          connections={[
            { name: 'API', status: 'disconnected' },
            { name: 'WebSocket', status: 'disconnected' },
          ] as ConnectionStatus[]}
          preferences={{ darkMode: false, compactMode: false, showTokenCount: true, autoSave: true } as InterfacePreferences}
          onProfileChange={(profile: UserProfile) => handleAction(`Profile: ${profile.name}`)}
          onPreferencesChange={(prefs: InterfacePreferences) => handleAction(`Preferences: ${prefs.darkMode}`)}
          onReconnect={(name: string) => handleAction(`Reconnect: ${name}`)}
        />
      );
    default:
      return <HomeView />;
  }
};

// Main App Content
const AppContent: React.FC = () => {
  const { currentView, navigate } = useNavigation();

  return (
    <AppLayout
      activeMode={currentView}
      onNavigate={navigate}
    >
      <ViewRenderer />
    </AppLayout>
  );
};

// App Component
export interface AppProps {
  initialView?: ViewMode;
  initialTheme?: ThemeMode;
}

export const App: React.FC<AppProps> = () => {
  return (
    <ThemeProvider>
      <ToastProvider>
        <NavigationProvider>
          <AppContent />
        </NavigationProvider>
      </ToastProvider>
    </ThemeProvider>
  );
};

export default App;
