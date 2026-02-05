/**
 * PlanView - 执行计划视图
 * 步骤进度指示器、目标编辑器、运行时状态侧边栏
 */
import React from 'react';
export type PlanPhaseType = 'intent' | 'plan' | 'exec';
export type PlanPhaseStatus = 'pending' | 'active' | 'completed';
export interface PlanStep {
    id: string;
    phase: PlanPhaseType;
    title: string;
    description: string;
    status: PlanPhaseStatus;
    progress?: number;
}
export interface RuntimeStatus {
    isRunning: boolean;
    currentStep: string;
    elapsedTime: number;
    tokensUsed: number;
    estimatedCost: number;
}
export interface PlanViewProps {
    steps?: PlanStep[];
    goal?: string;
    runtimeStatus?: RuntimeStatus;
    onGoalChange?: (goal: string) => void;
    onStartPlan?: () => void;
    onStopPlan?: () => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const PlanView: React.FC<PlanViewProps>;
export default PlanView;
//# sourceMappingURL=PlanView.d.ts.map