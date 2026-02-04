/**
 * AgentModelConfig - Agent 模型配置面板
 * P027: 实现各 Agent 的模型配置 UI
 */

import React, { useState } from 'react';
import {
  ModelInfo,
  AgentModelConfig as AgentConfig,
  ModelPreset,
  AGENT_ROLE_INFO,
  TASK_TYPE_INFO,
  PROVIDER_INFO,
  formatCost,
  formatContextWindow,
} from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Toggle } from '../shared/Toggle';

export interface AgentModelConfigProps {
  agents: AgentConfig[];
  models: ModelInfo[];
  presets?: ModelPreset[];
  onAgentModelChange?: (agentId: string, modelId: string) => void;
  onTaskTypeOverrideChange?: (agentId: string, taskType: string, modelId: string | null) => void;
  onPresetApply?: (presetId: string) => void;
  onPresetSave?: (name: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

export const AgentModelConfigPanel: React.FC<AgentModelConfigProps> = ({
  agents,
  models,
  presets = [],
  onAgentModelChange,
  onTaskTypeOverrideChange,
  onPresetApply,
  onPresetSave,
  className,
  style,
}) => {
  const [expandedAgentId, setExpandedAgentId] = useState<string | null>(null);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const modelOptions: SelectOption[] = models.map((m) => ({
    value: m.id,
    label: m.name,
    description: `${PROVIDER_INFO[m.provider]?.name || m.provider} • ${formatContextWindow(m.contextWindow)} context`,
    icon: <span>{PROVIDER_INFO[m.provider]?.icon || '🤖'}</span>,
    color: PROVIDER_INFO[m.provider]?.color,
  }));

  const toggleExpand = (agentId: string) => {
    setExpandedAgentId(expandedAgentId === agentId ? null : agentId);
  };

  return (
    <div
      className={className}
      style={{
        backgroundColor: '#fff',
        borderRadius: borderRadius['2xl'],
        border: `1px solid ${colors.slate[200]}`,
        overflow: 'hidden',
        ...style,
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: spacing[4],
          borderBottom: `1px solid ${colors.slate[100]}`,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
          <div
            style={{
              width: 36,
              height: 36,
              borderRadius: borderRadius.lg,
              backgroundColor: colors.indigo[50],
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 18,
            }}
          >
            🤖
          </div>
          <div>
            <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }}>
              Agent Model Configuration
            </h3>
            <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
              Configure models for each agent role
            </span>
          </div>
        </div>
        <Toggle
          checked={showAdvanced}
          onChange={setShowAdvanced}
          label="Advanced"
          size="sm"
        />
      </div>

      {/* Agent List */}
      <div style={{ padding: spacing[4] }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
          {agents.map((agent) => {
            const roleInfo = AGENT_ROLE_INFO[agent.agentRole] || { icon: '🤖', color: colors.slate[500], description: '' };
            const selectedModel = models.find((m) => m.id === agent.modelId);
            const isExpanded = expandedAgentId === agent.agentId;
            const hasOverrides = agent.taskTypeOverrides && Object.keys(agent.taskTypeOverrides).length > 0;

            return (
              <Card
                key={agent.agentId}
                padding="none"
                style={{ overflow: 'hidden' }}
              >
                {/* Agent Header */}
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: spacing[4],
                    padding: spacing[4],
                    cursor: showAdvanced ? 'pointer' : 'default',
                  }}
                  onClick={() => showAdvanced && toggleExpand(agent.agentId)}
                >
                  {/* Agent Info */}
                  <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3], minWidth: 200 }}>
                    <div
                      style={{
                        width: 44,
                        height: 44,
                        borderRadius: borderRadius.xl,
                        backgroundColor: `${roleInfo.color}15`,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        fontSize: 22,
                      }}
                    >
                      {roleInfo.icon}
                    </div>
                    <div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
                        <span style={{ fontSize: fontSize.sm, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
                          {agent.agentName}
                        </span>
                        {hasOverrides && (
                          <Badge size="sm" variant="warning">
                            {Object.keys(agent.taskTypeOverrides!).length} overrides
                          </Badge>
                        )}
                      </div>
                      <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
                        {roleInfo.description}
                      </div>
                    </div>
                  </div>

                  {/* Model Selector */}
                  <div style={{ flex: 1 }} onClick={(e) => e.stopPropagation()}>
                    <CustomSelect
                      options={modelOptions}
                      value={agent.modelId}
                      onChange={(v) => onAgentModelChange?.(agent.agentId, v)}
                      placeholder="Select model..."
                      size="sm"
                      searchable
                    />
                  </div>

                  {/* Model Info */}
                  {selectedModel && (
                    <div style={{ textAlign: 'right', minWidth: 100 }}>
                      <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Cost</div>
                      <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
                        {formatCost(selectedModel.costPerInputToken)} in
                      </div>
                      <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.medium, color: colors.slate[700] }}>
                        {formatCost(selectedModel.costPerOutputToken)} out
                      </div>
                    </div>
                  )}

                  {/* Expand Arrow */}
                  {showAdvanced && (
                    <span
                      style={{
                        fontSize: 12,
                        color: colors.slate[400],
                        transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                        transition: transitions.fast,
                      }}
                    >
                      ▼
                    </span>
                  )}
                </div>

                {/* Task Type Overrides (Advanced) */}
                {showAdvanced && isExpanded && (
                  <div
                    style={{
                      padding: spacing[4],
                      paddingTop: 0,
                      borderTop: `1px solid ${colors.slate[100]}`,
                      backgroundColor: colors.slate[50],
                    }}
                  >
                    <div style={{ marginBottom: spacing[3] }}>
                      <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.bold, color: colors.slate[600], textTransform: 'uppercase' }}>
                        Task Type Overrides
                      </span>
                      <span style={{ fontSize: fontSize.xs, color: colors.slate[400], marginLeft: spacing[2] }}>
                        (Optional: Use different models for specific task types)
                      </span>
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: spacing[3] }}>
                      {Object.entries(TASK_TYPE_INFO).map(([taskType, info]) => {
                        const overrideModelId = agent.taskTypeOverrides?.[taskType];

                        return (
                          <div
                            key={taskType}
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: spacing[2],
                              padding: spacing[2],
                              backgroundColor: '#fff',
                              borderRadius: borderRadius.lg,
                              border: `1px solid ${colors.slate[200]}`,
                            }}
                          >
                            <span style={{ fontSize: 16 }}>{info.icon}</span>
                            <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.medium, color: colors.slate[700], minWidth: 70 }}>
                              {info.label}
                            </span>
                            <div style={{ flex: 1 }}>
                              <CustomSelect
                                options={[
                                  { value: '', label: 'Use default', description: 'Inherit from agent' },
                                  ...modelOptions,
                                ]}
                                value={overrideModelId || ''}
                                onChange={(v) => onTaskTypeOverrideChange?.(agent.agentId, taskType, v || null)}
                                placeholder="Default"
                                size="sm"
                              />
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
              </Card>
            );
          })}
        </div>
      </div>

      {/* Presets */}
      {presets.length > 0 && (
        <div
          style={{
            padding: spacing[4],
            borderTop: `1px solid ${colors.slate[100]}`,
            backgroundColor: colors.slate[50],
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2], flexWrap: 'wrap' }}>
            <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[600] }}>
              Quick Presets:
            </span>
            {presets.map((preset) => (
              <button
                key={preset.id}
                onClick={() => onPresetApply?.(preset.id)}
                style={{
                  padding: `${spacing[1.5]}px ${spacing[3]}px`,
                  fontSize: fontSize.xs,
                  fontWeight: fontWeight.medium,
                  borderRadius: borderRadius.full,
                  backgroundColor: '#fff',
                  color: colors.slate[600],
                  border: `1px solid ${colors.slate[200]}`,
                  cursor: 'pointer',
                  transition: transitions.fast,
                }}
              >
                {preset.name}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

export default AgentModelConfigPanel;
