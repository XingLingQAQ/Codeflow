import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * PlanModePanel - Plan 模式面板组件
 * 可视化展示 Plan 模式各阶段，支持编辑愿景和约束
 */
import React, { useState, useMemo } from 'react';
import { PHASE_CONFIG, ARTIFACT_CONFIG, CONSTRAINT_TYPE_CONFIG, } from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Tabs, TabPanel } from '../shared/Tabs';
/**
 * 阶段进度条组件
 */
const PhaseProgress = ({ phases, currentPhase, onPhaseSelect }) => {
    return (_jsx("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[1], padding: spacing[4] }, children: phases.map((phase, index) => {
            const config = PHASE_CONFIG[phase.id] || { icon: '📌', color: colors.slate[500], label: phase.name };
            const isActive = phase.id === currentPhase;
            const isCompleted = phase.status === 'completed';
            return (_jsxs(React.Fragment, { children: [index > 0 && (_jsx("div", { style: {
                            flex: 1,
                            height: 2,
                            backgroundColor: isCompleted ? colors.primary[400] : colors.slate[200],
                            transition: transitions.normal,
                        } })), _jsxs("div", { onClick: () => onPhaseSelect?.(phase.id), style: {
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            gap: spacing[1],
                            cursor: 'pointer',
                            transition: transitions.fast,
                        }, children: [_jsx("div", { style: {
                                    width: isActive ? 48 : 40,
                                    height: isActive ? 48 : 40,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: isCompleted
                                        ? colors.primary[500]
                                        : isActive
                                            ? config.color
                                            : colors.slate[100],
                                    border: isActive ? `3px solid ${config.color}30` : 'none',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: isActive ? 20 : 16,
                                    boxShadow: isActive ? shadows.lg : shadows.sm,
                                    transition: transitions.fast,
                                }, children: isCompleted ? '✓' : config.icon }), _jsx("span", { style: {
                                    fontSize: fontSize.xs,
                                    fontWeight: isActive ? fontWeight.bold : fontWeight.medium,
                                    color: isActive ? config.color : colors.slate[500],
                                    textTransform: 'uppercase',
                                }, children: config.label })] })] }, phase.id));
        }) }));
};
/**
 * 愿景编辑器组件
 */
const VisionEditor = ({ vision, onUpdate }) => {
    const [editedVision, setEditedVision] = useState(vision || { goal: '', requirements: [], constraints: [], questions: [] });
    const handleGoalChange = (goal) => {
        const updated = { ...editedVision, goal };
        setEditedVision(updated);
        onUpdate?.(updated);
    };
    const handleRequirementAdd = () => {
        const updated = {
            ...editedVision,
            requirements: [...editedVision.requirements, ''],
        };
        setEditedVision(updated);
    };
    const handleRequirementChange = (index, value) => {
        const requirements = [...editedVision.requirements];
        requirements[index] = value;
        const updated = { ...editedVision, requirements };
        setEditedVision(updated);
        onUpdate?.(updated);
    };
    const handleRequirementRemove = (index) => {
        const requirements = editedVision.requirements.filter((_, i) => i !== index);
        const updated = { ...editedVision, requirements };
        setEditedVision(updated);
        onUpdate?.(updated);
    };
    return (_jsxs("div", { style: { padding: spacing[4] }, children: [_jsxs("div", { style: { marginBottom: spacing[6] }, children: [_jsx("label", { style: {
                            display: 'block',
                            marginBottom: spacing[2],
                            fontSize: fontSize.xs,
                            fontWeight: fontWeight.bold,
                            color: colors.slate[700],
                            textTransform: 'uppercase',
                        }, children: "Goal Statement" }), _jsx("textarea", { value: editedVision.goal, onChange: (e) => handleGoalChange(e.target.value), placeholder: "Describe the main goal of this plan...", style: {
                            width: '100%',
                            minHeight: 100,
                            padding: spacing[3],
                            fontSize: fontSize.sm,
                            borderRadius: borderRadius.xl,
                            border: `1px solid ${colors.slate[200]}`,
                            backgroundColor: colors.slate[50],
                            resize: 'vertical',
                            outline: 'none',
                            transition: transitions.fast,
                        } })] }), _jsxs("div", { style: { marginBottom: spacing[6] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[2] }, children: [_jsx("label", { style: {
                                    fontSize: fontSize.xs,
                                    fontWeight: fontWeight.bold,
                                    color: colors.slate[700],
                                    textTransform: 'uppercase',
                                }, children: "Core Requirements" }), _jsx(Button, { size: "sm", variant: "ghost", onClick: handleRequirementAdd, children: "+ Add" })] }), _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[2] }, children: [editedVision.requirements.map((req, index) => (_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("span", { style: { color: colors.primary[500], fontWeight: fontWeight.bold }, children: "\u2022" }), _jsx("input", { type: "text", value: req, onChange: (e) => handleRequirementChange(index, e.target.value), placeholder: "Enter requirement...", style: {
                                            flex: 1,
                                            padding: `${spacing[2]}px ${spacing[3]}px`,
                                            fontSize: fontSize.sm,
                                            borderRadius: borderRadius.lg,
                                            border: `1px solid ${colors.slate[200]}`,
                                            backgroundColor: '#fff',
                                            outline: 'none',
                                        } }), _jsx("button", { onClick: () => handleRequirementRemove(index), style: {
                                            width: 24,
                                            height: 24,
                                            borderRadius: borderRadius.full,
                                            backgroundColor: colors.slate[100],
                                            border: 'none',
                                            cursor: 'pointer',
                                            color: colors.slate[500],
                                            fontSize: 12,
                                        }, children: "\u00D7" })] }, index))), editedVision.requirements.length === 0 && (_jsx("div", { style: { padding: spacing[4], textAlign: 'center', color: colors.slate[400], fontSize: fontSize.sm }, children: "No requirements added yet" }))] })] }), editedVision.questions.length > 0 && (_jsxs("div", { children: [_jsx("label", { style: {
                            display: 'block',
                            marginBottom: spacing[2],
                            fontSize: fontSize.xs,
                            fontWeight: fontWeight.bold,
                            color: colors.slate[700],
                            textTransform: 'uppercase',
                        }, children: "Clarifying Questions" }), _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: editedVision.questions.map((q) => (_jsxs(Card, { padding: "sm", variant: "outlined", children: [_jsxs("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700], marginBottom: spacing[2] }, children: [q.question, q.required && _jsx("span", { style: { color: colors.error.main, marginLeft: 4 }, children: "*" })] }), _jsx("input", { type: "text", value: q.answer || '', placeholder: "Your answer...", style: {
                                        width: '100%',
                                        padding: `${spacing[2]}px ${spacing[3]}px`,
                                        fontSize: fontSize.sm,
                                        borderRadius: borderRadius.lg,
                                        border: `1px solid ${colors.slate[200]}`,
                                        backgroundColor: colors.slate[50],
                                        outline: 'none',
                                    } })] }, q.id))) })] }))] }));
};
/**
 * 约束列表组件
 */
const ConstraintList = ({ constraints = [], onAdd, onRemove }) => {
    const [showAddForm, setShowAddForm] = useState(false);
    const [newConstraint, setNewConstraint] = useState({
        type: 'functional',
        description: '',
        priority: 'should',
        source: 'manual',
    });
    const groupedConstraints = useMemo(() => {
        const groups = {
            functional: [],
            technical: [],
            business: [],
            security: [],
        };
        constraints.forEach((c) => {
            groups[c.type]?.push(c);
        });
        return groups;
    }, [constraints]);
    const handleAdd = () => {
        if (newConstraint.description.trim()) {
            onAdd?.(newConstraint);
            setNewConstraint({ type: 'functional', description: '', priority: 'should', source: 'manual' });
            setShowAddForm(false);
        }
    };
    const typeOptions = Object.entries(CONSTRAINT_TYPE_CONFIG).map(([key, config]) => ({
        value: key,
        label: config.label,
        icon: _jsx("span", { children: config.icon }),
        color: config.color,
    }));
    const priorityOptions = [
        { value: 'must', label: 'Must Have', color: colors.error.main },
        { value: 'should', label: 'Should Have', color: colors.warning.main },
        { value: 'could', label: 'Could Have', color: colors.success.main },
    ];
    return (_jsxs("div", { style: { padding: spacing[4] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[4] }, children: [_jsxs("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700] }, children: ["Constraints (", constraints.length, ")"] }), _jsx(Button, { size: "sm", variant: "secondary", onClick: () => setShowAddForm(!showAddForm), children: showAddForm ? 'Cancel' : '+ Add Constraint' })] }), showAddForm && (_jsx(Card, { padding: "md", style: { marginBottom: spacing[4], backgroundColor: colors.slate[50] }, children: _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', gap: spacing[3] }, children: [_jsx(CustomSelect, { options: typeOptions, value: newConstraint.type, onChange: (v) => setNewConstraint({ ...newConstraint, type: v }), label: "Type", size: "sm", style: { flex: 1 } }), _jsx(CustomSelect, { options: priorityOptions, value: newConstraint.priority, onChange: (v) => setNewConstraint({ ...newConstraint, priority: v }), label: "Priority", size: "sm", style: { flex: 1 } })] }), _jsx("textarea", { value: newConstraint.description, onChange: (e) => setNewConstraint({ ...newConstraint, description: e.target.value }), placeholder: "Describe the constraint...", style: {
                                width: '100%',
                                minHeight: 80,
                                padding: spacing[3],
                                fontSize: fontSize.sm,
                                borderRadius: borderRadius.lg,
                                border: `1px solid ${colors.slate[200]}`,
                                backgroundColor: '#fff',
                                resize: 'vertical',
                                outline: 'none',
                            } }), _jsx(Button, { size: "sm", onClick: handleAdd, children: "Add Constraint" })] }) })), Object.entries(groupedConstraints).map(([type, items]) => {
                if (items.length === 0)
                    return null;
                const config = CONSTRAINT_TYPE_CONFIG[type];
                return (_jsxs("div", { style: { marginBottom: spacing[4] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2], marginBottom: spacing[2] }, children: [_jsx("span", { children: config.icon }), _jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: config.color, textTransform: 'uppercase' }, children: config.label }), _jsx(Badge, { size: "sm", variant: "default", children: items.length })] }), _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[2] }, children: items.map((constraint) => (_jsxs("div", { style: {
                                    display: 'flex',
                                    alignItems: 'flex-start',
                                    gap: spacing[3],
                                    padding: spacing[3],
                                    backgroundColor: '#fff',
                                    borderRadius: borderRadius.lg,
                                    border: `1px solid ${colors.slate[200]}`,
                                }, children: [_jsx("div", { style: {
                                            width: 4,
                                            height: '100%',
                                            minHeight: 40,
                                            borderRadius: 2,
                                            backgroundColor: constraint.priority === 'must'
                                                ? colors.error.main
                                                : constraint.priority === 'should'
                                                    ? colors.warning.main
                                                    : colors.success.main,
                                        } }), _jsxs("div", { style: { flex: 1 }, children: [_jsx("div", { style: { fontSize: fontSize.sm, color: colors.slate[700], marginBottom: spacing[1] }, children: constraint.description }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx(Badge, { size: "sm", variant: constraint.priority === 'must' ? 'error' : constraint.priority === 'should' ? 'warning' : 'success', children: constraint.priority }), _jsxs("span", { style: { fontSize: fontSize.xs, color: colors.slate[400] }, children: ["Source: ", constraint.source] })] })] }), onRemove && (_jsx("button", { onClick: () => onRemove(constraint.id), style: {
                                            width: 24,
                                            height: 24,
                                            borderRadius: borderRadius.full,
                                            backgroundColor: colors.slate[100],
                                            border: 'none',
                                            cursor: 'pointer',
                                            color: colors.slate[500],
                                            fontSize: 12,
                                        }, children: "\u00D7" }))] }, constraint.id))) })] }, type));
            }), constraints.length === 0 && !showAddForm && (_jsx("div", { style: { padding: spacing[6], textAlign: 'center', color: colors.slate[400], fontSize: fontSize.sm }, children: "No constraints defined yet" }))] }));
};
/**
 * 工件查看器组件
 */
const ArtifactViewer = ({ artifacts = [], onView }) => {
    return (_jsxs("div", { style: { padding: spacing[4] }, children: [_jsx("div", { style: { marginBottom: spacing[4] }, children: _jsxs("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[700] }, children: ["Generated Artifacts (", artifacts.length, ")"] }) }), _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: spacing[3] }, children: artifacts.map((artifact) => {
                    const config = ARTIFACT_CONFIG[artifact.type] || { icon: '📄', color: colors.slate[500], label: artifact.type };
                    return (_jsxs(Card, { padding: "md", hoverable: true, onClick: () => onView?.(artifact.id), style: { cursor: 'pointer' }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3], marginBottom: spacing[2] }, children: [_jsx("div", { style: {
                                            width: 36,
                                            height: 36,
                                            borderRadius: borderRadius.lg,
                                            backgroundColor: `${config.color}15`,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            fontSize: 18,
                                        }, children: config.icon }), _jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsx("div", { style: {
                                                    fontSize: fontSize.sm,
                                                    fontWeight: fontWeight.semibold,
                                                    color: colors.slate[800],
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                }, children: artifact.name }), _jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: config.label })] })] }), _jsx(Badge, { size: "sm", variant: artifact.status === 'completed'
                                    ? 'success'
                                    : artifact.status === 'generating'
                                        ? 'info'
                                        : artifact.status === 'error'
                                            ? 'error'
                                            : 'default', dot: artifact.status === 'generating', children: artifact.status })] }, artifact.id));
                }) }), artifacts.length === 0 && (_jsx("div", { style: { padding: spacing[6], textAlign: 'center', color: colors.slate[400], fontSize: fontSize.sm }, children: "No artifacts generated yet" }))] }));
};
/**
 * Plan 模式面板主组件
 */
export const PlanModePanel = ({ planId, planName, phases, vision, constraints, artifacts, currentPhase, onPhaseSelect, onVisionUpdate, onConstraintAdd, onConstraintRemove, onArtifactView, onExecute, onFastForward, className, style, }) => {
    const [activeTab, setActiveTab] = useState('vision');
    const tabs = [
        { id: 'vision', label: 'Vision', icon: _jsx("span", { children: "\uD83D\uDCA1" }) },
        { id: 'constraints', label: 'Constraints', icon: _jsx("span", { children: "\uD83D\uDD12" }), badge: constraints?.length },
        { id: 'artifacts', label: 'Artifacts', icon: _jsx("span", { children: "\uD83D\uDCE6" }), badge: artifacts?.length },
    ];
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
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[4] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: {
                                            width: 40,
                                            height: 40,
                                            borderRadius: borderRadius.xl,
                                            background: `linear-gradient(135deg, ${colors.primary[500]}, ${colors.indigo[500]})`,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            color: '#fff',
                                            fontSize: 18,
                                            boxShadow: shadows.blue,
                                        }, children: "\uD83D\uDCCB" }), _jsxs("div", { children: [_jsx("h1", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[900], margin: 0 }, children: planName }), _jsxs("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: ["Plan ID: ", planId] })] })] }), _jsxs("div", { style: { display: 'flex', gap: spacing[2] }, children: [_jsx(Button, { variant: "secondary", onClick: onFastForward, children: "\u26A1 Fast Forward" }), _jsx(Button, { onClick: onExecute, children: "\u25B6\uFE0F Execute Plan" })] })] }), _jsx(PhaseProgress, { phases: phases, currentPhase: currentPhase, onPhaseSelect: onPhaseSelect })] }), _jsx("div", { style: { padding: `${spacing[3]}px ${spacing[6]}px`, backgroundColor: '#fff', borderBottom: `1px solid ${colors.slate[100]}` }, children: _jsx(Tabs, { tabs: tabs, activeTab: activeTab, onChange: setActiveTab, variant: "pills" }) }), _jsxs("div", { style: { flex: 1, overflow: 'auto' }, children: [_jsx(TabPanel, { tabId: "vision", activeTab: activeTab, children: _jsx(VisionEditor, { vision: vision, onUpdate: onVisionUpdate }) }), _jsx(TabPanel, { tabId: "constraints", activeTab: activeTab, children: _jsx(ConstraintList, { constraints: constraints, onAdd: onConstraintAdd, onRemove: onConstraintRemove }) }), _jsx(TabPanel, { tabId: "artifacts", activeTab: activeTab, children: _jsx(ArtifactViewer, { artifacts: artifacts, onView: onArtifactView }) })] })] }));
};
export default PlanModePanel;
//# sourceMappingURL=PlanModePanel.js.map