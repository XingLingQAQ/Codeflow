import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * PhaseModelConfig - 阶段模型配置面板
 * P026: 实现 Plan 模式各阶段的模型配置 UI
 */
import { useState } from 'react';
import { PHASE_INFO, PROVIDER_INFO, formatContextWindow, estimateCost, } from './types';
import { colors, borderRadius, fontSize, fontWeight, spacing, transitions } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Badge } from '../shared/Badge';
import { Tooltip } from '../shared/Tooltip';
export const PhaseModelConfigPanel = ({ phases, models, presets = [], estimatedTokens = {}, onPhaseModelChange, onPresetApply, onPresetSave, onResetToDefault, className, style, }) => {
    const [showSavePreset, setShowSavePreset] = useState(false);
    const [presetName, setPresetName] = useState('');
    const modelOptions = models.map((m) => ({
        value: m.id,
        label: m.name,
        description: `${PROVIDER_INFO[m.provider]?.name || m.provider} • ${formatContextWindow(m.contextWindow)} context`,
        icon: _jsx("span", { children: PROVIDER_INFO[m.provider]?.icon || '🤖' }),
        color: PROVIDER_INFO[m.provider]?.color,
    }));
    const totalEstimatedCost = phases.reduce((acc, phase) => {
        const model = models.find((m) => m.id === phase.modelId);
        const tokens = estimatedTokens[phase.phaseId] || { input: 0, output: 0 };
        if (model) {
            return acc + estimateCost(tokens.input, tokens.output, model);
        }
        return acc;
    }, 0);
    const handleSavePreset = () => {
        if (presetName.trim()) {
            onPresetSave?.(presetName.trim());
            setPresetName('');
            setShowSavePreset(false);
        }
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
                                    backgroundColor: colors.primary[50],
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 18,
                                }, children: "\uD83D\uDCCB" }), _jsxs("div", { children: [_jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }, children: "Phase Model Configuration" }), _jsx("span", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Configure models for each plan phase" })] })] }), _jsx("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: _jsx(Tooltip, { content: "Estimated total cost", children: _jsxs(Badge, { variant: "info", size: "md", children: ["\uD83D\uDCB0 ~$", totalEstimatedCost.toFixed(4)] }) }) })] }), presets.length > 0 && (_jsxs("div", { style: {
                    padding: spacing[3],
                    borderBottom: `1px solid ${colors.slate[100]}`,
                    backgroundColor: colors.slate[50],
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2], flexWrap: 'wrap' }, children: [_jsx("span", { style: { fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[600] }, children: "Presets:" }), presets.map((preset) => (_jsx("button", { onClick: () => onPresetApply?.(preset.id), style: {
                                    padding: `${spacing[1]}px ${spacing[2.5]}px`,
                                    fontSize: fontSize.xs,
                                    fontWeight: fontWeight.medium,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: preset.isDefault ? colors.primary[50] : '#fff',
                                    color: preset.isDefault ? colors.primary[700] : colors.slate[600],
                                    border: `1px solid ${preset.isDefault ? colors.primary[200] : colors.slate[200]}`,
                                    cursor: 'pointer',
                                    transition: transitions.fast,
                                }, children: preset.name }, preset.id))), _jsx("button", { onClick: () => setShowSavePreset(!showSavePreset), style: {
                                    padding: `${spacing[1]}px ${spacing[2.5]}px`,
                                    fontSize: fontSize.xs,
                                    fontWeight: fontWeight.medium,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: 'transparent',
                                    color: colors.primary[600],
                                    border: `1px dashed ${colors.primary[300]}`,
                                    cursor: 'pointer',
                                }, children: "+ Save Current" })] }), showSavePreset && (_jsxs("div", { style: { display: 'flex', gap: spacing[2], marginTop: spacing[2] }, children: [_jsx("input", { type: "text", value: presetName, onChange: (e) => setPresetName(e.target.value), placeholder: "Preset name...", style: {
                                    flex: 1,
                                    padding: `${spacing[1.5]}px ${spacing[3]}px`,
                                    fontSize: fontSize.sm,
                                    borderRadius: borderRadius.lg,
                                    border: `1px solid ${colors.slate[200]}`,
                                    outline: 'none',
                                } }), _jsx(Button, { size: "sm", onClick: handleSavePreset, children: "Save" }), _jsx(Button, { size: "sm", variant: "ghost", onClick: () => setShowSavePreset(false), children: "Cancel" })] }))] })), _jsx("div", { style: { padding: spacing[4] }, children: _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: phases.map((phase) => {
                        const phaseInfo = PHASE_INFO[phase.phaseId] || { icon: '📌', color: colors.slate[500], description: '' };
                        const selectedModel = models.find((m) => m.id === phase.modelId);
                        const tokens = estimatedTokens[phase.phaseId] || { input: 0, output: 0 };
                        const phaseCost = selectedModel ? estimateCost(tokens.input, tokens.output, selectedModel) : 0;
                        return (_jsxs("div", { style: {
                                display: 'flex',
                                alignItems: 'center',
                                gap: spacing[4],
                                padding: spacing[3],
                                backgroundColor: colors.slate[50],
                                borderRadius: borderRadius.xl,
                                border: `1px solid ${colors.slate[100]}`,
                            }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3], minWidth: 180 }, children: [_jsx("div", { style: {
                                                width: 40,
                                                height: 40,
                                                borderRadius: borderRadius.lg,
                                                backgroundColor: `${phaseInfo.color}15`,
                                                display: 'flex',
                                                alignItems: 'center',
                                                justifyContent: 'center',
                                                fontSize: 20,
                                            }, children: phaseInfo.icon }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: phase.phaseName }), _jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: phaseInfo.description })] })] }), _jsx("div", { style: { flex: 1 }, children: _jsx(CustomSelect, { options: modelOptions, value: phase.modelId, onChange: (v) => onPhaseModelChange?.(phase.phaseId, v), placeholder: "Select model...", size: "sm", searchable: true }) }), _jsxs("div", { style: { textAlign: 'right', minWidth: 80 }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Est. Cost" }), _jsxs("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: ["$", phaseCost.toFixed(4)] })] })] }, phase.phaseId));
                    }) }) }), _jsx("div", { style: {
                    padding: spacing[4],
                    borderTop: `1px solid ${colors.slate[100]}`,
                    display: 'flex',
                    justifyContent: 'flex-end',
                    gap: spacing[2],
                }, children: _jsx(Button, { variant: "ghost", onClick: onResetToDefault, children: "Reset to Default" }) })] }));
};
export default PhaseModelConfigPanel;
//# sourceMappingURL=PhaseModelConfig.js.map