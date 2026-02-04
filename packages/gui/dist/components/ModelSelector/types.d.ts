/**
 * ModelSelector 组件类型定义
 */
import { ModelCapability, ModelProvider, ModelCost } from '@codeflow/core';
/**
 * 模型选项（用于 UI 展示）
 */
export interface ModelOption {
    id: string;
    name: string;
    provider: ModelProvider;
    cost: ModelCost;
    capabilities: ModelCapability[];
    contextWindow?: number;
    description?: string;
    deprecated?: boolean;
}
/**
 * ModelSelector 属性
 */
export interface ModelSelectorProps {
    /** 可选模型列表 */
    models: ModelOption[];
    /** 当前选中的模型 ID */
    selectedModelId?: string;
    /** 选择模型回调 */
    onSelect: (modelId: string) => void;
    /** 是否禁用 */
    disabled?: boolean;
    /** 是否加载中 */
    loading?: boolean;
    /** 占位文本 */
    placeholder?: string;
    /** 自定义类名 */
    className?: string;
    /** 自定义样式 */
    style?: React.CSSProperties;
    /** 是否显示成本信息 */
    showCost?: boolean;
    /** 是否显示能力标签 */
    showCapabilities?: boolean;
    /** 最大显示能力数量 */
    maxCapabilitiesToShow?: number;
}
/**
 * ModelCard 属性
 */
export interface ModelCardProps {
    model: ModelOption;
    isSelected?: boolean;
    onClick?: () => void;
    showCost?: boolean;
    showCapabilities?: boolean;
    maxCapabilitiesToShow?: number;
}
/**
 * ModelFilter 属性
 */
export interface ModelFilterProps {
    /** 可用的提供商列表 */
    providers: ModelProvider[];
    /** 可用的能力列表 */
    capabilities: ModelCapability[];
    /** 当前选中的提供商 */
    selectedProvider?: ModelProvider;
    /** 当前选中的能力 */
    selectedCapabilities?: ModelCapability[];
    /** 提供商变更回调 */
    onProviderChange: (provider?: ModelProvider) => void;
    /** 能力变更回调 */
    onCapabilitiesChange: (capabilities: ModelCapability[]) => void;
    /** 自定义类名 */
    className?: string;
}
/**
 * ModelSearch 属性
 */
export interface ModelSearchProps {
    /** 搜索值 */
    value: string;
    /** 搜索变更回调 */
    onChange: (value: string) => void;
    /** 占位文本 */
    placeholder?: string;
    /** 自定义类名 */
    className?: string;
}
/**
 * 提供商图标映射
 */
export declare const PROVIDER_ICONS: Record<ModelProvider, string>;
/**
 * 提供商显示名称
 */
export declare const PROVIDER_NAMES: Record<ModelProvider, string>;
/**
 * 能力显示名称
 */
export declare const CAPABILITY_NAMES: Record<ModelCapability, string>;
/**
 * 能力颜色映射
 */
export declare const CAPABILITY_COLORS: Record<ModelCapability, string>;
/**
 * 格式化成本显示
 */
export declare function formatCost(cost: ModelCost): string;
/**
 * 格式化上下文窗口
 */
export declare function formatContextWindow(tokens?: number): string;
//# sourceMappingURL=types.d.ts.map