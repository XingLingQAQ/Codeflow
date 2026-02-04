/**
 * PlanModePanel Types - Plan 模式面板类型定义
 */

export interface PlanPhase {
  id: string;
  name: string;
  status: 'pending' | 'active' | 'completed' | 'skipped';
  progress: number;
  startTime?: number;
  endTime?: number;
  model?: string;
  artifacts?: PlanArtifact[];
}

export interface PlanArtifact {
  id: string;
  type: 'proposal' | 'spec' | 'design' | 'task' | 'roadmap' | 'architecture';
  name: string;
  path: string;
  status: 'pending' | 'generating' | 'completed' | 'error';
  content?: string;
  createdAt: number;
  updatedAt: number;
}

export interface VisionData {
  goal: string;
  requirements: string[];
  constraints: string[];
  questions: VisionQuestion[];
}

export interface VisionQuestion {
  id: string;
  question: string;
  answer?: string;
  required: boolean;
}

export interface ConstraintItem {
  id: string;
  type: 'functional' | 'technical' | 'business' | 'security';
  description: string;
  priority: 'must' | 'should' | 'could';
  source: string;
}

export interface PlanModePanelProps {
  planId: string;
  planName: string;
  phases: PlanPhase[];
  vision?: VisionData;
  constraints?: ConstraintItem[];
  artifacts?: PlanArtifact[];
  currentPhase?: string;
  onPhaseSelect?: (phaseId: string) => void;
  onVisionUpdate?: (vision: VisionData) => void;
  onConstraintAdd?: (constraint: Omit<ConstraintItem, 'id'>) => void;
  onConstraintRemove?: (constraintId: string) => void;
  onArtifactView?: (artifactId: string) => void;
  onExecute?: () => void;
  onFastForward?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

// Phase 配置
export const PHASE_CONFIG: Record<string, { icon: string; color: string; label: string }> = {
  vision: { icon: '💡', color: '#f59e0b', label: 'Vision' },
  constraints: { icon: '🔒', color: '#8b5cf6', label: 'Constraints' },
  architecture: { icon: '🏗️', color: '#3b82f6', label: 'Architecture' },
  research: { icon: '🔍', color: '#06b6d4', label: 'Research' },
  explore: { icon: '🧭', color: '#10b981', label: 'Explore' },
  review: { icon: '👁️', color: '#6366f1', label: 'Review' },
  implement: { icon: '⚡', color: '#ec4899', label: 'Implement' },
  qa: { icon: '✅', color: '#22c55e', label: 'QA' },
};

// Artifact 配置
export const ARTIFACT_CONFIG: Record<string, { icon: string; color: string; label: string }> = {
  proposal: { icon: '📄', color: '#3b82f6', label: 'Proposal' },
  spec: { icon: '📋', color: '#8b5cf6', label: 'Specification' },
  design: { icon: '🎨', color: '#ec4899', label: 'Design' },
  task: { icon: '✅', color: '#22c55e', label: 'Tasks' },
  roadmap: { icon: '🗺️', color: '#f59e0b', label: 'Roadmap' },
  architecture: { icon: '🏛️', color: '#06b6d4', label: 'Architecture' },
};

// Constraint 类型配置
export const CONSTRAINT_TYPE_CONFIG: Record<string, { icon: string; color: string; label: string }> = {
  functional: { icon: '⚙️', color: '#3b82f6', label: 'Functional' },
  technical: { icon: '🔧', color: '#8b5cf6', label: 'Technical' },
  business: { icon: '💼', color: '#f59e0b', label: 'Business' },
  security: { icon: '🔐', color: '#ef4444', label: 'Security' },
};

// 格式化时间
export const formatDuration = (ms: number): string => {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
};
