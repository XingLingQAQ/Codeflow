/**
 * SettingsView - 设置页面
 * 个人资料表单、AI 模型选择、连接状态、界面偏好
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
import { Toggle } from '../shared/Toggle';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Avatar } from '../shared/Avatar';

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

// Icons
const SettingsIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </svg>
);

const UserIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
    <circle cx="12" cy="7" r="4" />
  </svg>
);

const RefreshIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="23 4 23 10 17 10" />
    <polyline points="1 20 1 14 7 14" />
    <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
  </svg>
);

// Demo data
const demoProfile: UserProfile = {
  name: 'John Doe',
  email: 'john.doe@example.com',
  role: 'Developer',
};

const demoConnections: ConnectionStatus[] = [
  { name: 'Claude API', status: 'connected', lastSync: '2 minutes ago' },
  { name: 'GitHub', status: 'connected', lastSync: '5 minutes ago' },
  { name: 'Jira', status: 'disconnected' },
  { name: 'Slack', status: 'error', lastSync: 'Failed to connect' },
];

const demoPreferences: InterfacePreferences = {
  darkMode: false,
  compactMode: false,
  showTokenCount: true,
  autoSave: true,
};

const modelOptions: SelectOption[] = [
  { value: 'claude-opus', label: 'Claude Opus 4.5' },
  { value: 'claude-sonnet', label: 'Claude Sonnet 4' },
  { value: 'claude-haiku', label: 'Claude Haiku 3.5' },
  { value: 'gpt-4o', label: 'GPT-4o' },
  { value: 'gemini-pro', label: 'Gemini Pro' },
];

// Status colors
const statusColors: Record<string, { bg: string; text: string; dot: string }> = {
  connected: { bg: colors.success.light, text: colors.success.dark, dot: colors.success.main },
  disconnected: { bg: colors.slate[100], text: colors.slate[500], dot: colors.slate[400] },
  error: { bg: colors.error.light, text: colors.error.dark, dot: colors.error.main },
};

// Section Component
const SettingsSection: React.FC<{
  title: string;
  description?: string;
  children: React.ReactNode;
}> = ({ title, description, children }) => (
  <Card style={{ marginBottom: spacing[6] }}>
    <CardHeader style={{ borderBottom: `1px solid ${colors.slate[200]}` }}>
      <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
        {title}
      </h3>
      {description && (
        <p style={{ fontSize: fontSize.sm, color: colors.slate[500], marginTop: spacing[1] }}>
          {description}
        </p>
      )}
    </CardHeader>
    <CardContent>{children}</CardContent>
  </Card>
);

// Form Field Component
const FormField: React.FC<{
  label: string;
  children: React.ReactNode;
}> = ({ label, children }) => (
  <div style={{ marginBottom: spacing[4] }}>
    <label
      style={{
        display: 'block',
        fontSize: fontSize.sm,
        fontWeight: fontWeight.medium,
        color: colors.slate[700],
        marginBottom: spacing[2],
      }}
    >
      {label}
    </label>
    {children}
  </div>
);

// Toggle Row Component
const ToggleRow: React.FC<{
  label: string;
  description?: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}> = ({ label, description, checked, onChange }) => (
  <div
    style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'space-between',
      padding: `${spacing[3]}px 0`,
      borderBottom: `1px solid ${colors.slate[100]}`,
    }}
  >
    <div>
      <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
        {label}
      </span>
      {description && (
        <p style={{ fontSize: fontSize.xs, color: colors.slate[500], marginTop: spacing[0.5] }}>
          {description}
        </p>
      )}
    </div>
    <Toggle checked={checked} onChange={onChange} />
  </div>
);

export const SettingsView: React.FC<SettingsViewProps> = ({
  profile: initialProfile = demoProfile,
  connections = demoConnections,
  preferences: initialPreferences = demoPreferences,
  selectedModel: initialModel = 'claude-opus',
  onProfileChange,
  onModelChange,
  onPreferencesChange,
  onReconnect,
  className,
  style,
}) => {
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

  const handleProfileChange = (field: keyof UserProfile, value: string) => {
    const newProfile = { ...profile, [field]: value };
    setProfile(newProfile);
    onProfileChange?.(newProfile);
  };

  const handlePreferenceChange = (field: keyof InterfacePreferences, value: boolean) => {
    const newPreferences = { ...preferences, [field]: value };
    setPreferences(newPreferences);
    onPreferencesChange?.(newPreferences);
  };

  const handleModelChange = (value: string) => {
    setSelectedModel(value);
    onModelChange?.(value);
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
              backgroundColor: colors.slate[100],
              borderRadius: borderRadius.lg,
              color: colors.slate[600],
            }}
          >
            <SettingsIcon />
          </div>
          <div>
            <h1 style={{ fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
              Settings
            </h1>
            <p style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
              Manage your account and preferences
            </p>
          </div>
        </div>
      </div>

      {/* Content */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: spacing[6],
          maxWidth: 800,
          margin: '0 auto',
          width: '100%',
        }}
      >
        {/* Profile Section */}
        <SettingsSection title="Profile" description="Your personal information">
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[4], marginBottom: spacing[6] }}>
            <Avatar name={profile.name} size="lg" />
            <div>
              <h4 style={{ fontSize: fontSize.base, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
                {profile.name}
              </h4>
              <p style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>{profile.role}</p>
            </div>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: isMobile ? '1fr' : '1fr 1fr', gap: spacing[4] }}>
            <FormField label="Name">
              <Input
                value={profile.name}
                onChange={(e) => handleProfileChange('name', e.target.value)}
                placeholder="Your name"
              />
            </FormField>
            <FormField label="Email">
              <Input
                type="email"
                value={profile.email}
                onChange={(e) => handleProfileChange('email', e.target.value)}
                placeholder="your@email.com"
              />
            </FormField>
          </div>
        </SettingsSection>

        {/* AI Model Section */}
        <SettingsSection title="AI Model" description="Select your preferred AI model">
          <FormField label="Default Model">
            <CustomSelect
              options={modelOptions}
              value={selectedModel}
              onChange={handleModelChange}
              placeholder="Select a model"
            />
          </FormField>
        </SettingsSection>

        {/* Connections Section */}
        <SettingsSection title="Connections" description="Manage your integrations">
          <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
            {connections.map((connection) => {
              const statusStyle = statusColors[connection.status];
              return (
                <div
                  key={connection.name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: spacing[3],
                    backgroundColor: colors.slate[50],
                    borderRadius: borderRadius.lg,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
                    <div
                      style={{
                        width: 8,
                        height: 8,
                        borderRadius: borderRadius.full,
                        backgroundColor: statusStyle.dot,
                      }}
                    />
                    <div>
                      <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
                        {connection.name}
                      </span>
                      {connection.lastSync && (
                        <p style={{ fontSize: fontSize.xs, color: colors.slate[400] }}>
                          {connection.lastSync}
                        </p>
                      )}
                    </div>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
                    <Badge style={{ backgroundColor: statusStyle.bg, color: statusStyle.text }}>
                      {connection.status}
                    </Badge>
                    {connection.status !== 'connected' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => onReconnect?.(connection.name)}
                        style={{ padding: spacing[1] }}
                      >
                        <RefreshIcon />
                      </Button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </SettingsSection>

        {/* Interface Preferences Section */}
        <SettingsSection title="Interface" description="Customize your experience">
          <ToggleRow
            label="Dark Mode"
            description="Use dark theme across the application"
            checked={preferences.darkMode}
            onChange={(checked) => handlePreferenceChange('darkMode', checked)}
          />
          <ToggleRow
            label="Compact Mode"
            description="Reduce spacing for more content"
            checked={preferences.compactMode}
            onChange={(checked) => handlePreferenceChange('compactMode', checked)}
          />
          <ToggleRow
            label="Show Token Count"
            description="Display token usage in conversations"
            checked={preferences.showTokenCount}
            onChange={(checked) => handlePreferenceChange('showTokenCount', checked)}
          />
          <ToggleRow
            label="Auto Save"
            description="Automatically save changes"
            checked={preferences.autoSave}
            onChange={(checked) => handlePreferenceChange('autoSave', checked)}
          />
        </SettingsSection>

        {/* Save Button */}
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button variant="primary">Save Changes</Button>
        </div>
      </div>
    </div>
  );
};

export default SettingsView;
