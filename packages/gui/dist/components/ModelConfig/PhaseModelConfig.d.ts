/**
 * PhaseModelConfig - 阶段模型配置面板
 * P026: 实现 Plan 模式各阶段的模型配置 UI
 */
import React from 'react';
import { ModelInfo, PhaseModelConfig as PhaseConfig, ModelPreset } from './types';
export interface PhaseModelConfigProps {
    phases: PhaseConfig[];
    models: ModelInfo[];
    presets?: ModelPreset[];
    estimatedTokens?: Record<string, {
        input: number;
        output: number;
    }>;
    onPhaseModelChange?: (phaseId: string, modelId: string) => void;
    onPresetApply?: (presetId: string) => void;
    onPresetSave?: (name: string) => void;
    onResetToDefault?: () => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const PhaseModelConfigPanel: React.FC<PhaseModelConfigProps>;
export default PhaseModelConfigPanel;
//# sourceMappingURL=PhaseModelConfig.d.ts.map