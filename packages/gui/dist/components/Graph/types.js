/**
 * 图谱可视化类型定义
 */
/**
 * 默认配置
 */
export const DEFAULT_LAYOUT_CONFIG = {
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
export const DEFAULT_VIEW_CONFIG = {
    layout: DEFAULT_LAYOUT_CONFIG,
    minZoom: 0.1,
    maxZoom: 3,
    wheelSensitivity: 0.3,
    panningEnabled: true,
    userZoomingEnabled: true,
    boxSelectionEnabled: true,
    autoungrabify: false,
};
export const DEFAULT_CHUNK_CONFIG = {
    chunkSize: 500,
    loadDelay: 50,
};
/**
 * 节点类型颜色映射
 */
export const NODE_TYPE_COLORS = {
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
export function getNodeColor(type) {
    return NODE_TYPE_COLORS[type] || NODE_TYPE_COLORS.default;
}
//# sourceMappingURL=types.js.map