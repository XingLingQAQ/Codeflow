/**
 * ModelSettingsPage - 全局模型设置页面
 * P029: 实现全局模型设置页面，集中管理所有模型配置
 */

import React, { useState } from 'react';
import {
  ModelInfo,
  PROVIDER_INFO,
  formatCost,
  formatContextWindow,
} from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Input } from '../shared/Input';
import { Toggle } from '../shared/Toggle';
import { Tabs, TabPanel } from '../shared/Tabs';
import { Modal } from '../shared/Modal';

export interface ModelSettingsPageProps {
  models: ModelInfo[];
  apiKeys?: Record<string, { key: string; isValid: boolean }>;
  defaultModelId?: string;
  costLimit?: number;
  usageStats?: {
    totalCost: number;
    totalTokens: number;
    byModel: Record<string, { cost: number; tokens: number }>;
  };
  onModelAdd?: (model: Omit<ModelInfo, 'id'>) => void;
  onModelRemove?: (modelId: string) => void;
  onModelEdit?: (modelId: string, updates: Partial<ModelInfo>) => void;
  onApiKeyUpdate?: (provider: string, key: string) => void;
  onDefaultModelChange?: (modelId: string) => void;
  onCostLimitChange?: (limit: number) => void;
  onExportConfig?: () => void;
  onImportConfig?: (config: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

export const ModelSettingsPage: React.FC<ModelSettingsPageProps> = ({
  models,
  apiKeys = {},
  defaultModelId,
  costLimit = 10,
  usageStats,
  onModelAdd,
  onModelRemove,
  onModelEdit,
  onApiKeyUpdate,
  onDefaultModelChange,
  onCostLimitChange,
  onExportConfig,
  onImportConfig,
  className,
  style,
}) => {
  const [activeTab, setActiveTab] = useState('models');
  const [showAddModel, setShowAddModel] = useState(false);
  const [editingModelId, setEditingModelId] = useState<string | null>(null);
  const [newModel, setNewModel] = useState<Partial<ModelInfo>>({
    provider: 'anthropic',
    capabilities: [],
  });

  const tabs = [
    { id: 'models', label: 'Models', icon: <span>🤖</span>, badge: models.length },
    { id: 'api-keys', label: 'API Keys', icon: <span>🔑</span> },
    { id: 'limits', label: 'Limits', icon: <span>💰</span> },
    { id: 'usage', label: 'Usage', icon: <span>📊</span> },
  ];

  const providerOptions: SelectOption[] = Object.entries(PROVIDER_INFO).map(([key, info]) => ({
    value: key,
    label: info.name,
    icon: <span>{info.icon}</span>,
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
          padding: `${spacing[6]}px ${spacing[8]}px`,
          backgroundColor: '#fff',
          borderBottom: `1px solid ${colors.slate[200]}`,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: spacing[4] }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[4] }}>
            <div
              style={{
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
              }}
            >
              ⚙️
            </div>
            <div>
              <h1 style={{ fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[900], margin: 0 }}>
                Model Settings
              </h1>
              <span style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
                Manage models, API keys, and usage limits
              </span>
            </div>
          </div>
          <div style={{ display: 'flex', gap: spacing[2] }}>
            <Button variant="secondary" onClick={onExportConfig}>
              📤 Export
            </Button>
            <Button variant="secondary" onClick={() => {}}>
              📥 Import
            </Button>
          </div>
        </div>

        <Tabs tabs={tabs} activeTab={activeTab} onChange={setActiveTab} variant="underline" />
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflow: 'auto', padding: spacing[6] }}>
        <div style={{ maxWidth: 1200, margin: '0 auto' }}>
          {/* Models Tab */}
          <TabPanel tabId="models" activeTab={activeTab} style={{ padding: 0 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: spacing[4] }}>
              <h2 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }}>
                Available Models
              </h2>
              <Button onClick={() => setShowAddModel(true)}>
                + Add Model
              </Button>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: spacing[4] }}>
              {models.map((model) => {
                const providerInfo = PROVIDER_INFO[model.provider];
                const isDefault = model.id === defaultModelId;

                return (
                  <Card key={model.id} padding="md" hoverable>
                    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: spacing[3] }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
                        <div
                          style={{
                            width: 40,
                            height: 40,
                            borderRadius: borderRadius.lg,
                            backgroundColor: `${providerInfo?.color || colors.slate[500]}15`,
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            fontSize: 20,
                          }}
                        >
                          {providerInfo?.icon || '🤖'}
                        </div>
                        <div>
                          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
                            <span style={{ fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
                              {model.name}
                            </span>
                            {isDefault && <Badge size="sm" variant="primary">Default</Badge>}
                          </div>
                          <span style={{ fontSize: fontSize.xs, color: providerInfo?.color || colors.slate[500] }}>
                            {providerInfo?.name || model.provider}
                          </span>
                        </div>
                      </div>
                      <button
                        onClick={() => setEditingModelId(model.id)}
                        style={{
                          padding: spacing[1],
                          backgroundColor: 'transparent',
                          border: 'none',
                          cursor: 'pointer',
                          color: colors.slate[400],
                        }}
                      >
                        ⋮
                      </button>
                    </div>

                    {model.description && (
                      <p style={{ fontSize: fontSize.sm, color: colors.slate[600], marginBottom: spacing[3], lineHeight: 1.5 }}>
                        {model.description}
                      </p>
                    )}

                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: spacing[1], marginBottom: spacing[3] }}>
                      {model.capabilities.map((cap) => (
                        <Badge key={cap} size="sm" variant="default">
                          {cap}
                        </Badge>
                      ))}
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: spacing[2] }}>
                      <div style={{ padding: spacing[2], backgroundColor: colors.slate[50], borderRadius: borderRadius.md }}>
                        <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Input</div>
                        <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
                          {formatCost(model.costPerInputToken)}
                        </div>
                      </div>
                      <div style={{ padding: spacing[2], backgroundColor: colors.slate[50], borderRadius: borderRadius.md }}>
                        <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Output</div>
                        <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
                          {formatCost(model.costPerOutputToken)}
                        </div>
                      </div>
                      <div style={{ padding: spacing[2], backgroundColor: colors.slate[50], borderRadius: borderRadius.md }}>
                        <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Context</div>
                        <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
                          {formatContextWindow(model.contextWindow)}
                        </div>
                      </div>
                    </div>

                    {!isDefault && (
                      <Button
                        variant="ghost"
                        size="sm"
                        fullWidth
                        onClick={() => onDefaultModelChange?.(model.id)}
                        style={{ marginTop: spacing[3] }}
                      >
                        Set as Default
                      </Button>
                    )}
                  </Card>
                );
              })}
            </div>
          </TabPanel>

          {/* API Keys Tab */}
          <TabPanel tabId="api-keys" activeTab={activeTab} style={{ padding: 0 }}>
            <h2 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }}>
              API Key Management
            </h2>
            <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[4] }}>
              {Object.entries(PROVIDER_INFO).map(([provider, info]) => {
                const keyInfo = apiKeys[provider];

                return (
                  <Card key={provider} padding="md">
                    <div style={{ display: 'flex', alignItems: 'center', gap: spacing[4] }}>
                      <div
                        style={{
                          width: 48,
                          height: 48,
                          borderRadius: borderRadius.xl,
                          backgroundColor: `${info.color}15`,
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontSize: 24,
                        }}
                      >
                        {info.icon}
                      </div>
                      <div style={{ flex: 1 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2], marginBottom: spacing[1] }}>
                          <span style={{ fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
                            {info.name}
                          </span>
                          {keyInfo?.isValid ? (
                            <Badge size="sm" variant="success">Connected</Badge>
                          ) : keyInfo?.key ? (
                            <Badge size="sm" variant="error">Invalid</Badge>
                          ) : (
                            <Badge size="sm" variant="default">Not Set</Badge>
                          )}
                        </div>
                        <Input
                          type="password"
                          placeholder={`Enter ${info.name} API key...`}
                          value={keyInfo?.key || ''}
                          onChange={(e) => onApiKeyUpdate?.(provider, e.target.value)}
                          size="sm"
                          fullWidth
                        />
                      </div>
                    </div>
                  </Card>
                );
              })}
            </div>
            <div
              style={{
                marginTop: spacing[4],
                padding: spacing[4],
                backgroundColor: colors.warning.light,
                borderRadius: borderRadius.xl,
                border: `1px solid #fcd34d`,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: spacing[3] }}>
                <span style={{ fontSize: 20 }}>⚠️</span>
                <div>
                  <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[1] }}>
                    Security Notice
                  </div>
                  <div style={{ fontSize: fontSize.sm, color: colors.slate[600] }}>
                    API keys are stored securely and encrypted. Never share your API keys with others.
                  </div>
                </div>
              </div>
            </div>
          </TabPanel>

          {/* Limits Tab */}
          <TabPanel tabId="limits" activeTab={activeTab} style={{ padding: 0 }}>
            <h2 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }}>
              Cost Limits & Alerts
            </h2>
            <Card padding="lg">
              <div style={{ marginBottom: spacing[6] }}>
                <label style={{ display: 'block', fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }}>
                  Session Cost Limit
                </label>
                <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
                  <Input
                    type="number"
                    value={costLimit}
                    onChange={(e) => onCostLimitChange?.(parseFloat(e.target.value))}
                    size="md"
                    style={{ width: 120 }}
                  />
                  <span style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>USD per session</span>
                </div>
                <p style={{ fontSize: fontSize.xs, color: colors.slate[500], marginTop: spacing[2] }}>
                  You'll receive an alert when approaching this limit
                </p>
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
                <Toggle
                  checked={true}
                  label="Alert at 70% of budget"
                  size="md"
                />
                <Toggle
                  checked={true}
                  label="Alert at 90% of budget"
                  size="md"
                />
                <Toggle
                  checked={false}
                  label="Auto-pause at 100% of budget"
                  size="md"
                />
              </div>
            </Card>
          </TabPanel>

          {/* Usage Tab */}
          <TabPanel tabId="usage" activeTab={activeTab} style={{ padding: 0 }}>
            <h2 style={{ fontSize: fontSize.lg, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }}>
              Usage Statistics
            </h2>
            {usageStats ? (
              <>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: spacing[4], marginBottom: spacing[6] }}>
                  <Card padding="md">
                    <div style={{ fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }}>Total Cost</div>
                    <div style={{ fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[800] }}>
                      ${usageStats.totalCost.toFixed(4)}
                    </div>
                  </Card>
                  <Card padding="md">
                    <div style={{ fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }}>Total Tokens</div>
                    <div style={{ fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[800] }}>
                      {formatContextWindow(usageStats.totalTokens)}
                    </div>
                  </Card>
                  <Card padding="md">
                    <div style={{ fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }}>Models Used</div>
                    <div style={{ fontSize: fontSize['2xl'], fontWeight: fontWeight.bold, color: colors.slate[800] }}>
                      {Object.keys(usageStats.byModel).length}
                    </div>
                  </Card>
                </div>

                <Card padding="md">
                  <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], marginBottom: spacing[4] }}>
                    Usage by Model
                  </h3>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
                    {Object.entries(usageStats.byModel).map(([modelId, stats]) => {
                      const model = models.find((m) => m.id === modelId);
                      const percentage = (stats.cost / usageStats.totalCost) * 100;

                      return (
                        <div key={modelId}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: spacing[1] }}>
                            <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
                              {model?.name || modelId}
                            </span>
                            <span style={{ fontSize: fontSize.sm, color: colors.slate[600] }}>
                              ${stats.cost.toFixed(4)} ({percentage.toFixed(1)}%)
                            </span>
                          </div>
                          <div
                            style={{
                              height: 8,
                              borderRadius: borderRadius.full,
                              backgroundColor: colors.slate[100],
                              overflow: 'hidden',
                            }}
                          >
                            <div
                              style={{
                                width: `${percentage}%`,
                                height: '100%',
                                backgroundColor: PROVIDER_INFO[model?.provider || '']?.color || colors.primary[500],
                                borderRadius: borderRadius.full,
                              }}
                            />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </Card>
              </>
            ) : (
              <Card padding="lg" style={{ textAlign: 'center' }}>
                <div style={{ fontSize: 48, marginBottom: spacing[4] }}>📊</div>
                <div style={{ fontSize: fontSize.lg, fontWeight: fontWeight.semibold, color: colors.slate[700], marginBottom: spacing[2] }}>
                  No Usage Data Yet
                </div>
                <div style={{ fontSize: fontSize.sm, color: colors.slate[500] }}>
                  Start using models to see usage statistics here
                </div>
              </Card>
            )}
          </TabPanel>
        </div>
      </div>

      {/* Add Model Modal */}
      <Modal
        isOpen={showAddModel}
        onClose={() => setShowAddModel(false)}
        title="Add Custom Model"
        icon={<span>🤖</span>}
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowAddModel(false)}>
              Cancel
            </Button>
            <Button onClick={handleAddModel}>
              Add Model
            </Button>
          </>
        }
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[4] }}>
          <Input
            label="Model Name"
            placeholder="e.g., claude-3-opus"
            value={newModel.name || ''}
            onChange={(e) => setNewModel({ ...newModel, name: e.target.value })}
            fullWidth
          />
          <CustomSelect
            label="Provider"
            options={providerOptions}
            value={newModel.provider || ''}
            onChange={(v) => setNewModel({ ...newModel, provider: v })}
          />
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: spacing[3] }}>
            <Input
              label="Input Cost ($/1K tokens)"
              type="number"
              step="0.0001"
              value={newModel.costPerInputToken || ''}
              onChange={(e) => setNewModel({ ...newModel, costPerInputToken: parseFloat(e.target.value) })}
            />
            <Input
              label="Output Cost ($/1K tokens)"
              type="number"
              step="0.0001"
              value={newModel.costPerOutputToken || ''}
              onChange={(e) => setNewModel({ ...newModel, costPerOutputToken: parseFloat(e.target.value) })}
            />
          </div>
          <Input
            label="Context Window"
            type="number"
            value={newModel.contextWindow || ''}
            onChange={(e) => setNewModel({ ...newModel, contextWindow: parseInt(e.target.value) })}
            fullWidth
          />
          <Input
            label="Description (optional)"
            placeholder="Brief description of the model..."
            value={newModel.description || ''}
            onChange={(e) => setNewModel({ ...newModel, description: e.target.value })}
            fullWidth
          />
        </div>
      </Modal>
    </div>
  );
};

export default ModelSettingsPage;
