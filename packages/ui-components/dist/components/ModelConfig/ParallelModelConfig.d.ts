/**
 * ParallelModelConfig - 并行模式模型配置
 * P028: 实现并行模式中各 Worker 的模型配置 UI
 */
import React from 'react';
import { ModelInfo, ParallelWorkerConfig, ModelPreset } from './types';
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
export declare const ParallelModelConfigPanel: React.FC<ParallelModelConfigProps>;
export default ParallelModelConfigPanel;
//# sourceMappingURL=ParallelModelConfig.d.ts.map