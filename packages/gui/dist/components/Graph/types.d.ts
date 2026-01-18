/**
 * 图谱可视化类型定义
 */
/**
 * 图谱节点
 */
export interface GraphNode {
    id: string;
    label: string;
    type: string;
    data?: Record<string, unknown>;
    position?: {
        x: number;
        y: number;
    };
    style?: NodeStyle;
}
/**
 * 图谱边
 */
export interface GraphEdge {
    id: string;
    source: string;
    target: string;
    label?: string;
    type?: string;
    data?: Record<string, unknown>;
    style?: EdgeStyle;
}
/**
 * 节点样式
 */
export interface NodeStyle {
    backgroundColor?: string;
    borderColor?: string;
    borderWidth?: number;
    width?: number;
    height?: number;
    shape?: 'ellipse' | 'rectangle' | 'roundrectangle' | 'diamond' | 'hexagon';
    fontSize?: number;
    fontColor?: string;
}
/**
 * 边样式
 */
export interface EdgeStyle {
    lineColor?: string;
    lineWidth?: number;
    lineStyle?: 'solid' | 'dashed' | 'dotted';
    targetArrowShape?: 'triangle' | 'circle' | 'square' | 'none';
    curveStyle?: 'bezier' | 'straight' | 'segments';
}
/**
 * 图谱数据
 */
export interface GraphData {
    nodes: GraphNode[];
    edges: GraphEdge[];
}
/**
 * 布局类型
 */
export type LayoutType = 'grid' | 'circle' | 'concentric' | 'breadthfirst' | 'cose' | 'cola' | 'dagre' | 'klay';
/**
 * 布局配置
 */
export interface LayoutConfig {
    name: LayoutType;
    animate?: boolean;
    animationDuration?: number;
    fit?: boolean;
    padding?: number;
    spacingFactor?: number;
    nodeDimensionsIncludeLabels?: boolean;
    nodeRepulsion?: number;
    idealEdgeLength?: number;
    edgeElasticity?: number;
    rankDir?: 'TB' | 'BT' | 'LR' | 'RL';
    rankSep?: number;
    nodeSep?: number;
}
/**
 * 图谱视图配置
 */
export interface GraphViewConfig {
    layout: LayoutConfig;
    minZoom?: number;
    maxZoom?: number;
    wheelSensitivity?: number;
    panningEnabled?: boolean;
    userZoomingEnabled?: boolean;
    boxSelectionEnabled?: boolean;
    autoungrabify?: boolean;
}
/**
 * 节点点击事件
 */
export interface NodeClickEvent {
    node: GraphNode;
    position: {
        x: number;
        y: number;
    };
    originalEvent: MouseEvent;
}
/**
 * 边点击事件
 */
export interface EdgeClickEvent {
    edge: GraphEdge;
    position: {
        x: number;
        y: number;
    };
    originalEvent: MouseEvent;
}
/**
 * 图谱组件 Props
 */
export interface GraphViewProps {
    data: GraphData;
    config?: Partial<GraphViewConfig>;
    width?: number | string;
    height?: number | string;
    className?: string;
    style?: React.CSSProperties;
    onNodeClick?: (event: NodeClickEvent) => void;
    onEdgeClick?: (event: EdgeClickEvent) => void;
    onNodeDoubleClick?: (event: NodeClickEvent) => void;
    onBackgroundClick?: () => void;
    onLayoutComplete?: () => void;
    loading?: boolean;
    emptyMessage?: string;
}
/**
 * 分片加载配置
 */
export interface ChunkLoadConfig {
    chunkSize: number;
    loadDelay: number;
    onChunkLoaded?: (loaded: number, total: number) => void;
}
/**
 * 默认配置
 */
export declare const DEFAULT_LAYOUT_CONFIG: LayoutConfig;
export declare const DEFAULT_VIEW_CONFIG: GraphViewConfig;
export declare const DEFAULT_CHUNK_CONFIG: ChunkLoadConfig;
/**
 * 节点类型颜色映射
 */
export declare const NODE_TYPE_COLORS: Record<string, string>;
/**
 * 获取节点颜色
 */
export declare function getNodeColor(type: string): string;
//# sourceMappingURL=types.d.ts.map