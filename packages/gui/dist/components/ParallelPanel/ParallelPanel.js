import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
/**
 * ParallelPanel - 并行模式面板组件
 * 实时显示各 Agent 执行状态，并排对比多个方案
 */
import React, { useState, useMemo } from 'react';
import { WORKER_STATUS_CONFIG, PROVIDER_COLORS, METRIC_CONFIG, } from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Tabs, TabPanel } from '../shared/Tabs';
import { Modal } from '../shared/Modal';
/**
 * Worker 状态卡片
 */
const WorkerCard = ({ worker, isSelected, onSelect, onCancel }) => {
    const statusConfig = WORKER_STATUS_CONFIG[worker.status];
    const providerColor = PROVIDER_COLORS[worker.modelProvider] || colors.slate[500];
    return (_jsxs(Card, { padding: "md", hoverable: true, selected: isSelected, onClick: onSelect, style: { minWidth: 240 }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("div", { style: {
                                    width: 32,
                                    height: 32,
                                    borderRadius: borderRadius.lg,
                                    backgroundColor: `${providerColor}15`,
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 16,
                                }, children: "\uD83E\uDD16" }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: worker.name }), _jsx("div", { style: { fontSize: fontSize.xs, color: providerColor }, children: worker.model })] })] }), _jsx(Badge, { size: "sm", variant: worker.status === 'completed'
                            ? 'success'
                            : worker.status === 'running'
                                ? 'info'
                                : worker.status === 'failed'
                                    ? 'error'
                                    : 'default', dot: worker.status === 'running', children: statusConfig.label })] }), worker.status === 'running' && (_jsxs("div", { style: { marginBottom: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }, children: [_jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Progress" }), _jsxs("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.primary[600] }, children: [worker.progress, "%"] })] }), _jsx("div", { style: {
                            height: 4,
                            borderRadius: borderRadius.full,
                            backgroundColor: colors.slate[100],
                            overflow: 'hidden',
                        }, children: _jsx("div", { style: {
                                width: `${worker.progress}%`,
                                height: '100%',
                                backgroundColor: colors.primary[500],
                                borderRadius: borderRadius.full,
                                transition: transitions.normal,
                            } }) })] })), _jsx("div", { style: { marginBottom: spacing[3] }, children: _jsxs("div", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }, children: ["Branch: ", _jsx("span", { style: { fontFamily: 'monospace', color: colors.slate[700] }, children: worker.branch })] }) }), worker.solution && (_jsx("div", { style: { display: 'flex', gap: spacing[2], flexWrap: 'wrap' }, children: Object.entries(worker.solution.metrics).slice(0, 3).map(([key, value]) => {
                    const config = METRIC_CONFIG[key];
                    return (_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            gap: 4,
                            padding: `${spacing[0.5]}px ${spacing[1.5]}px`,
                            backgroundColor: colors.slate[50],
                            borderRadius: borderRadius.md,
                            fontSize: fontSize.xs,
                        }, children: [_jsx("span", { children: config?.icon }), _jsx("span", { style: { color: colors.slate[600] }, children: value.toFixed(0) })] }, key));
                }) })), worker.status === 'running' && onCancel && (_jsx(Button, { size: "sm", variant: "ghost", onClick: (e) => {
                    e.stopPropagation();
                    onCancel();
                }, style: { marginTop: spacing[3], width: '100%' }, children: "Cancel" }))] }));
};
/**
 * 方案对比视图
 */
const SolutionCompare = ({ solutions, selectedId, onSelect }) => {
    if (solutions.length === 0) {
        return (_jsx("div", { style: { padding: spacing[6], textAlign: 'center', color: colors.slate[400] }, children: "No solutions available yet" }));
    }
    return (_jsxs("div", { style: { padding: spacing[4] }, children: [_jsxs("div", { style: { marginBottom: spacing[6] }, children: [_jsx("h3", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700], marginBottom: spacing[3] }, children: "Metrics Comparison" }), _jsxs("div", { style: {
                            display: 'grid',
                            gridTemplateColumns: `120px repeat(${solutions.length}, 1fr)`,
                            gap: 1,
                            backgroundColor: colors.slate[200],
                            borderRadius: borderRadius.lg,
                            overflow: 'hidden',
                        }, children: [_jsx("div", { style: { padding: spacing[3], backgroundColor: colors.slate[100], fontWeight: fontWeight.semibold, fontSize: fontSize.xs }, children: "Metric" }), solutions.map((sol) => (_jsx("div", { style: {
                                    padding: spacing[3],
                                    backgroundColor: sol.id === selectedId ? colors.primary[50] : colors.slate[100],
                                    fontWeight: fontWeight.semibold,
                                    fontSize: fontSize.xs,
                                    textAlign: 'center',
                                    cursor: 'pointer',
                                }, onClick: () => onSelect(sol.id), children: sol.workerId }, sol.id))), Object.keys(METRIC_CONFIG).map((metricKey) => {
                                const config = METRIC_CONFIG[metricKey];
                                const values = solutions.map((s) => s.metrics[metricKey]);
                                const maxValue = Math.max(...values);
                                return (_jsxs(React.Fragment, { children: [_jsxs("div", { style: {
                                                padding: spacing[3],
                                                backgroundColor: '#fff',
                                                fontSize: fontSize.xs,
                                                display: 'flex',
                                                alignItems: 'center',
                                                gap: spacing[1],
                                            }, children: [_jsx("span", { children: config.icon }), config.label] }), solutions.map((sol) => {
                                            const value = sol.metrics[metricKey];
                                            const isMax = value === maxValue;
                                            return (_jsxs("div", { style: {
                                                    padding: spacing[3],
                                                    backgroundColor: sol.id === selectedId ? colors.primary[50] : '#fff',
                                                    textAlign: 'center',
                                                    fontSize: fontSize.sm,
                                                    fontWeight: isMax ? fontWeight.bold : fontWeight.normal,
                                                    color: isMax ? config.color : colors.slate[600],
                                                }, children: [value.toFixed(1), isMax && _jsx("span", { style: { marginLeft: 4 }, children: "\uD83C\uDFC6" })] }, sol.id));
                                        })] }, metricKey));
                            })] })] }), _jsxs("div", { children: [_jsx("h3", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700], marginBottom: spacing[3] }, children: "File Changes" }), _jsx("div", { style: { display: 'flex', gap: spacing[4], overflowX: 'auto' }, children: solutions.map((sol) => (_jsxs(Card, { padding: "md", selected: sol.id === selectedId, onClick: () => onSelect(sol.id), style: { minWidth: 280, flex: 1 }, children: [_jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800], marginBottom: spacing[3] }, children: sol.workerId }), _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[2] }, children: [sol.files.slice(0, 5).map((file, i) => (_jsxs("div", { style: {
                                                display: 'flex',
                                                alignItems: 'center',
                                                gap: spacing[2],
                                                padding: spacing[2],
                                                backgroundColor: colors.slate[50],
                                                borderRadius: borderRadius.md,
                                                fontSize: fontSize.xs,
                                            }, children: [_jsx("span", { style: {
                                                        color: file.action === 'create'
                                                            ? colors.success.main
                                                            : file.action === 'delete'
                                                                ? colors.error.main
                                                                : colors.warning.main,
                                                    }, children: file.action === 'create' ? '+' : file.action === 'delete' ? '-' : '~' }), _jsx("span", { style: { flex: 1, fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis' }, children: file.path }), _jsxs("span", { style: { color: colors.success.main }, children: ["+", file.additions] }), _jsxs("span", { style: { color: colors.error.main }, children: ["-", file.deletions] })] }, i))), sol.files.length > 5 && (_jsxs("div", { style: { fontSize: fontSize.xs, color: colors.slate[400], textAlign: 'center' }, children: ["+", sol.files.length - 5, " more files"] }))] })] }, sol.id))) })] })] }));
};
/**
 * 合并预览模态框
 */
const MergePreviewModal = ({ isOpen, solution, onClose, onMerge }) => {
    const [strategy, setStrategy] = useState('merge');
    const strategyOptions = [
        { value: 'fast-forward', label: 'Fast Forward', description: 'Move HEAD to target branch' },
        { value: 'merge', label: 'Merge', description: 'Create a merge commit' },
        { value: 'rebase', label: 'Rebase', description: 'Rebase commits onto main' },
    ];
    if (!solution)
        return null;
    return (_jsx(Modal, { isOpen: isOpen, onClose: onClose, title: "Merge Solution", subtitle: `Merging ${solution.workerId}'s solution`, icon: _jsx("span", { children: "\uD83D\uDD00" }), size: "md", footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "secondary", onClick: onClose, children: "Cancel" }), _jsx(Button, { onClick: () => onMerge(strategy), children: "Merge Solution" })] }), children: _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[4] }, children: [_jsxs("div", { children: [_jsx("h4", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }, children: "Summary" }), _jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[600], lineHeight: 1.6 }, children: solution.summary })] }), _jsxs("div", { children: [_jsxs("h4", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }, children: ["Changes (", solution.files.length, " files)"] }), _jsx("div", { style: {
                                maxHeight: 200,
                                overflow: 'auto',
                                backgroundColor: colors.slate[50],
                                borderRadius: borderRadius.lg,
                                padding: spacing[3],
                            }, children: solution.files.map((file, i) => (_jsxs("div", { style: {
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: spacing[2],
                                    padding: `${spacing[1]}px 0`,
                                    fontSize: fontSize.xs,
                                    fontFamily: 'monospace',
                                }, children: [_jsx("span", { style: {
                                            color: file.action === 'create'
                                                ? colors.success.main
                                                : file.action === 'delete'
                                                    ? colors.error.main
                                                    : colors.warning.main,
                                        }, children: file.action === 'create' ? 'A' : file.action === 'delete' ? 'D' : 'M' }), _jsx("span", { style: { flex: 1 }, children: file.path })] }, i))) })] }), _jsx(CustomSelect, { options: strategyOptions, value: strategy, onChange: (v) => setStrategy(v), label: "Merge Strategy" })] }) }));
};
/**
 * 并行模式面板主组件
 */
export const ParallelPanel = ({ task, onWorkerSelect, onSolutionSelect, onSolutionMerge, onTaskCancel, onWorkerCancel, className, style, }) => {
    const [selectedWorkerId, setSelectedWorkerId] = useState(null);
    const [activeTab, setActiveTab] = useState('workers');
    const [showMergeModal, setShowMergeModal] = useState(false);
    const solutions = useMemo(() => {
        return task?.workers.filter((w) => w.solution).map((w) => w.solution) || [];
    }, [task]);
    const selectedSolution = useMemo(() => {
        return solutions.find((s) => s.id === task?.selectedSolutionId);
    }, [solutions, task?.selectedSolutionId]);
    const handleWorkerSelect = (workerId) => {
        setSelectedWorkerId(workerId);
        onWorkerSelect?.(workerId);
    };
    const handleSolutionSelect = (solutionId) => {
        onSolutionSelect?.(solutionId);
    };
    const handleMerge = (strategy) => {
        if (task?.selectedSolutionId) {
            onSolutionMerge?.(task.selectedSolutionId, strategy);
            setShowMergeModal(false);
        }
    };
    const tabs = [
        { id: 'workers', label: 'Workers', icon: _jsx("span", { children: "\uD83E\uDD16" }), badge: task?.workers.length },
        { id: 'compare', label: 'Compare', icon: _jsx("span", { children: "\u2696\uFE0F" }), badge: solutions.length },
    ];
    if (!task) {
        return (_jsx("div", { className: className, style: {
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                height: '100%',
                backgroundColor: colors.slate[50],
                ...style,
            }, children: _jsxs("div", { style: { textAlign: 'center', color: colors.slate[400] }, children: [_jsx("div", { style: { fontSize: 48, marginBottom: spacing[4] }, children: "\uD83D\uDD04" }), _jsx("div", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.semibold, marginBottom: spacing[2] }, children: "No Active Parallel Task" }), _jsx("div", { style: { fontSize: fontSize.sm }, children: "Start a parallel task to see workers here" })] }) }));
    }
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: colors.slate[50],
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: `${spacing[4]}px ${spacing[6]}px`,
                    backgroundColor: '#fff',
                    borderBottom: `1px solid ${colors.slate[200]}`,
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: {
                                            width: 40,
                                            height: 40,
                                            borderRadius: borderRadius.xl,
                                            background: `linear-gradient(135deg, ${colors.indigo[500]}, ${colors.primary[500]})`,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            color: '#fff',
                                            fontSize: 18,
                                            boxShadow: shadows.indigo,
                                        }, children: "\uD83D\uDD00" }), _jsxs("div", { children: [_jsx("h1", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[900], margin: 0 }, children: task.name }), _jsxs("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: [task.workers.filter((w) => w.status === 'completed').length, "/", task.workers.length, " workers completed"] })] })] }), _jsxs("div", { style: { display: 'flex', gap: spacing[2] }, children: [task.selectedSolutionId && (_jsx(Button, { onClick: () => setShowMergeModal(true), children: "\uD83D\uDD00 Merge Selected" })), task.status === 'running' && (_jsx(Button, { variant: "danger", onClick: onTaskCancel, children: "Cancel Task" }))] })] }), _jsxs("div", { children: [_jsxs("div", { style: { display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }, children: [_jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Overall Progress" }), _jsxs("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.primary[600] }, children: [Math.round(task.workers.reduce((acc, w) => acc + w.progress, 0) / task.workers.length), "%"] })] }), _jsx("div", { style: {
                                    height: 6,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: colors.slate[100],
                                    overflow: 'hidden',
                                    display: 'flex',
                                }, children: task.workers.map((worker) => (_jsx("div", { style: {
                                        flex: 1,
                                        height: '100%',
                                        backgroundColor: worker.status === 'completed'
                                            ? colors.success.main
                                            : worker.status === 'failed'
                                                ? colors.error.main
                                                : worker.status === 'running'
                                                    ? colors.primary[500]
                                                    : colors.slate[300],
                                        transition: transitions.normal,
                                    } }, worker.id))) })] })] }), _jsx("div", { style: { padding: `${spacing[3]}px ${spacing[6]}px`, backgroundColor: '#fff', borderBottom: `1px solid ${colors.slate[100]}` }, children: _jsx(Tabs, { tabs: tabs, activeTab: activeTab, onChange: setActiveTab, variant: "pills" }) }), _jsxs("div", { style: { flex: 1, overflow: 'auto' }, children: [_jsx(TabPanel, { tabId: "workers", activeTab: activeTab, children: _jsx("div", { style: { padding: spacing[4] }, children: _jsx("div", { style: { display: 'flex', gap: spacing[4], overflowX: 'auto', paddingBottom: spacing[2] }, children: task.workers.map((worker) => (_jsx(WorkerCard, { worker: worker, isSelected: selectedWorkerId === worker.id, onSelect: () => handleWorkerSelect(worker.id), onCancel: worker.status === 'running' ? () => onWorkerCancel?.(worker.id) : undefined }, worker.id))) }) }) }), _jsx(TabPanel, { tabId: "compare", activeTab: activeTab, children: _jsx(SolutionCompare, { solutions: solutions, selectedId: task.selectedSolutionId, onSelect: handleSolutionSelect }) })] }), _jsx(MergePreviewModal, { isOpen: showMergeModal, solution: selectedSolution, onClose: () => setShowMergeModal(false), onMerge: handleMerge })] }));
};
export default ParallelPanel;
//# sourceMappingURL=ParallelPanel.js.map