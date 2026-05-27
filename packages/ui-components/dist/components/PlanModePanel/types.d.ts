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
export declare const PHASE_CONFIG: Record<string, {
    icon: string;
    color: string;
    label: string;
}>;
export declare const ARTIFACT_CONFIG: Record<string, {
    icon: string;
    color: string;
    label: string;
}>;
export declare const CONSTRAINT_TYPE_CONFIG: Record<string, {
    icon: string;
    color: string;
    label: string;
}>;
export declare const formatDuration: (ms: number) => string;
//# sourceMappingURL=types.d.ts.map