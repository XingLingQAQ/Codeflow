import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
/**
 * ModelSettingsPage - 全局模型设置页面
 * P029: 实现全局模型设置页面，集中管理所有模型配置
 */
import { useState } from 'react';
import { PROVIDER_INFO, formatCost, formatContextWindow, } from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Input } from '../shared/Input';
import { Toggle } from '../shared/Toggle';
import { Tabs, TabPanel } from '../shared/Tabs';
import { Modal } from '../shared/Modal';
export const ModelSettingsPage = ({ models, apiKeys = {}, defaultModelId, costLimit = 10, usageStats, onModelAdd, onModelRemove, onModelEdit, onApiKeyUpdate, onDefaultModelChange, onCostLimitChange, onExportConfig, onImportConfig, className, style, }) => {
    const [activeTab, setActiveTab] = useState('models');
    const [showAddModel, setShowAddModel] = useState(false);
    const [editingModelId, setEditingModelId] = useState(null);
    const [newModel, setNewModel] = useState({
        provider: 'anthropic',
        capabilities: [],
    });
    const tabs = [
        { id: 'models', label: 'Models', icon: _jsx("span", { children: "\uD83E\uDD16" }), badge: models.length },
        { id: 'api-keys', label: 'API Keys', icon: _jsx("span", { children: "\uD83D\uDD11" }) },
        { id: 'limits', label: 'Limits', icon: _jsx("span", { children: "\uD83D\uDCB0" }) },
        { id: 'usage', label: 'Usage', icon: _jsx("span", { children: "\uD83D\uDCCA" }) },
    ];
    const providerOptions = Object.entries(PROVIDER_INFO).map(([key, info]) => ({
        value: key,
        label: info.name,
        icon: _jsx("span", { children: info.icon }),
        color: info.color,
    }));
    const capabilityOptions = [
        { value: 'coding', label: 'Coding' },
        { value: 'reasoning', label: 'Reasoning' },
        { value: 'frontend', label: 'Frontend' },
        { value: 'backend', label: 'Backend' },
        { value: 'analysis', label: 'Analysis' },
        { value: 'creative', label: 'Creative' },
    ];
    const handleAddModel = () => {
        if (newModel.name && newModel.provider) {
            onModelAdd?.({
                name: newModel.name,
                provider: newModel.provider,
                capabilities: newModel.capabilities || [],
                costPerInputToken: newModel.costPerInputToken || 0,
                costPerOutputToken: newModel.costPerOutputToken || 0,
                contextWindow: newModel.contextWindow || 128000,
                description: newModel.description,
            });
            setNewModel({ provider: 'anthropic', capabilities: [] });
            setShowAddModel(false);
        }
    };
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: colors.slate[50],
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: `${spacing[6]}px ${spacing[8]}px`,
                    backgroundColor: '#fff',
                    borderBottom: `1px solid ${colors.slate[200]}`,
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[4] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[4] }, children: [_jsx("div", { style: {
                                            width: 48,
                                            height: 48,
                                            borderRadius: borderRadius['2xl'],
                                            background: `linear-gradient(135deg, ${colors.slate[800]}, ${colors.slate[600]})`,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            fontSize: 24,
                                            color: '#fff',
                                            boxShadow: shadows.lg,
                                        }, children: "\u2699\uFE0F" }), _jsxs("div", { children: [_jsx("h1", { style: { fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[900], margin: 0 }, children: "Model Settings" }), _jsx("span", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: "Manage models, API keys, and usage limits" })] })] }), _jsxs("div", { style: { display: 'flex', gap: spacing[2] }, children: [_jsx(Button, { variant: "secondary", onClick: onExportConfig, children: "\uD83D\uDCE4 Export" }), _jsx(Button, { variant: "secondary", onClick: () => { }, children: "\uD83D\uDCE5 Import" })] })] }), _jsx(Tabs, { tabs: tabs, activeTab: activeTab, onChange: setActiveTab, variant: "underline" })] }), _jsx("div", { style: { flex: 1, overflow: 'auto', padding: spacing[6] }, children: _jsxs("div", { style: { maxWidth: 1200, margin: '0 auto' }, children: [_jsxs(TabPanel, { tabId: "models", activeTab: activeTab, style: { padding: 0 }, children: [_jsxs("div", { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: spacing[4] }, children: [_jsx("h2", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }, children: "Available Models" }), _jsx(Button, { onClick: () => setShowAddModel(true), children: "+ Add Model" })] }), _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: spacing[4] }, children: models.map((model) => {
                                        const providerInfo = PROVIDER_INFO[model.provider];
                                        const isDefault = model.id === defaultModelId;
                                        return (_jsxs(Card, { padding: "md", hoverable: true, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: spacing[3] }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: {
                                                                        width: 40,
                                                                        height: 40,
                                                                        borderRadius: borderRadius.lg,
                                                                        backgroundColor: `${providerInfo?.color || colors.slate[500]}15`,
                                                                        display: 'flex',
                                                                        alignItems: 'center',
                                                                        justifyContent: 'center',
                                                                        fontSize: 20,
                                                                    }, children: providerInfo?.icon || '🤖' }), _jsxs("div", { children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx("span", { style: { fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: model.name }), isDefault && _jsx(Badge, { size: "sm", variant: "primary", children: "Default" })] }), _jsx("span", { style: { fontSize: fontSize.xs, color: providerInfo?.color || colors.slate[500] }, children: providerInfo?.name || model.provider })] })] }), _jsx("button", { onClick: () => setEditingModelId(model.id), style: {
                                                                padding: spacing[1],
                                                                backgroundColor: 'transparent',
                                                                border: 'none',
                                                                cursor: 'pointer',
                                                                color: colors.slate[400],
                                                            }, children: "\u22EE" })] }), model.description && (_jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[600], marginBottom: spacing[3], lineHeight: 1.5 }, children: model.description })), _jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: spacing[1], marginBottom: spacing[3] }, children: model.capabilities.map((cap) => (_jsx(Badge, { size: "sm", variant: "default", children: cap }, cap))) }), _jsxs("div", { style: { display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: spacing[2] }, children: [_jsxs("div", { style: { padding: spacing[2], backgroundColor: colors.slate[50], borderRadius: borderRadius.md }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Input" }), _jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: formatCost(model.costPerInputToken) })] }), _jsxs("div", { style: { padding: spacing[2], backgroundColor: colors.slate[50], borderRadius: borderRadius.md }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Output" }), _jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: formatCost(model.costPerOutputToken) })] }), _jsxs("div", { style: { padding: spacing[2], backgroundColor: colors.slate[50], borderRadius: borderRadius.md }, children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500] }, children: "Context" }), _jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }, children: formatContextWindow(model.contextWindow) })] })] }), !isDefault && (_jsx(Button, { variant: "ghost", size: "sm", fullWidth: true, onClick: () => onDefaultModelChange?.(model.id), style: { marginTop: spacing[3] }, children: "Set as Default" }))] }, model.id));
                                    }) })] }), _jsxs(TabPanel, { tabId: "api-keys", activeTab: activeTab, style: { padding: 0 }, children: [_jsx("h2", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }, children: "API Key Management" }), _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[4] }, children: Object.entries(PROVIDER_INFO).map(([provider, info]) => {
                                        const keyInfo = apiKeys[provider];
                                        return (_jsx(Card, { padding: "md", children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[4] }, children: [_jsx("div", { style: {
                                                            width: 48,
                                                            height: 48,
                                                            borderRadius: borderRadius.xl,
                                                            backgroundColor: `${info.color}15`,
                                                            display: 'flex',
                                                            alignItems: 'center',
                                                            justifyContent: 'center',
                                                            fontSize: 24,
                                                        }, children: info.icon }), _jsxs("div", { style: { flex: 1 }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2], marginBottom: spacing[1] }, children: [_jsx("span", { style: { fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: info.name }), keyInfo?.isValid ? (_jsx(Badge, { size: "sm", variant: "success", children: "Connected" })) : keyInfo?.key ? (_jsx(Badge, { size: "sm", variant: "error", children: "Invalid" })) : (_jsx(Badge, { size: "sm", variant: "default", children: "Not Set" }))] }), _jsx(Input, { type: "password", placeholder: `Enter ${info.name} API key...`, value: keyInfo?.key || '', onChange: (e) => onApiKeyUpdate?.(provider, e.target.value), size: "sm", fullWidth: true })] })] }) }, provider));
                                    }) }), _jsx("div", { style: {
                                        marginTop: spacing[4],
                                        padding: spacing[4],
                                        backgroundColor: colors.warning.light,
                                        borderRadius: borderRadius.xl,
                                        border: `1px solid #fcd34d`,
                                    }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'flex-start', gap: spacing[3] }, children: [_jsx("span", { style: { fontSize: 20 }, children: "\u26A0\uFE0F" }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[1] }, children: "Security Notice" }), _jsx("div", { style: { fontSize: fontSize.sm, color: colors.slate[600] }, children: "API keys are stored securely and encrypted. Never share your API keys with others." })] })] }) })] }), _jsxs(TabPanel, { tabId: "limits", activeTab: activeTab, style: { padding: 0 }, children: [_jsx("h2", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }, children: "Cost Limits & Alerts" }), _jsxs(Card, { padding: "lg", children: [_jsxs("div", { style: { marginBottom: spacing[6] }, children: [_jsx("label", { style: { display: 'block', fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }, children: "Session Cost Limit" }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx(Input, { type: "number", value: costLimit, onChange: (e) => onCostLimitChange?.(parseFloat(e.target.value)), size: "md", style: { width: 120 } }), _jsx("span", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: "USD per session" })] }), _jsx("p", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginTop: spacing[2] }, children: "You'll receive an alert when approaching this limit" })] }), _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: [_jsx(Toggle, { checked: true, label: "Alert at 70% of budget", size: "md" }), _jsx(Toggle, { checked: true, label: "Alert at 90% of budget", size: "md" }), _jsx(Toggle, { checked: false, label: "Auto-pause at 100% of budget", size: "md" })] })] })] }), _jsxs(TabPanel, { tabId: "usage", activeTab: activeTab, style: { padding: 0 }, children: [_jsx("h2", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }, children: "Usage Statistics" }), usageStats ? (_jsxs(_Fragment, { children: [_jsxs("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: spacing[4], marginBottom: spacing[6] }, children: [_jsxs(Card, { padding: "md", children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }, children: "Total Cost" }), _jsxs("div", { style: { fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[800] }, children: ["$", usageStats.totalCost.toFixed(4)] })] }), _jsxs(Card, { padding: "md", children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }, children: "Total Tokens" }), _jsx("div", { style: { fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[800] }, children: formatContextWindow(usageStats.totalTokens) })] }), _jsxs(Card, { padding: "md", children: [_jsx("div", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }, children: "Models Used" }), _jsx("div", { style: { fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[800] }, children: Object.keys(usageStats.byModel).length })] })] }), _jsxs(Card, { padding: "md", children: [_jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }, children: "Usage by Model" }), _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: Object.entries(usageStats.byModel).map(([modelId, stats]) => {
                                                        const model = models.find((m) => m.id === modelId);
                                                        const percentage = (stats.cost / usageStats.totalCost) * 100;
                                                        return (_jsxs("div", { children: [_jsxs("div", { style: { display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }, children: [_jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }, children: model?.name || modelId }), _jsxs("span", { style: { fontSize: fontSize.sm, color: colors.slate[600] }, children: ["$", stats.cost.toFixed(4), " (", percentage.toFixed(1), "%)"] })] }), _jsx("div", { style: {
                                                                        height: 8,
                                                                        borderRadius: borderRadius.full,
                                                                        backgroundColor: colors.slate[100],
                                                                        overflow: 'hidden',
                                                                    }, children: _jsx("div", { style: {
                                                                            width: `${percentage}%`,
                                                                            height: '100%',
                                                                            backgroundColor: PROVIDER_INFO[model?.provider || '']?.color || colors.primary[500],
                                                                            borderRadius: borderRadius.full,
                                                                        } }) })] }, modelId));
                                                    }) })] })] })) : (_jsxs(Card, { padding: "lg", style: { textAlign: 'center' }, children: [_jsx("div", { style: { fontSize: 48, marginBottom: spacing[4] }, children: "\uD83D\uDCCA" }), _jsx("div", { style: { fontSize: fontSize.lg, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }, children: "No Usage Data Yet" }), _jsx("div", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: "Start using models to see usage statistics here" })] }))] })] }) }), _jsx(Modal, { isOpen: showAddModel, onClose: () => setShowAddModel(false), title: "Add Custom Model", icon: _jsx("span", { children: "\uD83E\uDD16" }), size: "md", footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "secondary", onClick: () => setShowAddModel(false), children: "Cancel" }), _jsx(Button, { onClick: handleAddModel, children: "Add Model" })] }), children: _jsxs("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[4] }, children: [_jsx(Input, { label: "Model Name", placeholder: "e.g., claude-3-opus", value: newModel.name || '', onChange: (e) => setNewModel({ ...newModel, name: e.target.value }), fullWidth: true }), _jsx(CustomSelect, { label: "Provider", options: providerOptions, value: newModel.provider || '', onChange: (v) => setNewModel({ ...newModel, provider: v }) }), _jsxs("div", { style: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: spacing[3] }, children: [_jsx(Input, { label: "Input Cost ($/1K tokens)", type: "number", step: "0.0001", value: newModel.costPerInputToken || '', onChange: (e) => setNewModel({ ...newModel, costPerInputToken: parseFloat(e.target.value) }) }), _jsx(Input, { label: "Output Cost ($/1K tokens)", type: "number", step: "0.0001", value: newModel.costPerOutputToken || '', onChange: (e) => setNewModel({ ...newModel, costPerOutputToken: parseFloat(e.target.value) }) })] }), _jsx(Input, { label: "Context Window", type: "number", value: newModel.contextWindow || '', onChange: (e) => setNewModel({ ...newModel, contextWindow: parseInt(e.target.value) }), fullWidth: true }), _jsx(Input, { label: "Description (optional)", placeholder: "Brief description of the model...", value: newModel.description || '', onChange: (e) => setNewModel({ ...newModel, description: e.target.value }), fullWidth: true })] }) })] }));
};
export default ModelSettingsPage;
//# sourceMappingURL=ModelSettingsPage.js.map