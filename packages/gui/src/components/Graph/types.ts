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
  position?: { x: number; y: number };
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
export type LayoutType =
  | 'grid'
  | 'circle'
  | 'concentric'
  | 'breadthfirst'
  | 'cose'
  | 'cola'
  | 'dagre'
  | 'klay';

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
  // cose 特定
  nodeRepulsion?: number;
  idealEdgeLength?: number;
  edgeElasticity?: number;
  // dagre 特定
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
  position: { x: number; y: number };
  originalEvent: MouseEvent;
}

/**
 * 边点击事件
 */
export interface EdgeClickEvent {
  edge: GraphEdge;
  position: { x: number; y: number };
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
export const DEFAULT_LAYOUT_CONFIG: LayoutConfig = {
  name: 'cose',
  animate: true,
  animationDuration: 500,
  fit: true,
  padding: 50,
  spacingFactor: 1.5,
  nodeDimensionsIncludeLabels: true,
  nodeRepulsion: 4500,
  idealEdgeLength: 100,
  edgeElasticity: 100,
};

export const DEFAULT_VIEW_CONFIG: GraphViewConfig = {
  layout: DEFAULT_LAYOUT_CONFIG,
  minZoom: 0.1,
  maxZoom: 3,
  wheelSensitivity: 0.3,
  panningEnabled: true,
  userZoomingEnabled: true,
  boxSelectionEnabled: true,
  autoungrabify: false,
};

export const DEFAULT_CHUNK_CONFIG: ChunkLoadConfig = {
  chunkSize: 500,
  loadDelay: 50,
};

/**
 * 节点类型颜色映射
 */
export const NODE_TYPE_COLORS: Record<string, string> = {
  'codeflow:File': '#4CAF50',
  'codeflow:Class': '#2196F3',
  'codeflow:Function': '#FF9800',
  'codeflow:Variable': '#9C27B0',
  'codeflow:Module': '#00BCD4',
  'codeflow:Package': '#795548',
  'codeflow:Decision': '#F44336',
  'codeflow:Requirement': '#E91E63',
  'codeflow:Issue': '#FF5722',
  'codeflow:Feature': '#8BC34A',
  'codeflow:Bug': '#f44336',
  'codeflow:Concept': '#607D8B',
  'codeflow:Technology': '#3F51B5',
  'codeflow:Pattern': '#009688',
  default: '#9E9E9E',
};

/**
 * 获取节点颜色
 */
export function getNodeColor(type: string): string {
  return NODE_TYPE_COLORS[type] || NODE_TYPE_COLORS.default;
}
