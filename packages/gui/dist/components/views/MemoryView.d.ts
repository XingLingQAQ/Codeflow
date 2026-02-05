/**
 * MemoryView - 记忆节点视图
 * 交互式节点图、节点详情面板、关系匹配度显示
 */
import React from 'react';
export interface MemoryNode {
    id: string;
    label: string;
    type: 'concept' | 'entity' | 'relation' | 'fact';
    content: string;
    connections: string[];
    relevance: number;
    x?: number;
    y?: number;
}
export interface MemoryViewProps {
    nodes?: MemoryNode[];
    onSelectNode?: (nodeId: string) => void;
    onDeleteNode?: (nodeId: string) => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const MemoryView: React.FC<MemoryViewProps>;
export default MemoryView;
//# sourceMappingURL=MemoryView.d.ts.map