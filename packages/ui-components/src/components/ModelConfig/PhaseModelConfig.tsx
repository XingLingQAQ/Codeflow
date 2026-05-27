/**
 * PhaseModelConfig - 阶段模型配置面板
 * P026: 实现 Plan 模式各阶段的模型配置 UI
 */

import React, { useState } from 'react';
import {
  ModelInfo,
  PhaseModelConfig as PhaseConfig,
  ModelPreset,
  PHASE_INFO,
  PROVIDER_INFO,
  formatCost,
  formatContextWindow,
  estimateCost,
} from './types';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from '../shared/tokens';
import { CustomSelect, SelectOption } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
import { Card } from '../shared/Card';
import { Badge } from '../shared/Badge';
import { Tooltip } from '../shared/Tooltip';

export interface PhaseModelConfigProps {
  phases: PhaseConfig[];
  models: ModelInfo[];
  presets?: ModelPreset[];
  estimatedTokens?: Record<string, { input: number; output: number }>;
  onPhaseModelChange?: (phaseId: string, modelId: string) => void;
  onPresetApply?: (presetId: string) => void;
  onPresetSave?: (name: string) => void;
  onResetToDefault?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

export const PhaseModelConfigPanel: React.FC<PhaseModelConfigProps> = ({
  phases,
  models,
  presets = [],
  estimatedTokens = {},
  onPhaseModelChange,
  onPresetApply,
  onPresetSave,
  onResetToDefault,
  className,
  style,
}) => {
  const [showSavePreset, setShowSavePreset] = useState(false);
  const [presetName, setPresetName] = useState('');

  const modelOptions: SelectOption[] = models.map((m) => ({
    value: m.id,
    label: m.name,
    description: `${PROVIDER_INFO[m.provider]?.name || m.provider} • ${formatContextWindow(m.contextWindow)} context`,
    icon: <span>{PROVIDER_INFO[m.provider]?.icon || '🤖'}</span>,
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
              backgroundColor: colors.primary[50],
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 18,
            }}
          >
            📋
          </div>
          <div>
            <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }}>
              Phase Model Configuration
            </h3>
            <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
              Configure models for each plan phase
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2] }}>
          <Tooltip content="Estimated total cost">
            <Badge variant="info" size="md">
              💰 ~${totalEstimatedCost.toFixed(4)}
            </Badge>
          </Tooltip>
        </div>
      </div>

      {/* Presets */}
      {presets.length > 0 && (
        <div
          style={{
            padding: spacing[3],
            borderBottom: `1px solid ${colors.slate[100]}`,
            backgroundColor: colors.slate[50],
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2], flexWrap: 'wrap' }}>
            <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[600] }}>
              Presets:
            </span>
            {presets.map((preset) => (
              <button
                key={preset.id}
                onClick={() => onPresetApply?.(preset.id)}
                style={{
                  padding: `${spacing[1]}px ${spacing[2.5]}px`,
                  fontSize: fontSize.xs,
                  fontWeight: fontWeight.medium,
                  borderRadius: borderRadius.full,
                  backgroundColor: preset.isDefault ? colors.primary[50] : '#fff',
                  color: preset.isDefault ? colors.primary[700] : colors.slate[600],
                  border: `1px solid ${preset.isDefault ? colors.primary[200] : colors.slate[200]}`,
                  cursor: 'pointer',
                  transition: transitions.fast,
                }}
              >
                {preset.name}
              </button>
            ))}
            <button
              onClick={() => setShowSavePreset(!showSavePreset)}
              style={{
                padding: `${spacing[1]}px ${spacing[2.5]}px`,
                fontSize: fontSize.xs,
                fontWeight: fontWeight.medium,
                borderRadius: borderRadius.full,
                backgroundColor: 'transparent',
                color: colors.primary[600],
                border: `1px dashed ${colors.primary[300]}`,
                cursor: 'pointer',
              }}
            >
              + Save Current
            </button>
          </div>
          {showSavePreset && (
            <div style={{ display: 'flex', gap: spacing[2], marginTop: spacing[2] }}>
              <input
                type="text"
                value={presetName}
                onChange={(e) => setPresetName(e.target.value)}
                placeholder="Preset name..."
                style={{
                  flex: 1,
                  padding: `${spacing[1.5]}px ${spacing[3]}px`,
                  fontSize: fontSize.sm,
                  borderRadius: borderRadius.lg,
                  border: `1px solid ${colors.slate[200]}`,
                  outline: 'none',
                }}
              />
              <Button size="sm" onClick={handleSavePreset}>Save</Button>
              <Button size="sm" variant="ghost" onClick={() => setShowSavePreset(false)}>Cancel</Button>
            </div>
          )}
        </div>
      )}

      {/* Phase List */}
      <div style={{ padding: spacing[4] }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
          {phases.map((phase) => {
            const phaseInfo = PHASE_INFO[phase.phaseId] || { icon: '📌', color: colors.slate[500], description: '' };
            const selectedModel = models.find((m) => m.id === phase.modelId);
            const tokens = estimatedTokens[phase.phaseId] || { input: 0, output: 0 };
            const phaseCost = selectedModel ? estimateCost(tokens.input, tokens.output, selectedModel) : 0;

            return (
              <div
                key={phase.phaseId}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: spacing[4],
                  padding: spacing[3],
                  backgroundColor: colors.slate[50],
                  borderRadius: borderRadius.xl,
                  border: `1px solid ${colors.slate[100]}`,
                }}
              >
                {/* Phase Info */}
                <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3], minWidth: 180 }}>
                  <div
                    style={{
                      width: 40,
                      height: 40,
                      borderRadius: borderRadius.lg,
                      backgroundColor: `${phaseInfo.color}15`,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontSize: 20,
                    }}
                  >
                    {phaseInfo.icon}
                  </div>
                  <div>
                    <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
                      {phase.phaseName}
                    </div>
                    <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
                      {phaseInfo.description}
                    </div>
                  </div>
                </div>

                {/* Model Selector */}
                <div style={{ flex: 1 }}>
                  <CustomSelect
                    options={modelOptions}
                    value={phase.modelId}
                    onChange={(v) => onPhaseModelChange?.(phase.phaseId, v)}
                    placeholder="Select model..."
                    size="sm"
                    searchable
                  />
                </div>

                {/* Cost Estimate */}
                <div style={{ textAlign: 'right', minWidth: 80 }}>
                  <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Est. Cost</div>
                  <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
                    ${phaseCost.toFixed(4)}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Footer */}
      <div
        style={{
          padding: spacing[4],
          borderTop: `1px solid ${colors.slate[100]}`,
          display: 'flex',
          justifyContent: 'flex-end',
          gap: spacing[2],
        }}
      >
        <Button variant="ghost" onClick={onResetToDefault}>
          Reset to Default
        </Button>
      </div>
    </div>
  );
};

export default PhaseModelConfigPanel;
