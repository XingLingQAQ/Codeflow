/**
 * SettingsView - 设置页面
 * 个人资料表单、AI 模型选择、连接状态、界面偏好
 */
import React from 'react';
export interface UserProfile {
    name: string;
    email: string;
    avatar?: string;
    role: string;
}
export interface ConnectionStatus {
    name: string;
    status: 'connected' | 'disconnected' | 'error';
    lastSync?: string;
}
export interface InterfacePreferences {
    darkMode: boolean;
    compactMode: boolean;
    showTokenCount: boolean;
    autoSave: boolean;
}
export interface SettingsViewProps {
    profile?: UserProfile;
    connections?: ConnectionStatus[];
    preferences?: InterfacePreferences;
    selectedModel?: string;
    onProfileChange?: (profile: UserProfile) => void;
    onModelChange?: (model: string) => void;
    onPreferencesChange?: (preferences: InterfacePreferences) => void;
    onReconnect?: (connectionName: string) => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const SettingsView: React.FC<SettingsViewProps>;
export default SettingsView;
//# sourceMappingURL=SettingsView.d.ts.map