import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * SettingsView - 设置页面
 * 个人资料表单、AI 模型选择、连接状态、界面偏好
 */
import { useState, useEffect } from 'react';
import { colors, spacing, borderRadius, fontSize, fontWeight, breakpoints, } from '../shared/tokens';
import { Card, CardContent, CardHeader } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Button } from '../shared/Button';
import { Input } from '../shared/Input';
import { Toggle } from '../shared/Toggle';
import { CustomSelect } from '../shared/CustomSelect';
import { Avatar } from '../shared/Avatar';
// Icons
const SettingsIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "3" }), _jsx("path", { d: "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" })] }));
const UserIcon = () => (_jsxs("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("path", { d: "M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" }), _jsx("circle", { cx: "12", cy: "7", r: "4" })] }));
const RefreshIcon = () => (_jsxs("svg", { width: "14", height: "14", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("polyline", { points: "23 4 23 10 17 10" }), _jsx("polyline", { points: "1 20 1 14 7 14" }), _jsx("path", { d: "M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" })] }));
// Demo data
const demoProfile = {
    name: 'John Doe',
    email: 'john.doe@example.com',
    role: 'Developer',
};
const demoConnections = [
    { name: 'Claude API', status: 'connected', lastSync: '2 minutes ago' },
    { name: 'GitHub', status: 'connected', lastSync: '5 minutes ago' },
    { name: 'Jira', status: 'disconnected' },
    { name: 'Slack', status: 'error', lastSync: 'Failed to connect' },
];
const demoPreferences = {
    darkMode: false,
    compactMode: false,
    showTokenCount: true,
    autoSave: true,
};
const modelOptions = [
    { value: 'claude-opus', label: 'Claude Opus 4.5' },
    { value: 'claude-sonnet', label: 'Claude Sonnet 4' },
    { value: 'claude-haiku', label: 'Claude Haiku 3.5' },
    { value: 'gpt-4o', label: 'GPT-4o' },
    { value: 'gemini-pro', label: 'Gemini Pro' },
];
// Status colors
const statusColors = {
    connected: { bg: colors.success.light, text: colors.success.dark, dot: colors.success.main },
    disconnected: { bg: colors.slate[100], text: colors.slate[500], dot: colors.slate[400] },
    error: { bg: colors.error.light, text: colors.error.dark, dot: colors.error.main },
};
// Section Component
const SettingsSection = ({ title, description, children }) => (_jsxs(Card, { style: { marginBottom: spacing[6] }, children: [_jsxs(CardHeader, { style: { borderBottom: `1px solid ${colors.slate[200]}` }, children: [_jsx("h3", { style: { fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: title }), description && (_jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[500], marginTop: spacing[1] }, children: description }))] }), _jsx(CardContent, { children: children })] }));
// Form Field Component
const FormField = ({ label, children }) => (_jsxs("div", { style: { marginBottom: spacing[4] }, children: [_jsx("label", { style: {
                display: 'block',
                fontSize: fontSize.sm,
                fontWeight: fontWeight.medium,
                color: colors.slate[700],
                marginBottom: spacing[2],
            }, children: label }), children] }));
// Toggle Row Component
const ToggleRow = ({ label, description, checked, onChange }) => (_jsxs("div", { style: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: `${spacing[3]}px 0`,
        borderBottom: `1px solid ${colors.slate[100]}`,
    }, children: [_jsxs("div", { children: [_jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }, children: label }), description && (_jsx("p", { style: { fontSize: fontSize.xs, color: colors.slate[500], marginTop: spacing[0.5] }, children: description }))] }), _jsx(Toggle, { checked: checked, onChange: onChange })] }));
export const SettingsView = ({ profile: initialProfile = demoProfile, connections = demoConnections, preferences: initialPreferences = demoPreferences, selectedModel: initialModel = 'claude-opus', onProfileChange, onModelChange, onPreferencesChange, onReconnect, className, style, }) => {
    const [profile, setProfile] = useState(initialProfile);
    const [preferences, setPreferences] = useState(initialPreferences);
    const [selectedModel, setSelectedModel] = useState(initialModel);
    const [isMobile, setIsMobile] = useState(false);
    useEffect(() => {
        const checkMobile = () => setIsMobile(window.innerWidth < breakpoints.md);
        checkMobile();
        window.addEventListener('resize', checkMobile);
        return () => window.removeEventListener('resize', checkMobile);
    }, []);
    const handleProfileChange = (field, value) => {
        const newProfile = { ...profile, [field]: value };
        setProfile(newProfile);
        onProfileChange?.(newProfile);
    };
    const handlePreferenceChange = (field, value) => {
        const newPreferences = { ...preferences, [field]: value };
        setPreferences(newPreferences);
        onPreferencesChange?.(newPreferences);
    };
    const handleModelChange = (value) => {
        setSelectedModel(value);
        onModelChange?.(value);
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
                                backgroundColor: colors.slate[100],
                                borderRadius: borderRadius.lg,
                                color: colors.slate[600],
                            }, children: _jsx(SettingsIcon, {}) }), _jsxs("div", { children: [_jsx("h1", { style: { fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }, children: "Settings" }), _jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: "Manage your account and preferences" })] })] }) }), _jsxs("div", { style: {
                    flex: 1,
                    overflowY: 'auto',
                    padding: spacing[6],
                    maxWidth: 800,
                    margin: '0 auto',
                    width: '100%',
                }, children: [_jsxs(SettingsSection, { title: "Profile", description: "Your personal information", children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[4], marginBottom: spacing[6] }, children: [_jsx(Avatar, { name: profile.name, size: "lg" }), _jsxs("div", { children: [_jsx("h4", { style: { fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }, children: profile.name }), _jsx("p", { style: { fontSize: fontSize.sm, color: colors.slate[500] }, children: profile.role })] })] }), _jsxs("div", { style: { display: 'grid', gridTemplateColumns: isMobile ? '1fr' : '1fr 1fr', gap: spacing[4] }, children: [_jsx(FormField, { label: "Name", children: _jsx(Input, { value: profile.name, onChange: (e) => handleProfileChange('name', e.target.value), placeholder: "Your name" }) }), _jsx(FormField, { label: "Email", children: _jsx(Input, { type: "email", value: profile.email, onChange: (e) => handleProfileChange('email', e.target.value), placeholder: "your@email.com" }) })] })] }), _jsx(SettingsSection, { title: "AI Model", description: "Select your preferred AI model", children: _jsx(FormField, { label: "Default Model", children: _jsx(CustomSelect, { options: modelOptions, value: selectedModel, onChange: handleModelChange, placeholder: "Select a model" }) }) }), _jsx(SettingsSection, { title: "Connections", description: "Manage your integrations", children: _jsx("div", { style: { display: 'flex', flexDirection: 'column', gap: spacing[3] }, children: connections.map((connection) => {
                                const statusStyle = statusColors[connection.status];
                                return (_jsxs("div", { style: {
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'space-between',
                                        padding: spacing[3],
                                        backgroundColor: colors.slate[50],
                                        borderRadius: borderRadius.lg,
                                    }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [_jsx("div", { style: {
                                                        width: 8,
                                                        height: 8,
                                                        borderRadius: borderRadius.full,
                                                        backgroundColor: statusStyle.dot,
                                                    } }), _jsxs("div", { children: [_jsx("span", { style: { fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }, children: connection.name }), connection.lastSync && (_jsx("p", { style: { fontSize: fontSize.xs, color: colors.slate[400] }, children: connection.lastSync }))] })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[2] }, children: [_jsx(Badge, { style: { backgroundColor: statusStyle.bg, color: statusStyle.text }, children: connection.status }), connection.status !== 'connected' && (_jsx(Button, { variant: "ghost", size: "sm", onClick: () => onReconnect?.(connection.name), style: { padding: spacing[1] }, children: _jsx(RefreshIcon, {}) }))] })] }, connection.name));
                            }) }) }), _jsxs(SettingsSection, { title: "Interface", description: "Customize your experience", children: [_jsx(ToggleRow, { label: "Dark Mode", description: "Use dark theme across the application", checked: preferences.darkMode, onChange: (checked) => handlePreferenceChange('darkMode', checked) }), _jsx(ToggleRow, { label: "Compact Mode", description: "Reduce spacing for more content", checked: preferences.compactMode, onChange: (checked) => handlePreferenceChange('compactMode', checked) }), _jsx(ToggleRow, { label: "Show Token Count", description: "Display token usage in conversations", checked: preferences.showTokenCount, onChange: (checked) => handlePreferenceChange('showTokenCount', checked) }), _jsx(ToggleRow, { label: "Auto Save", description: "Automatically save changes", checked: preferences.autoSave, onChange: (checked) => handlePreferenceChange('autoSave', checked) })] }), _jsx("div", { style: { display: 'flex', justifyContent: 'flex-end' }, children: _jsx(Button, { variant: "primary", children: "Save Changes" }) })] })] }));
};
export default SettingsView;
//# sourceMappingURL=SettingsView.js.map