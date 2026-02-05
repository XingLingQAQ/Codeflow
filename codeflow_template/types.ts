import { ReactNode } from 'react';

export enum ViewMode {
  HOME = 'home',
  PROJECTS = 'projects',
  SESSIONS = 'sessions',
  MEMORY = 'memory',
  AGENTS = 'agents',
  PLAN = 'plan',
  SETTINGS = 'settings',
}

export interface NavItem {
  id: ViewMode;
  label: string;
  icon: ReactNode;
  active?: boolean;
}

export interface MemoryNode {
  id: string;
  x: number;
  y: number;
  label: string;
  type: 'service' | 'db' | 'auth' | 'config' | 'error' | 'api' | 'doc';
  color: string;
  icon: string;
}

export interface AgentPreset {
  title: string;
  description: string;
  count: number;
  tags: string[];
  avatars: string[];
  color: string;
}