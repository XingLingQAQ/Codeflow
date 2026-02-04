import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * AgentModelConfig - Agent 模型配置面板
 * P027: 实现各 Agent 的模型配置 UI
 */
import { useState } from 'react';
import { AGENT_ROLE_INFO, TASK_TYPE_INFO, PROVIDER_INFO, formatCost, formatContextWindow, } from './types';
import { colors, borderRadius, fontSize, fontWeight, spacing, transitions } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Toggle } from '../shared/Toggle';
export const AgentModelConfigPanel = ({ agents, models, presets = [], onAgentModelChange, onTaskTypeOverrideChange, onPresetApply, onPresetSave, className, style, }) => {
    const [expandedAgentId, setExpandedAgentId] = useState(null);
    const [showAdvanced, setShowAdvanced] = useState(false);
    const modelOptions = models.map((m) => ({
        value: m.id,
        label: m.name,
        description: `${PROVIDER_INFO[m.provider]?.name || m.provider} • ${formatContextWindow(m.contextWindow)} context`,
        icon: _jsx("span", { children: PROVIDER_INFO[m.provider]?.icon || '🤖' }),
        color: PROVIDER_INFO[m.provider]?.color,
    }));
    const toggleExpand = (agentId) => {
        setExpandedAgentId(expandedAgentId === agentId ? null : agentId);
    };
    return (_jsxs("div", { className: className, style: {
            backgroundColor: '#fff',
            borderRadius: borderRadius['2xl'],
            border: `1px solid ${colors.slate[200]}`,
            overflow: 'hidden',
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: spacing[4],
                    borderBottom: `1px solid ${colors.slate[100]}`,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: {
                                    width: 36,
                                    height: 36,
                                    borderRadius: borderRadius.lg,
                                    backgroundColor: colors.indigo[50],
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 18,
                                }, children: "\uD83E\uDD16" }), _jsxs("div", { children: [_jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }, children: "Agent Model Configuration" }), _jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Configure models for each agent role" })] })] }), _jsx(Toggle, { checked: showAdvanced, onChange: setShowAdvanced, label: "Advanced", size: "sm" })] }), _jsx("div", { style: { padding: spacing[4] }, children: _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: agents.map((agent) => {
                        const roleInfo = AGENT_ROLE_INFO[agent.agentRole] || { icon: '🤖', color: colors.slate[500], description: '' };
                        const selectedModel = models.find((m) => m.id === agent.modelId);
                        const isExpanded = expandedAgentId === agent.agentId;
                        const hasOverrides = agent.taskTypeOverrides && Object.keys(agent.taskTypeOverrides).length > 0;
                        return (_jsxs(Card, { padding: "none", style: { overflow: 'hidden' }, children: [_jsxs("div", { style: {
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: spacing[4],
                                        padding: spacing[4],
                                        cursor: showAdvanced ? 'pointer' : 'default',
                                    }, onClick: () => showAdvanced && toggleExpand(agent.agentId), children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3], minWidth: 200 }, children: [_jsx("div", { style: {
                                                        width: 44,
                                                        height: 44,
                                                        borderRadius: borderRadius.xl,
                                                        backgroundColor: `${roleInfo.color}15`,
                                                        display: 'flex',
                                                        alignItems: 'center',
                                                        justifyContent: 'center',
                                                        fontSize: 22,
                                                    }, children: roleInfo.icon }), _jsxs("div", { children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: agent.agentName }), hasOverrides && (_jsxs(Badge, { size: "sm", variant: "warning", children: [Object.keys(agent.taskTypeOverrides).length, " overrides"] }))] }), _jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: roleInfo.description })] })] }), _jsx("div", { style: { flex: 1 }, onClick: (e) => e.stopPropagation(), children: _jsx(CustomSelect, { options: modelOptions, value: agent.modelId, onChange: (v) => onAgentModelChange?.(agent.agentId, v), placeholder: "Select model...", size: "sm", searchable: true }) }), selectedModel && (_jsxs("div", { style: { textAlign: 'right', minWidth: 100 }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Cost" }), _jsxs("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }, children: [formatCost(selectedModel.costPerInputToken), " in"] }), _jsxs("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }, children: [formatCost(selectedModel.costPerOutputToken), " out"] })] })), showAdvanced && (_jsx("span", { style: {
                                                fontSize: 12,
                                                color: colors.slate[400],
                                                transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                                                transition: transitions.fast,
                                            }, children: "\u25BC" }))] }), showAdvanced && isExpanded && (_jsxs("div", { style: {
                                        padding: spacing[4],
                                        paddingTop: 0,
                                        borderTop: `1px solid ${colors.slate[100]}`,
                                        backgroundColor: colors.slate[50],
                                    }, children: [_jsxs("div", { style: { marginBottom: spacing[3] }, children: [_jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: colors.slate[600], textTransform: 'uppercase' }, children: "Task Type Overrides" }), _jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[400], marginLeft: spacing[2] }, children: "(Optional: Use different models for specific task types)" })] }), _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: spacing[3] }, children: Object.entries(TASK_TYPE_INFO).map(([taskType, info]) => {
                                                const overrideModelId = agent.taskTypeOverrides?.[taskType];
                                                return (_jsxs("div", { style: {
                                                        display: 'flex',
                                                        alignItems: 'center',
                                                        gap: spacing[2],
                                                        padding: spacing[2],
                                                        backgroundColor: '#fff',
                                                        borderRadius: borderRadius.lg,
                                                        border: `1px solid ${colors.slate[200]}`,
                                                    }, children: [_jsx("span", { style: { fontSize: 16 }, children: info.icon }), _jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.medium, color: colors.slate[700], minWidth: 70 }, children: info.label }), _jsx("div", { style: { flex: 1 }, children: _jsx(CustomSelect, { options: [
                                                                    { value: '', label: 'Use default', description: 'Inherit from agent' },
                                                                    ...modelOptions,
                                                                ], value: overrideModelId || '', onChange: (v) => onTaskTypeOverrideChange?.(agent.agentId, taskType, v || null), placeholder: "Default", size: "sm" }) })] }, taskType));
                                            }) })] }))] }, agent.agentId));
                    }) }) }), presets.length > 0 && (_jsx("div", { style: {
                    padding: spacing[4],
                    borderTop: `1px solid ${colors.slate[100]}`,
                    backgroundColor: colors.slate[50],
                }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2], flexWrap: 'wrap' }, children: [_jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[600] }, children: "Quick Presets:" }), presets.map((preset) => (_jsx("button", { onClick: () => onPresetApply?.(preset.id), style: {
                                padding: `${spacing[1.5]}px ${spacing[3]}px`,
                                fontSize: fontSize.xs,
                                fontWeight: fontWeight.medium,
                                borderRadius: borderRadius.full,
                                backgroundColor: '#fff',
                                color: colors.slate[600],
                                border: `1px solid ${colors.slate[200]}`,
                                cursor: 'pointer',
                                transition: transitions.fast,
                            }, children: preset.name }, preset.id)))] }) }))] }));
};
export default AgentModelConfigPanel;
//# sourceMappingURL=AgentModelConfig.js.map