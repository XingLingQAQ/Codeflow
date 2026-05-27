import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * 图谱可视化组件
 * 基于 Cytoscape.js 实现
 */
import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import { DEFAULT_VIEW_CONFIG, DEFAULT_CHUNK_CONFIG, getNodeColor, } from './types';
/**
 * 将 GraphData 转换为 Cytoscape 元素格式
 */
function toCytoscapeElements(data) {
    const elements = [];
    for (const node of data.nodes) {
        elements.push({
            group: 'nodes',
            data: {
                id: node.id,
                label: node.label,
                type: node.type,
                ...node.data,
            },
            position: node.position,
            style: node.style ? {
                'background-color': node.style.backgroundColor || getNodeColor(node.type),
                'border-color': node.style.borderColor,
                'border-width': node.style.borderWidth,
                width: node.style.width,
                height: node.style.height,
                shape: node.style.shape,
                'font-size': node.style.fontSize,
                color: node.style.fontColor,
            } : {
                'background-color': getNodeColor(node.type),
            },
        });
    }
    for (const edge of data.edges) {
        elements.push({
            group: 'edges',
            data: {
                id: edge.id,
                source: edge.source,
                target: edge.target,
                label: edge.label,
                type: edge.type,
                ...edge.data,
            },
            style: edge.style ? {
                'line-color': edge.style.lineColor,
                width: edge.style.lineWidth,
                'line-style': edge.style.lineStyle,
                'target-arrow-shape': edge.style.targetArrowShape,
                'curve-style': edge.style.curveStyle,
            } : undefined,
        });
    }
    return elements;
}
/**
 * 分片加载数据
 */
async function loadDataInChunks(cy, data, config) {
    const allElements = toCytoscapeElements(data);
    const totalChunks = Math.ceil(allElements.length / config.chunkSize);
    for (let i = 0; i < totalChunks; i++) {
        const start = i * config.chunkSize;
        const end = Math.min(start + config.chunkSize, allElements.length);
        const chunk = allElements.slice(start, end);
        cy.batch(() => {
            cy.add(chunk);
        });
        config.onChunkLoaded?.(end, allElements.length);
        if (i < totalChunks - 1) {
            await new Promise(resolve => setTimeout(resolve, config.loadDelay));
        }
    }
}
/**
 * 默认样式表
 */
const defaultStylesheet = [
    {
        selector: 'node',
        style: {
            'background-color': '#666',
            label: 'data(label)',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': '12px',
            color: '#fff',
            'text-outline-color': '#000',
            'text-outline-width': 1,
            width: 40,
            height: 40,
        },
    },
    {
        selector: 'node:selected',
        style: {
            'border-width': 3,
            'border-color': '#FFD700',
        },
    },
    {
        selector: 'edge',
        style: {
            width: 2,
            'line-color': '#999',
            'target-arrow-color': '#999',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            label: 'data(label)',
            'font-size': '10px',
            'text-rotation': 'autorotate',
            color: '#666',
        },
    },
    {
        selector: 'edge:selected',
        style: {
            'line-color': '#FFD700',
            'target-arrow-color': '#FFD700',
            width: 3,
        },
    },
];
/**
 * 图谱可视化组件
 */
export const GraphView = ({ data, config: userConfig, width = '100%', height = '600px', className, style, onNodeClick, onEdgeClick, onNodeDoubleClick, onBackgroundClick, onLayoutComplete, loading = false, emptyMessage = 'No data to display', }) => {
    const containerRef = useRef(null);
    const cyRef = useRef(null);
    const [isLoading, setIsLoading] = useState(loading);
    const [loadProgress, setLoadProgress] = useState(0);
    const [error, setError] = useState(null);
    const config = useMemo(() => ({ ...DEFAULT_VIEW_CONFIG, ...userConfig }), [userConfig]);
    const chunkConfig = useMemo(() => ({
        ...DEFAULT_CHUNK_CONFIG,
        onChunkLoaded: (loaded, total) => {
            setLoadProgress(Math.round((loaded / total) * 100));
        },
    }), []);
    // 初始化 Cytoscape
    const initCytoscape = useCallback(async () => {
        if (!containerRef.current)
            return;
        try {
            // 动态导入 Cytoscape（避免 SSR 问题）
            // @ts-expect-error - cytoscape is optional peer dependency
            const cytoscape = (await import('cytoscape')).default;
            // 销毁旧实例
            if (cyRef.current) {
                cyRef.current.destroy();
            }
            // 创建新实例
            cyRef.current = cytoscape({
                container: containerRef.current,
                style: defaultStylesheet,
                minZoom: config.minZoom,
                maxZoom: config.maxZoom,
                wheelSensitivity: config.wheelSensitivity,
                panningEnabled: config.panningEnabled,
                userZoomingEnabled: config.userZoomingEnabled,
                boxSelectionEnabled: config.boxSelectionEnabled,
                autoungrabify: config.autoungrabify,
            });
            // 绑定事件
            const cy = cyRef.current;
            cy.on('tap', 'node', (evt) => {
                const e = evt;
                if (onNodeClick) {
                    const nodeData = e.target.data();
                    onNodeClick({
                        node: {
                            id: e.target.id(),
                            label: nodeData.label,
                            type: nodeData.type,
                            data: nodeData,
                        },
                        position: e.target.position(),
                        originalEvent: e.originalEvent,
                    });
                }
            });
            cy.on('tap', 'edge', (evt) => {
                const e = evt;
                if (onEdgeClick) {
                    const edgeData = e.target.data();
                    onEdgeClick({
                        edge: {
                            id: e.target.id(),
                            source: edgeData.source,
                            target: edgeData.target,
                            label: edgeData.label,
                            type: edgeData.type,
                            data: edgeData,
                        },
                        position: e.target.position(),
                        originalEvent: e.originalEvent,
                    });
                }
            });
            cy.on('dbltap', 'node', (evt) => {
                const e = evt;
                if (onNodeDoubleClick) {
                    const nodeData = e.target.data();
                    onNodeDoubleClick({
                        node: {
                            id: e.target.id(),
                            label: nodeData.label,
                            type: nodeData.type,
                            data: nodeData,
                        },
                        position: e.target.position(),
                        originalEvent: e.originalEvent,
                    });
                }
            });
            cy.on('tap', (evt) => {
                const e = evt;
                if (e.target === cy && onBackgroundClick) {
                    onBackgroundClick();
                }
            });
            setError(null);
        }
        catch (err) {
            setError(`Failed to initialize graph: ${err}`);
        }
    }, [config, onNodeClick, onEdgeClick, onNodeDoubleClick, onBackgroundClick]);
    // 加载数据
    const loadData = useCallback(async () => {
        if (!cyRef.current || !data)
            return;
        const cy = cyRef.current;
        setIsLoading(true);
        setLoadProgress(0);
        try {
            // 清除现有元素
            cy.elements().remove();
            // 分片加载大数据集
            if (data.nodes.length + data.edges.length > 1000) {
                await loadDataInChunks(cy, data, chunkConfig);
            }
            else {
                const elements = toCytoscapeElements(data);
                cy.add(elements);
            }
            // 运行布局
            const layout = cy.layout({
                ...config.layout,
                stop: () => {
                    onLayoutComplete?.();
                },
            });
            layout.run();
        }
        catch (err) {
            setError(`Failed to load data: ${err}`);
        }
        finally {
            setIsLoading(false);
            setLoadProgress(100);
        }
    }, [data, config.layout, chunkConfig, onLayoutComplete]);
    // 初始化
    useEffect(() => {
        initCytoscape();
        return () => {
            if (cyRef.current) {
                cyRef.current.destroy();
                cyRef.current = null;
            }
        };
    }, [initCytoscape]);
    // 数据变化时重新加载
    useEffect(() => {
        if (cyRef.current && data) {
            loadData();
        }
    }, [data, loadData]);
    // 窗口大小变化时调整
    useEffect(() => {
        const handleResize = () => {
            if (cyRef.current) {
                cyRef.current.resize();
                cyRef.current.fit(50);
            }
        };
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);
    // 空数据状态
    if (!data || (data.nodes.length === 0 && data.edges.length === 0)) {
        return (_jsx("div", { className: className, style: {
                width,
                height,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: '#f5f5f5',
                color: '#666',
                ...style,
            }, children: emptyMessage }));
    }
    // 错误状态
    if (error) {
        return (_jsx("div", { className: className, style: {
                width,
                height,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: '#fff5f5',
                color: '#c00',
                ...style,
            }, children: error }));
    }
    return (_jsxs("div", { className: className, style: {
            width,
            height,
            position: 'relative',
            ...style,
        }, children: [_jsx("div", { ref: containerRef, style: {
                    width: '100%',
                    height: '100%',
                } }), isLoading && (_jsxs("div", { style: {
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    right: 0,
                    bottom: 0,
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    justifyContent: 'center',
                    backgroundColor: 'rgba(255, 255, 255, 0.8)',
                }, children: [_jsx("div", { style: { marginBottom: 8 }, children: "Loading graph..." }), _jsx("div", { style: {
                            width: 200,
                            height: 4,
                            backgroundColor: '#e0e0e0',
                            borderRadius: 2,
                        }, children: _jsx("div", { style: {
                                width: `${loadProgress}%`,
                                height: '100%',
                                backgroundColor: '#2196F3',
                                borderRadius: 2,
                                transition: 'width 0.2s',
                            } }) }), _jsxs("div", { style: { marginTop: 4, fontSize: 12, color: '#666' }, children: [loadProgress, "%"] })] })), _jsxs("div", { style: {
                    position: 'absolute',
                    bottom: 8,
                    right: 8,
                    padding: '4px 8px',
                    backgroundColor: 'rgba(0, 0, 0, 0.6)',
                    color: '#fff',
                    fontSize: 12,
                    borderRadius: 4,
                }, children: [data.nodes.length, " nodes, ", data.edges.length, " edges"] })] }));
};
export default GraphView;
//# sourceMappingURL=GraphView.js.map