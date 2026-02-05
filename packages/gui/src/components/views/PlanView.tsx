/**
 * PlanView - 执行计划视图
 * 步骤进度指示器、目标编辑器、运行时状态侧边栏
 */

import React, { useState, useEffect } from 'react';
import {
  colors,
  spacing,
  borderRadius,
  fontSize,
  fontWeight,
  shadows,
  transitions,
  breakpoints,
} from '../shared/tokens';
import { Card, CardContent, CardHeader } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Button } from '../shared/Button';
import { Input } from '../shared/Input';
import { ProgressBar } from '../shared/ProgressBar';

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

// Icons
const TargetIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="10" />
    <circle cx="12" cy="12" r="6" />
    <circle cx="12" cy="12" r="2" />
  </svg>
);

const PlayIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polygon points="5 3 19 12 5 21 5 3" />
  </svg>
);

const StopIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="6" y="6" width="12" height="12" />
  </svg>
);

const CheckIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="20 6 9 17 4 12" />
  </svg>
);

const ClockIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="10" />
    <polyline points="12 6 12 12 16 14" />
  </svg>
);

const CpuIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="4" y="4" width="16" height="16" rx="2" ry="2" />
    <rect x="9" y="9" width="6" height="6" />
    <line x1="9" y1="1" x2="9" y2="4" />
    <line x1="15" y1="1" x2="15" y2="4" />
    <line x1="9" y1="20" x2="9" y2="23" />
    <line x1="15" y1="20" x2="15" y2="23" />
    <line x1="20" y1="9" x2="23" y2="9" />
    <line x1="20" y1="14" x2="23" y2="14" />
    <line x1="1" y1="9" x2="4" y2="9" />
    <line x1="1" y1="14" x2="4" y2="14" />
  </svg>
);

const DollarIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <line x1="12" y1="1" x2="12" y2="23" />
    <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
  </svg>
);

// Demo data
const demoSteps: PlanStep[] = [
  {
    id: '1',
    phase: 'intent',
    title: 'Intent Analysis',
    description: 'Analyze user intent and extract requirements',
    status: 'completed',
    progress: 100,
  },
  {
    id: '2',
    phase: 'plan',
    title: 'Plan Generation',
    description: 'Generate execution plan with steps and dependencies',
    status: 'active',
    progress: 65,
  },
  {
    id: '3',
    phase: 'exec',
    title: 'Execution',
    description: 'Execute the plan and generate outputs',
    status: 'pending',
    progress: 0,
  },
];

const demoRuntimeStatus: RuntimeStatus = {
  isRunning: true,
  currentStep: 'Generating plan structure...',
  elapsedTime: 45,
  tokensUsed: 2450,
  estimatedCost: 0.12,
};

// Phase colors
const phaseColors: Record<PlanPhaseType, { bg: string; border: string; text: string }> = {
  intent: { bg: colors.primary[100], border: colors.primary[400], text: colors.primary[700] },
  plan: { bg: colors.indigo[100], border: colors.indigo[400], text: colors.indigo[700] },
  exec: { bg: colors.success.light, border: colors.success.main, text: colors.success.dark },
};

// Status colors
const statusColors: Record<PlanPhaseStatus, { bg: string; text: string }> = {
  pending: { bg: colors.slate[100], text: colors.slate[500] },
  active: { bg: colors.primary[100], text: colors.primary[700] },
  completed: { bg: colors.success.light, text: colors.success.dark },
};

// Step Component
const StepCard: React.FC<{ step: PlanStep; isLast: boolean }> = ({ step, isLast }) => {
  const phaseStyle = phaseColors[step.phase];
  const statusStyle = statusColors[step.status];

  return (
    <div style={{ display: 'flex', gap: spacing[4] }}>
      {/* Timeline */}
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
        <div
          style={{
            width: 32,
            height: 32,
            borderRadius: borderRadius.full,
            backgroundColor: step.status === 'completed' ? colors.success.main : step.status === 'active' ? colors.primary[500] : colors.slate[200],
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: step.status === 'pending' ? colors.slate[400] : '#fff',
            flexShrink: 0,
          }}
        >
          {step.status === 'completed' ? (
            <CheckIcon />
          ) : (
            <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold }}>
              {step.phase === 'intent' ? '1' : step.phase === 'plan' ? '2' : '3'}
            </span>
          )}
        </div>
        {!isLast && (
          <div
            style={{
              width: 2,
              flex: 1,
              backgroundColor: step.status === 'completed' ? colors.success.main : colors.slate[200],
              minHeight: 40,
            }}
          />
        )}
      </div>

      {/* Content */}
      <Card style={{ flex: 1, marginBottom: isLast ? 0 : spacing[4] }}>
        <CardContent>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[2] }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
              <h4 style={{ fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
                {step.title}
              </h4>
              <Badge style={{ backgroundColor: phaseStyle.bg, color: phaseStyle.text }}>
                {step.phase.toUpperCase()}
              </Badge>
            </div>
            <Badge style={{ backgroundColor: statusStyle.bg, color: statusStyle.text }}>
              {step.status}
            </Badge>
          </div>
          <p style={{ fontSize: fontSize.sm, color: colors.slate[500], marginBottom: spacing[3] }}>
            {step.description}
          </p>
          {step.status !== 'pending' && (
            <ProgressBar
              value={step.progress || 0}
              status={step.status === 'completed' ? 'success' : 'default'}
            />
          )}
        </CardContent>
      </Card>
    </div>
  );
};

// Runtime Status Sidebar
const RuntimeSidebar: React.FC<{
  status: RuntimeStatus;
  onStop: () => void;
}> = ({ status, onStop }) => {
  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, '0')}`;
  };

  return (
    <Card style={{ height: 'fit-content' }}>
      <CardHeader style={{ borderBottom: `1px solid ${colors.slate[200]}` }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
            Runtime Status
          </h3>
          <div
            style={{
              width: 8,
              height: 8,
              borderRadius: borderRadius.full,
              backgroundColor: status.isRunning ? colors.success.main : colors.slate[300],
              animation: status.isRunning ? 'pulse 2s infinite' : 'none',
            }}
          />
        </div>
      </CardHeader>
      <CardContent>
        {/* Current Step */}
        <div style={{ marginBottom: spacing[4] }}>
          <span style={{ fontSize: fontSize.xs, color: colors.slate[400], textTransform: 'uppercase' }}>
            Current Step
          </span>
          <p style={{ fontSize: fontSize.sm, color: colors.slate[700], marginTop: spacing[1] }}>
            {status.currentStep}
          </p>
        </div>

        {/* Stats */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
            <div style={{ color: colors.slate[400] }}>
              <ClockIcon />
            </div>
            <span style={{ fontSize: fontSize.sm, color: colors.slate[600] }}>
              Elapsed: {formatTime(status.elapsedTime)}
            </span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
            <div style={{ color: colors.slate[400] }}>
              <CpuIcon />
            </div>
            <span style={{ fontSize: fontSize.sm, color: colors.slate[600] }}>
              Tokens: {status.tokensUsed.toLocaleString()}
            </span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
            <div style={{ color: colors.slate[400] }}>
              <DollarIcon />
            </div>
            <span style={{ fontSize: fontSize.sm, color: colors.slate[600] }}>
              Cost: ${status.estimatedCost.toFixed(2)}
            </span>
          </div>
        </div>

        {/* Stop Button */}
        {status.isRunning && (
          <Button
            variant="ghost"
            onClick={onStop}
            style={{
              marginTop: spacing[4],
              width: '100%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: spacing[2],
              color: colors.error.main,
            }}
          >
            <StopIcon />
            <span>Stop Execution</span>
          </Button>
        )}
      </CardContent>
    </Card>
  );
};

export const PlanView: React.FC<PlanViewProps> = ({
  steps = demoSteps,
  goal: initialGoal = 'Build a responsive dashboard with real-time data visualization',
  runtimeStatus = demoRuntimeStatus,
  onGoalChange,
  onStartPlan,
  onStopPlan,
  className,
  style,
}) => {
  const [goal, setGoal] = useState(initialGoal);
  const [isMobile, setIsMobile] = useState(false);

  useEffect(() => {
    const checkMobile = () => setIsMobile(window.innerWidth < breakpoints.lg);
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  const handleGoalChange = (value: string) => {
    setGoal(value);
    onGoalChange?.(value);
  };

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        backgroundColor: colors.slate[50],
        ...style,
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: `${spacing[4]}px ${spacing[6]}px`,
          backgroundColor: '#fff',
          borderBottom: `1px solid ${colors.slate[200]}`,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 40,
              height: 40,
              backgroundColor: colors.primary[50],
              borderRadius: borderRadius.lg,
              color: colors.primary[600],
            }}
          >
            <TargetIcon />
          </div>
          <div>
            <h1 style={{ fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
              Execution Plan
            </h1>
            <p style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
              Intent → Plan → Execute workflow
            </p>
          </div>
        </div>
      </div>

      {/* Content */}
      <div
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: isMobile ? 'column' : 'row',
          gap: spacing[6],
          padding: spacing[6],
          overflowY: 'auto',
        }}
      >
        {/* Main Content */}
        <div style={{ flex: 1, minWidth: 0 }}>
          {/* Goal Editor */}
          <Card style={{ marginBottom: spacing[6] }}>
            <CardHeader style={{ borderBottom: `1px solid ${colors.slate[200]}` }}>
              <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
                Goal
              </h3>
            </CardHeader>
            <CardContent>
              <textarea
                value={goal}
                onChange={(e) => handleGoalChange(e.target.value)}
                placeholder="Describe your goal..."
                style={{
                  width: '100%',
                  minHeight: 100,
                  padding: spacing[3],
                  border: `1px solid ${colors.slate[200]}`,
                  borderRadius: borderRadius.lg,
                  fontSize: fontSize.sm,
                  color: colors.slate[700],
                  resize: 'vertical',
                  outline: 'none',
                  transition: transitions.fast,
                  fontFamily: 'inherit',
                }}
                onFocus={(e) => {
                  e.target.style.borderColor = colors.primary[400];
                  e.target.style.boxShadow = `0 0 0 3px ${colors.primary[100]}`;
                }}
                onBlur={(e) => {
                  e.target.style.borderColor = colors.slate[200];
                  e.target.style.boxShadow = 'none';
                }}
              />
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: spacing[3] }}>
                <Button
                  variant="primary"
                  onClick={onStartPlan}
                  style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}
                >
                  <PlayIcon />
                  <span>Start Plan</span>
                </Button>
              </div>
            </CardContent>
          </Card>

          {/* Steps */}
          <div>
            <h3
              style={{
                fontSize: fontSize.base,
                fontWeight: fontWeight.semibold,
                color: colors.slate[800],
                marginBottom: spacing[4],
              }}
            >
              Execution Steps
            </h3>
            {steps.map((step, index) => (
              <StepCard key={step.id} step={step} isLast={index === steps.length - 1} />
            ))}
          </div>
        </div>

        {/* Sidebar */}
        <div style={{ width: isMobile ? '100%' : 280, flexShrink: 0 }}>
          <RuntimeSidebar status={runtimeStatus} onStop={onStopPlan || (() => {})} />
        </div>
      </div>
    </div>
  );
};

export default PlanView;
