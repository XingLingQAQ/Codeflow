/**
 * AgentModelConfig - Agent 模型配置面板
 * P027: 实现各 Agent 的模型配置 UI
 */
import React from 'react';
import { ModelInfo, AgentModelConfig as AgentConfig, ModelPreset } from './types';
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
export declare const AgentModelConfigPanel: React.FC<AgentModelConfigProps>;
export default AgentModelConfigPanel;
//# sourceMappingURL=AgentModelConfig.d.ts.map