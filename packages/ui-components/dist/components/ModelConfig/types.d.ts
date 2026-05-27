/**
 * ModelConfig Types - 模型配置组件类型定义
 */
export interface ModelInfo {
    id: string;
    name: string;
    provider: string;
    capabilities: string[];
    costPerInputToken: number;
    costPerOutputToken: number;
    contextWindow: number;
    description?: string;
}
export interface PhaseModelConfig {
    phaseId: string;
    phaseName: string;
    modelId: string;
    description?: string;
}
export interface AgentModelConfig {
    agentId: string;
    agentName: string;
    agentRole: string;
    modelId: string;
    taskTypeOverrides?: Record<string, string>;
}
export interface ParallelWorkerConfig {
    workerId: string;
    workerName: string;
    modelId: string;
}
export interface ModelPreset {
    id: string;
    name: string;
    description: string;
    config: Record<string, string>;
    isDefault?: boolean;
}
export declare const PHASE_INFO: Record<string, {
    icon: string;
    color: string;
    description: string;
}>;
export declare const AGENT_ROLE_INFO: Record<string, {
    icon: string;
    color: string;
    description: string;
}>;
export declare const TASK_TYPE_INFO: Record<string, {
    icon: string;
    label: string;
}>;
export declare const PROVIDER_INFO: Record<string, {
    icon: string;
    color: string;
    name: string;
}>;
export declare const formatCost: (cost: number) => string;
export declare const formatContextWindow: (tokens: number) => string;
export declare const estimateCost: (inputTokens: number, outputTokens: number, model: ModelInfo) => number;
//# sourceMappingURL=types.d.ts.map