import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * ParallelModelConfig - 并行模式模型配置
 * P028: 实现并行模式中各 Worker 的模型配置 UI
 */
import { useState } from 'react';
import { PROVIDER_INFO, formatCost, estimateCost, } from './types';
import { colors, borderRadius, fontSize, fontWeight, spacing, transitions } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Badge } from '../shared/Badge';
import { Tooltip } from '../shared/Tooltip';
// 预设组合
const PRESET_COMBINATIONS = [
    { id: 'diverse', name: 'Diverse', description: 'Mix of different providers', models: ['claude-3-opus', 'gpt-4o', 'gemini-1.5-pro'] },
    { id: 'claude-team', name: 'Claude Team', description: 'All Claude models', models: ['claude-3-opus', 'claude-3-sonnet', 'claude-3-haiku'] },
    { id: 'cost-effective', name: 'Cost Effective', description: 'Budget-friendly options', models: ['claude-3-haiku', 'gpt-4o-mini', 'gemini-1.5-flash'] },
    { id: 'high-quality', name: 'High Quality', description: 'Best models only', models: ['claude-3-opus', 'gpt-4o', 'gemini-1.5-pro'] },
];
export const ParallelModelConfigPanel = ({ workers, models, presets = [], maxWorkers = 5, estimatedTokensPerWorker = 10000, onWorkerModelChange, onWorkerAdd, onWorkerRemove, onPresetApply, onPresetSave, className, style, }) => {
    const [draggedIndex, setDraggedIndex] = useState(null);
    const modelOptions = models.map((m) => ({
        value: m.id,
        label: m.name,
        description: `${PROVIDER_INFO[m.provider]?.name || m.provider} • ${formatCost(m.costPerInputToken + m.costPerOutputToken)}`,
        icon: _jsx("span", { children: PROVIDER_INFO[m.provider]?.icon || '🤖' }),
        color: PROVIDER_INFO[m.provider]?.color,
    }));
    // 计算总成本
    const totalEstimatedCost = workers.reduce((acc, worker) => {
        const model = models.find((m) => m.id === worker.modelId);
        if (model) {
            return acc + estimateCost(estimatedTokensPerWorker, estimatedTokensPerWorker * 0.5, model);
        }
        return acc;
    }, 0);
    // 按提供商分组统计
    const providerStats = workers.reduce((acc, worker) => {
        const model = models.find((m) => m.id === worker.modelId);
        if (model) {
            acc[model.provider] = (acc[model.provider] || 0) + 1;
        }
        return acc;
    }, {});
    const handleDragStart = (index) => {
        setDraggedIndex(index);
    };
    const handleDragOver = (e, index) => {
        e.preventDefault();
    };
    const handleDrop = (e, targetIndex) => {
        e.preventDefault();
        // Reorder logic would go here
        setDraggedIndex(null);
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
                                    background: `linear-gradient(135deg, ${colors.indigo[500]}, ${colors.primary[500]})`,
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 18,
                                    color: '#fff',
                                }, children: "\uD83D\uDD00" }), _jsxs("div", { children: [_jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }, children: "Parallel Worker Configuration" }), _jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Configure models for parallel execution" })] })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: { display: 'flex', gap: spacing[1] }, children: Object.entries(providerStats).map(([provider, count]) => (_jsx(Tooltip, { content: `${PROVIDER_INFO[provider]?.name || provider}: ${count} workers`, children: _jsxs("div", { style: {
                                            display: 'flex',
                                            alignItems: 'center',
                                            gap: 4,
                                            padding: `${spacing[0.5]}px ${spacing[1.5]}px`,
                                            backgroundColor: `${PROVIDER_INFO[provider]?.color || colors.slate[500]}15`,
                                            borderRadius: borderRadius.full,
                                            fontSize: fontSize.xs,
                                        }, children: [_jsx("span", { children: PROVIDER_INFO[provider]?.icon }), _jsx("span", { style: { fontWeight: fontWeight.bold, color: PROVIDER_INFO[provider]?.color }, children: count })] }) }, provider))) }), _jsxs(Badge, { variant: "info", size: "md", children: ["\uD83D\uDCB0 ~$", totalEstimatedCost.toFixed(4)] })] })] }), _jsx("div", { style: {
                    padding: spacing[3],
                    borderBottom: `1px solid ${colors.slate[100]}`,
                    backgroundColor: colors.slate[50],
                }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2], flexWrap: 'wrap' }, children: [_jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[600] }, children: "Quick Setup:" }), PRESET_COMBINATIONS.map((preset) => (_jsx(Tooltip, { content: preset.description, children: _jsx("button", { onClick: () => onPresetApply?.(preset.id), style: {
                                    padding: `${spacing[1.5]}px ${spacing[3]}px`,
                                    fontSize: fontSize.xs,
                                    fontWeight: fontWeight.medium,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: '#fff',
                                    color: colors.slate[600],
                                    border: `1px solid ${colors.slate[200]}`,
                                    cursor: 'pointer',
                                    transition: transitions.fast,
                                }, children: preset.name }) }, preset.id)))] }) }), _jsx("div", { style: { padding: spacing[4] }, children: _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: [workers.map((worker, index) => {
                            const selectedModel = models.find((m) => m.id === worker.modelId);
                            const providerInfo = selectedModel ? PROVIDER_INFO[selectedModel.provider] : null;
                            return (_jsxs("div", { draggable: true, onDragStart: () => handleDragStart(index), onDragOver: (e) => handleDragOver(e, index), onDrop: (e) => handleDrop(e, index), style: {
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: spacing[3],
                                    padding: spacing[3],
                                    backgroundColor: draggedIndex === index ? colors.primary[50] : colors.slate[50],
                                    borderRadius: borderRadius.xl,
                                    border: `1px solid ${draggedIndex === index ? colors.primary[300] : colors.slate[200]}`,
                                    cursor: 'grab',
                                    transition: transitions.fast,
                                }, children: [_jsx("div", { style: { color: colors.slate[400], cursor: 'grab' }, children: "\u22EE\u22EE" }), _jsx("div", { style: {
                                            width: 32,
                                            height: 32,
                                            borderRadius: borderRadius.full,
                                            backgroundColor: providerInfo?.color || colors.slate[300],
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            color: '#fff',
                                            fontSize: fontSize.sm,
                                            fontWeight: fontWeight.bold,
                                        }, children: index + 1 }), _jsx("div", { style: { minWidth: 100 }, children: _jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: worker.workerName }) }), _jsx("div", { style: { flex: 1 }, children: _jsx(CustomSelect, { options: modelOptions, value: worker.modelId, onChange: (v) => onWorkerModelChange?.(worker.workerId, v), placeholder: "Select model...", size: "sm", searchable: true }) }), selectedModel && (_jsxs("div", { style: { textAlign: 'right', minWidth: 80 }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Est. Cost" }), _jsxs("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: ["$", estimateCost(estimatedTokensPerWorker, estimatedTokensPerWorker * 0.5, selectedModel).toFixed(4)] })] })), workers.length > 1 && (_jsx("button", { onClick: () => onWorkerRemove?.(worker.workerId), style: {
                                            width: 28,
                                            height: 28,
                                            borderRadius: borderRadius.full,
                                            backgroundColor: colors.slate[100],
                                            border: 'none',
                                            cursor: 'pointer',
                                            color: colors.slate[500],
                                            fontSize: 14,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            transition: transitions.fast,
                                        }, children: "\u00D7" }))] }, worker.workerId));
                        }), workers.length < maxWorkers && (_jsxs("button", { onClick: onWorkerAdd, style: {
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                gap: spacing[2],
                                padding: spacing[3],
                                backgroundColor: 'transparent',
                                border: `2px dashed ${colors.slate[200]}`,
                                borderRadius: borderRadius.xl,
                                cursor: 'pointer',
                                color: colors.slate[500],
                                fontSize: fontSize.sm,
                                fontWeight: fontWeight.medium,
                                transition: transitions.fast,
                            }, children: [_jsx("span", { style: { fontSize: 18 }, children: "+" }), "Add Worker (", workers.length, "/", maxWorkers, ")"] }))] }) }), _jsx("div", { style: {
                    padding: spacing[4],
                    borderTop: `1px solid ${colors.slate[100]}`,
                    backgroundColor: colors.slate[50],
                }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between' }, children: [_jsxs("div", { children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }, children: "Total Estimated Cost" }), _jsxs("div", { style: { fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: ["$", totalEstimatedCost.toFixed(4)] })] }), _jsxs("div", { style: { textAlign: 'right' }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }, children: "Per Worker Average" }), _jsxs("div", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: ["$", (totalEstimatedCost / workers.length).toFixed(4)] })] })] }) })] }));
};
export default ParallelModelConfigPanel;
//# sourceMappingURL=ParallelModelConfig.js.map