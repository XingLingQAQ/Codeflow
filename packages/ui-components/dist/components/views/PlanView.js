import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * PlanView - 执行计划视图
 * 步骤进度指示器、目标编辑器、运行时状态侧边栏
 */
import { useState, useEffect } from 'react';
import { colors, spacing, borderRadius, fontSize, fontWeight, transitions, breakpoints, } from '../shared/tokens';
import { Card, CardContent, CardHeader } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Button } from '../shared/Button';
import { ProgressBar } from '../shared/ProgressBar';
// Icons
const TargetIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "10" }), _jsx("circle", { cx: "12", cy: "12", r: "6" }), _jsx("circle", { cx: "12", cy: "12", r: "2" })] }));
const PlayIcon = () => (_jsx("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("polygon", { points: "5 3 19 12 5 21 5 3" }) }));
const StopIcon = () => (_jsx("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("rect", { x: "6", y: "6", width: "12", height: "12" }) }));
const CheckIcon = () => (_jsx("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("polyline", { points: "20 6 9 17 4 12" }) }));
const ClockIcon = () => (_jsxs("svg", { width: "14", height: "14", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "10" }), _jsx("polyline", { points: "12 6 12 12 16 14" })] }));
const CpuIcon = () => (_jsxs("svg", { width: "14", height: "14", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("rect", { x: "4", y: "4", width: "16", height: "16", rx: "2", ry: "2" }), _jsx("rect", { x: "9", y: "9", width: "6", height: "6" }), _jsx("line", { x1: "9", y1: "1", x2: "9", y2: "4" }), _jsx("line", { x1: "15", y1: "1", x2: "15", y2: "4" }), _jsx("line", { x1: "9", y1: "20", x2: "9", y2: "23" }), _jsx("line", { x1: "15", y1: "20", x2: "15", y2: "23" }), _jsx("line", { x1: "20", y1: "9", x2: "23", y2: "9" }), _jsx("line", { x1: "20", y1: "14", x2: "23", y2: "14" }), _jsx("line", { x1: "1", y1: "9", x2: "4", y2: "9" }), _jsx("line", { x1: "1", y1: "14", x2: "4", y2: "14" })] }));
const DollarIcon = () => (_jsxs("svg", { width: "14", height: "14", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("line", { x1: "12", y1: "1", x2: "12", y2: "23" }), _jsx("path", { d: "M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" })] }));
// Demo data
const demoSteps = [
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
const demoRuntimeStatus = {
    isRunning: true,
    currentStep: 'Generating plan structure...',
    elapsedTime: 45,
    tokensUsed: 2450,
    estimatedCost: 0.12,
};
// Phase colors
const phaseColors = {
    intent: { bg: colors.primary[100], border: colors.primary[400], text: colors.primary[700] },
    plan: { bg: colors.indigo[100], border: colors.indigo[400], text: colors.indigo[700] },
    exec: { bg: colors.success.light, border: colors.success.main, text: colors.success.dark },
};
// Status colors
const statusColors = {
    pending: { bg: colors.slate[100], text: colors.slate[500] },
    active: { bg: colors.primary[100], text: colors.primary[700] },
    completed: { bg: colors.success.light, text: colors.success.dark },
};
// Step Component
const StepCard = ({ step, isLast }) => {
    const phaseStyle = phaseColors[step.phase];
    const statusStyle = statusColors[step.status];
    return (_jsxs("div", { style: { display: 'flex', gap: spacing[4] }, children: [_jsxs("div", { style: { display: 'flex', flexDirection: 'column', alignItems: 'center' }, children: [_jsx("div", { style: {
                            width: 32,
                            height: 32,
                            borderRadius: borderRadius.full,
                            backgroundColor: step.status === 'completed' ? colors.success.main : step.status === 'active' ? colors.primary[500] : colors.slate[200],
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            color: step.status === 'pending' ? colors.slate[400] : '#fff',
                            flexShrink: 0,
                        }, children: step.status === 'completed' ? (_jsx(CheckIcon, {})) : (_jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold }, children: step.phase === 'intent' ? '1' : step.phase === 'plan' ? '2' : '3' })) }), !isLast && (_jsx("div", { style: {
                            width: 2,
                            flex: 1,
                            backgroundColor: step.status === 'completed' ? colors.success.main : colors.slate[200],
                            minHeight: 40,
                        } }))] }), _jsx(Card, { style: { flex: 1, marginBottom: isLast ? 0 : spacing[4] }, children: _jsxs(CardContent, { children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[2] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("h4", { style: { fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: step.title }), _jsx(Badge, { style: { backgroundColor: phaseStyle.bg, color: phaseStyle.text }, children: step.phase.toUpperCase() })] }), _jsx(Badge, { style: { backgroundColor: statusStyle.bg, color: statusStyle.text }, children: step.status })] }), _jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[500], marginBottom: spacing[3] }, children: step.description }), step.status !== 'pending' && (_jsx(ProgressBar, { value: step.progress || 0, status: step.status === 'completed' ? 'success' : 'default' }))] }) })] }));
};
// Runtime Status Sidebar
const RuntimeSidebar = ({ status, onStop }) => {
    const formatTime = (seconds) => {
        const mins = Math.floor(seconds / 60);
        const secs = seconds % 60;
        return `${mins}:${secs.toString().padStart(2, '0')}`;
    };
    return (_jsxs(Card, { style: { height: 'fit-content' }, children: [_jsx(CardHeader, { style: { borderBottom: `1px solid ${colors.slate[200]}` }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between' }, children: [_jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: "Runtime Status" }), _jsx("div", { style: {
                                width: 8,
                                height: 8,
                                borderRadius: borderRadius.full,
                                backgroundColor: status.isRunning ? colors.success.main : colors.slate[300],
                                animation: status.isRunning ? 'pulse 2s infinite' : 'none',
                            } })] }) }), _jsxs(CardContent, { children: [_jsxs("div", { style: { marginBottom: spacing[4] }, children: [_jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[400], textTransform: 'uppercase' }, children: "Current Step" }), _jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[700], marginTop: spacing[1] }, children: status.currentStep })] }), _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("div", { style: { color: colors.slate[400] }, children: _jsx(ClockIcon, {}) }), _jsxs("span", { style: { fontSize: fontSize.sm, color: colors.slate[600] }, children: ["Elapsed: ", formatTime(status.elapsedTime)] })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("div", { style: { color: colors.slate[400] }, children: _jsx(CpuIcon, {}) }), _jsxs("span", { style: { fontSize: fontSize.sm, color: colors.slate[600] }, children: ["Tokens: ", status.tokensUsed.toLocaleString()] })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("div", { style: { color: colors.slate[400] }, children: _jsx(DollarIcon, {}) }), _jsxs("span", { style: { fontSize: fontSize.sm, color: colors.slate[600] }, children: ["Cost: $", status.estimatedCost.toFixed(2)] })] })] }), status.isRunning && (_jsxs(Button, { variant: "ghost", onClick: onStop, style: {
                            marginTop: spacing[4],
                            width: '100%',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            gap: spacing[2],
                            color: colors.error.main,
                        }, children: [_jsx(StopIcon, {}), _jsx("span", { children: "Stop Execution" })] }))] })] }));
};
export const PlanView = ({ steps = demoSteps, goal: initialGoal = 'Build a responsive dashboard with real-time data visualization', runtimeStatus = demoRuntimeStatus, onGoalChange, onStartPlan, onStopPlan, className, style, }) => {
    const [goal, setGoal] = useState(initialGoal);
    const [isMobile, setIsMobile] = useState(false);
    useEffect(() => {
        const checkMobile = () => setIsMobile(window.innerWidth < breakpoints.lg);
        checkMobile();
        window.addEventListener('resize', checkMobile);
        return () => window.removeEventListener('resize', checkMobile);
    }, []);
    const handleGoalChange = (value) => {
        setGoal(value);
        onGoalChange?.(value);
    };
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: colors.slate[50],
            ...style,
        }, children: [_jsx("div", { style: {
                    padding: `${spacing[4]}px ${spacing[6]}px`,
                    backgroundColor: '#fff',
                    borderBottom: `1px solid ${colors.slate[200]}`,
                }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: {
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                width: 40,
                                height: 40,
                                backgroundColor: colors.primary[50],
                                borderRadius: borderRadius.lg,
                                color: colors.primary[600],
                            }, children: _jsx(TargetIcon, {}) }), _jsxs("div", { children: [_jsx("h1", { style: { fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: "Execution Plan" }), _jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: "Intent \u2192 Plan \u2192 Execute workflow" })] })] }) }), _jsxs("div", { style: {
                    flex: 1,
                    display: 'flex',
                    flexDirection: isMobile ? 'column' : 'row',
                    gap: spacing[6],
                    padding: spacing[6],
                    overflowY: 'auto',
                }, children: [_jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsxs(Card, { style: { marginBottom: spacing[6] }, children: [_jsx(CardHeader, { style: { borderBottom: `1px solid ${colors.slate[200]}` }, children: _jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: "Goal" }) }), _jsxs(CardContent, { children: [_jsx("textarea", { value: goal, onChange: (e) => handleGoalChange(e.target.value), placeholder: "Describe your goal...", style: {
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
                                                }, onFocus: (e) => {
                                                    e.target.style.borderColor = colors.primary[400];
                                                    e.target.style.boxShadow = `0 0 0 3px ${colors.primary[100]}`;
                                                }, onBlur: (e) => {
                                                    e.target.style.borderColor = colors.slate[200];
                                                    e.target.style.boxShadow = 'none';
                                                } }), _jsx("div", { style: { display: 'flex', justifyContent: 'flex-end', marginTop: spacing[3] }, children: _jsxs(Button, { variant: "primary", onClick: onStartPlan, style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx(PlayIcon, {}), _jsx("span", { children: "Start Plan" })] }) })] })] }), _jsxs("div", { children: [_jsx("h3", { style: {
                                            fontSize: fontSize.base,
                                            fontWeight: fontWeight.semibold,
                                            color: colors.slate[800],
                                            marginBottom: spacing[4],
                                        }, children: "Execution Steps" }), steps.map((step, index) => (_jsx(StepCard, { step: step, isLast: index === steps.length - 1 }, step.id)))] })] }), _jsx("div", { style: { width: isMobile ? '100%' : 280, flexShrink: 0 }, children: _jsx(RuntimeSidebar, { status: runtimeStatus, onStop: onStopPlan || (() => { }) }) })] })] }));
};
export default PlanView;
//# sourceMappingURL=PlanView.js.map