/**
 * ParallelModelConfig - 并行模式模型配置
 * P028: 实现并行模式中各 Worker 的模型配置 UI
 */

import React, { useState } from 'react';
import {
  ModelInfo,
  ParallelWorkerConfig,
  ModelPreset,
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

export interface ParallelModelConfigProps {
  workers: ParallelWorkerConfig[];
  models: ModelInfo[];
  presets?: ModelPreset[];
  maxWorkers?: number;
  estimatedTokensPerWorker?: number;
  onWorkerModelChange?: (workerId: string, modelId: string) => void;
  onWorkerAdd?: () => void;
  onWorkerRemove?: (workerId: string) => void;
  onPresetApply?: (presetId: string) => void;
  onPresetSave?: (name: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

// 预设组合
const PRESET_COMBINATIONS = [
  { id: 'diverse', name: 'Diverse', description: 'Mix of different providers', models: ['claude-3-opus', 'gpt-4o', 'gemini-1.5-pro'] },
  { id: 'claude-team', name: 'Claude Team', description: 'All Claude models', models: ['claude-3-opus', 'claude-3-sonnet', 'claude-3-haiku'] },
  { id: 'cost-effective', name: 'Cost Effective', description: 'Budget-friendly options', models: ['claude-3-haiku', 'gpt-4o-mini', 'gemini-1.5-flash'] },
  { id: 'high-quality', name: 'High Quality', description: 'Best models only', models: ['claude-3-opus', 'gpt-4o', 'gemini-1.5-pro'] },
];

export const ParallelModelConfigPanel: React.FC<ParallelModelConfigProps> = ({
  workers,
  models,
  presets = [],
  maxWorkers = 5,
  estimatedTokensPerWorker = 10000,
  onWorkerModelChange,
  onWorkerAdd,
  onWorkerRemove,
  onPresetApply,
  onPresetSave,
  className,
  style,
}) => {
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null);

  const modelOptions: SelectOption[] = models.map((m) => ({
    value: m.id,
    label: m.name,
    description: `${PROVIDER_INFO[m.provider]?.name || m.provider} • ${formatCost(m.costPerInputToken + m.costPerOutputToken)}`,
    icon: <span>{PROVIDER_INFO[m.provider]?.icon || '🤖'}</span>,
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
  }, {} as Record<string, number>);

  const handleDragStart = (index: number) => {
    setDraggedIndex(index);
  };

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
  };

  const handleDrop = (e: React.DragEvent, targetIndex: number) => {
    e.preventDefault();
    // Reorder logic would go here
    setDraggedIndex(null);
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
              background: `linear-gradient(135deg, ${colors.indigo[500]}, ${colors.primary[500]})`,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 18,
              color: '#fff',
            }}
          >
            🔀
          </div>
          <div>
            <h3 style={{ fontSize: fontSize.base, fontWeight: fontWeight.bold, color: colors.slate[800], margin: 0 }}>
              Parallel Worker Configuration
            </h3>
            <span style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>
              Configure models for parallel execution
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[3] }}>
          {/* Provider Stats */}
          <div style={{ display: 'flex', gap: spacing[1] }}>
            {Object.entries(providerStats).map(([provider, count]) => (
              <Tooltip key={provider} content={`${PROVIDER_INFO[provider]?.name || provider}: ${count} workers`}>
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 4,
                    padding: `${spacing[0.5]}px ${spacing[1.5]}px`,
                    backgroundColor: `${PROVIDER_INFO[provider]?.color || colors.slate[500]}15`,
                    borderRadius: borderRadius.full,
                    fontSize: fontSize.xs,
                  }}
                >
                  <span>{PROVIDER_INFO[provider]?.icon}</span>
                  <span style={{ fontWeight: fontWeight.bold, color: PROVIDER_INFO[provider]?.color }}>{count}</span>
                </div>
              </Tooltip>
            ))}
          </div>
          <Badge variant="info" size="md">
            💰 ~${totalEstimatedCost.toFixed(4)}
          </Badge>
        </div>
      </div>

      {/* Quick Presets */}
      <div
        style={{
          padding: spacing[3],
          borderBottom: `1px solid ${colors.slate[100]}`,
          backgroundColor: colors.slate[50],
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: spacing[2], flexWrap: 'wrap' }}>
          <span style={{ fontSize: fontSize.xs, fontWeight: fontWeight.semibold, color: colors.slate[600] }}>
            Quick Setup:
          </span>
          {PRESET_COMBINATIONS.map((preset) => (
            <Tooltip key={preset.id} content={preset.description}>
              <button
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
            </Tooltip>
          ))}
        </div>
      </div>

      {/* Workers List */}
      <div style={{ padding: spacing[4] }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: spacing[3] }}>
          {workers.map((worker, index) => {
            const selectedModel = models.find((m) => m.id === worker.modelId);
            const providerInfo = selectedModel ? PROVIDER_INFO[selectedModel.provider] : null;

            return (
              <div
                key={worker.workerId}
                draggable
                onDragStart={() => handleDragStart(index)}
                onDragOver={(e) => handleDragOver(e, index)}
                onDrop={(e) => handleDrop(e, index)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: spacing[3],
                  padding: spacing[3],
                  backgroundColor: draggedIndex === index ? colors.primary[50] : colors.slate[50],
                  borderRadius: borderRadius.xl,
                  border: `1px solid ${draggedIndex === index ? colors.primary[300] : colors.slate[200]}`,
                  cursor: 'grab',
                  transition: transitions.fast,
                }}
              >
                {/* Drag Handle */}
                <div style={{ color: colors.slate[400], cursor: 'grab' }}>
                  ⋮⋮
                </div>

                {/* Worker Number */}
                <div
                  style={{
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
                  }}
                >
                  {index + 1}
                </div>

                {/* Worker Name */}
                <div style={{ minWidth: 100 }}>
                  <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[800] }}>
                    {worker.workerName}
                  </div>
                </div>

                {/* Model Selector */}
                <div style={{ flex: 1 }}>
                  <CustomSelect
                    options={modelOptions}
                    value={worker.modelId}
                    onChange={(v) => onWorkerModelChange?.(worker.workerId, v)}
                    placeholder="Select model..."
                    size="sm"
                    searchable
                  />
                </div>

                {/* Cost */}
                {selectedModel && (
                  <div style={{ textAlign: 'right', minWidth: 80 }}>
                    <div style={{ fontSize: fontSize.xs, color: colors.slate[500] }}>Est. Cost</div>
                    <div style={{ fontSize: fontSize.sm, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
                      ${estimateCost(estimatedTokensPerWorker, estimatedTokensPerWorker * 0.5, selectedModel).toFixed(4)}
                    </div>
                  </div>
                )}

                {/* Remove Button */}
                {workers.length > 1 && (
                  <button
                    onClick={() => onWorkerRemove?.(worker.workerId)}
                    style={{
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
                    }}
                  >
                    ×
                  </button>
                )}
              </div>
            );
          })}

          {/* Add Worker Button */}
          {workers.length < maxWorkers && (
            <button
              onClick={onWorkerAdd}
              style={{
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
              }}
            >
              <span style={{ fontSize: 18 }}>+</span>
              Add Worker ({workers.length}/{maxWorkers})
            </button>
          )}
        </div>
      </div>

      {/* Cost Comparison */}
      <div
        style={{
          padding: spacing[4],
          borderTop: `1px solid ${colors.slate[100]}`,
          backgroundColor: colors.slate[50],
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <div style={{ fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }}>
              Total Estimated Cost
            </div>
            <div style={{ fontSize: fontSize.xl, fontWeight: fontWeight.bold, color: colors.slate[800] }}>
              ${totalEstimatedCost.toFixed(4)}
            </div>
          </div>
          <div style={{ textAlign: 'right' }}>
            <div style={{ fontSize: fontSize.xs, color: colors.slate[500], marginBottom: spacing[1] }}>
              Per Worker Average
            </div>
            <div style={{ fontSize: fontSize.lg, fontWeight: fontWeight.semibold, color: colors.slate[700] }}>
              ${(totalEstimatedCost / workers.length).toFixed(4)}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default ParallelModelConfigPanel;
