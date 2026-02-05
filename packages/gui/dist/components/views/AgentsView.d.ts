/**
 * AgentsView - Agent 预设视图
 * 预设卡片网格、头像堆叠效果、悬停操作按钮
 */
import React from 'react';
export interface AgentPreset {
    id: string;
    name: string;
    description: string;
    category: string;
    models: string[];
    users: Array<{
        name: string;
        src?: string;
    }>;
    usageCount: number;
    isPopular?: boolean;
}
export interface AgentsViewProps {
    presets?: AgentPreset[];
    onUsePreset?: (presetId: string) => void;
    onCreatePreset?: () => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const AgentsView: React.FC<AgentsViewProps>;
export default AgentsView;
//# sourceMappingURL=AgentsView.d.ts.map