/**
 * ModelSettingsPage - 全局模型设置页面
 * P029: 实现全局模型设置页面，集中管理所有模型配置
 */
import React from 'react';
import { ModelInfo } from './types';
export interface ModelSettingsPageProps {
    models: ModelInfo[];
    apiKeys?: Record<string, {
        key: string;
        isValid: boolean;
    }>;
    defaultModelId?: string;
    costLimit?: number;
    usageStats?: {
        totalCost: number;
        totalTokens: number;
        byModel: Record<string, {
            cost: number;
            tokens: number;
        }>;
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
export declare const ModelSettingsPage: React.FC<ModelSettingsPageProps>;
export default ModelSettingsPage;
//# sourceMappingURL=ModelSettingsPage.d.ts.map