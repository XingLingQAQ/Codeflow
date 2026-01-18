import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * 动态上下文构建器组件
 * 左侧文件树 + 右侧AST树 + 底部Token预算显示
 */
import { useState, useCallback, useMemo } from 'react';
import { AST_TYPE_ICONS, AST_TYPE_COLORS, BUDGET_COLORS, calculateUsagePercent, isOverBudget, isNearBudget, } from './types';
/**
 * 文件树节点组件
 */
const FileTreeNodeComponent = ({ node, depth, selectedPath, onNodeClick, onNodeExpand }) => {
    const isSelected = node.path === selectedPath;
    const hasChildren = node.children && node.children.length > 0;
    return (_jsxs("div", { children: [_jsxs("div", { onClick: () => {
                    if (node.type === 'directory') {
                        onNodeExpand?.(node);
                    }
                    else {
                        onNodeClick?.(node);
                    }
                }, style: {
                    display: 'flex',
                    alignItems: 'center',
                    padding: '6px 8px',
                    paddingLeft: 8 + depth * 16,
                    cursor: 'pointer',
                    backgroundColor: isSelected ? '#e3f2fd' : 'transparent',
                    borderLeft: isSelected ? '3px solid #2196F3' : '3px solid transparent',
                    transition: 'background-color 0.15s',
                }, onMouseEnter: (e) => {
                    if (!isSelected) {
                        e.currentTarget.style.backgroundColor = '#f5f5f5';
                    }
                }, onMouseLeave: (e) => {
                    if (!isSelected) {
                        e.currentTarget.style.backgroundColor = 'transparent';
                    }
                }, children: [hasChildren && (_jsx("span", { style: {
                            marginRight: 4,
                            fontSize: 10,
                            color: '#666',
                            transform: node.isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                            transition: 'transform 0.15s',
                        }, children: "\u25B6" })), !hasChildren && _jsx("span", { style: { width: 14 } }), _jsx("span", { style: { marginRight: 6, fontSize: 14 }, children: node.type === 'directory' ? (node.isExpanded ? '📂' : '📁') : '📄' }), _jsx("span", { style: {
                            flex: 1,
                            fontSize: 13,
                            color: '#333',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                        }, children: node.name }), node.tokenCount !== undefined && node.tokenCount > 0 && (_jsx("span", { style: { fontSize: 10, color: '#999', marginLeft: 8 }, children: node.tokenCount.toLocaleString() }))] }), hasChildren && node.isExpanded && (_jsx("div", { children: node.children.map((child) => (_jsx(FileTreeNodeComponent, { node: child, depth: depth + 1, selectedPath: selectedPath, onNodeClick: onNodeClick, onNodeExpand: onNodeExpand }, child.id))) }))] }));
};
/**
 * 文件树组件
 */
export const FileTree = ({ nodes, selectedPath, onNodeClick, onNodeExpand, className, style, }) => {
    return (_jsx("div", { className: className, style: {
            flex: 1,
            overflowY: 'auto',
            backgroundColor: '#fafafa',
            borderRadius: 8,
            border: '1px solid #e0e0e0',
            ...style,
        }, children: nodes.length === 0 ? (_jsx("div", { style: {
                padding: 20,
                textAlign: 'center',
                color: '#999',
                fontSize: 13,
            }, children: "No files loaded" })) : (nodes.map((node) => (_jsx(FileTreeNodeComponent, { node: node, depth: 0, selectedPath: selectedPath, onNodeClick: onNodeClick, onNodeExpand: onNodeExpand }, node.id)))) }));
};
/**
 * AST树节点组件
 */
const ASTTreeNodeComponent = ({ node, depth, onNodeCheck, onNodeExpand }) => {
    const hasChildren = node.children && node.children.length > 0;
    const icon = AST_TYPE_ICONS[node.type];
    const color = AST_TYPE_COLORS[node.type];
    return (_jsxs("div", { children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    padding: '6px 8px',
                    paddingLeft: 8 + depth * 20,
                    backgroundColor: node.isChecked ? '#e8f5e9' : 'transparent',
                    transition: 'background-color 0.15s',
                }, children: [_jsx("input", { type: "checkbox", checked: node.isChecked || false, ref: (el) => {
                            if (el)
                                el.indeterminate = node.isIndeterminate || false;
                        }, onChange: (e) => onNodeCheck?.(node, e.target.checked), style: { marginRight: 8, cursor: 'pointer' } }), hasChildren && (_jsx("span", { onClick: () => onNodeExpand?.(node), style: {
                            marginRight: 4,
                            fontSize: 10,
                            color: '#666',
                            cursor: 'pointer',
                            transform: node.isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                            transition: 'transform 0.15s',
                        }, children: "\u25B6" })), !hasChildren && _jsx("span", { style: { width: 14 } }), _jsx("span", { style: {
                            display: 'inline-flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            width: 18,
                            height: 18,
                            marginRight: 6,
                            fontSize: 11,
                            fontWeight: 600,
                            backgroundColor: color,
                            color: '#fff',
                            borderRadius: 3,
                        }, children: icon }), _jsx("span", { style: {
                            flex: 1,
                            fontSize: 13,
                            color: '#333',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                        }, children: node.name }), _jsxs("span", { style: { fontSize: 10, color: '#999', marginLeft: 8 }, children: ["L", node.startLine, "-", node.endLine] }), _jsxs("span", { style: {
                            fontSize: 10,
                            color: node.isChecked ? '#4CAF50' : '#999',
                            marginLeft: 8,
                            minWidth: 50,
                            textAlign: 'right',
                        }, children: [node.tokenCount.toLocaleString(), " tk"] })] }), hasChildren && node.isExpanded && (_jsx("div", { children: node.children.map((child) => (_jsx(ASTTreeNodeComponent, { node: child, depth: depth + 1, onNodeCheck: onNodeCheck, onNodeExpand: onNodeExpand }, child.id))) }))] }));
};
/**
 * AST树组件
 */
export const ASTTree = ({ nodes, onNodeCheck, onNodeExpand, onSelectAll, onDeselectAll, className, style, }) => {
    const totalNodes = useMemo(() => {
        const count = (items) => {
            return items.reduce((acc, item) => {
                return acc + 1 + (item.children ? count(item.children) : 0);
            }, 0);
        };
        return count(nodes);
    }, [nodes]);
    const checkedCount = useMemo(() => {
        const count = (items) => {
            return items.reduce((acc, item) => {
                const selfCount = item.isChecked ? 1 : 0;
                const childCount = item.children ? count(item.children) : 0;
                return acc + selfCount + childCount;
            }, 0);
        };
        return count(nodes);
    }, [nodes]);
    return (_jsxs("div", { className: className, style: {
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            backgroundColor: '#fafafa',
            borderRadius: 8,
            border: '1px solid #e0e0e0',
            overflow: 'hidden',
            ...style,
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 12px',
                    borderBottom: '1px solid #e0e0e0',
                    backgroundColor: '#fff',
                }, children: [_jsxs("span", { style: { fontSize: 12, color: '#666' }, children: [checkedCount, " / ", totalNodes, " selected"] }), _jsxs("div", { style: { display: 'flex', gap: 8 }, children: [_jsx("button", { onClick: onSelectAll, style: {
                                    padding: '4px 10px',
                                    fontSize: 11,
                                    border: 'none',
                                    borderRadius: 4,
                                    backgroundColor: '#e3f2fd',
                                    color: '#1976D2',
                                    cursor: 'pointer',
                                }, children: "Select All" }), _jsx("button", { onClick: onDeselectAll, style: {
                                    padding: '4px 10px',
                                    fontSize: 11,
                                    border: 'none',
                                    borderRadius: 4,
                                    backgroundColor: '#ffebee',
                                    color: '#c62828',
                                    cursor: 'pointer',
                                }, children: "Deselect All" })] })] }), _jsx("div", { style: { flex: 1, overflowY: 'auto' }, children: nodes.length === 0 ? (_jsx("div", { style: {
                        padding: 20,
                        textAlign: 'center',
                        color: '#999',
                        fontSize: 13,
                    }, children: "Select a file to view AST" })) : (nodes.map((node) => (_jsx(ASTTreeNodeComponent, { node: node, depth: 0, onNodeCheck: onNodeCheck, onNodeExpand: onNodeExpand }, node.id)))) })] }));
};
/**
 * Token预算饼图组件
 */
const TokenPieChart = ({ budget, size = 120, }) => {
    const segments = [
        { key: 'systemPrompt', value: budget.systemPrompt, color: BUDGET_COLORS.systemPrompt },
        { key: 'recentDialog', value: budget.recentDialog, color: BUDGET_COLORS.recentDialog },
        { key: 'toolSchema', value: budget.toolSchema, color: BUDGET_COLORS.toolSchema },
        { key: 'outputSpace', value: budget.outputSpace, color: BUDGET_COLORS.outputSpace },
        { key: 'contextSelection', value: budget.contextSelection, color: BUDGET_COLORS.contextSelection },
    ];
    const available = Math.max(0, budget.total - budget.used);
    if (available > 0) {
        segments.push({ key: 'available', value: available, color: BUDGET_COLORS.available });
    }
    const total = segments.reduce((acc, s) => acc + s.value, 0);
    let currentAngle = -90;
    const paths = segments.map((segment) => {
        const angle = total > 0 ? (segment.value / total) * 360 : 0;
        const startAngle = currentAngle;
        const endAngle = currentAngle + angle;
        currentAngle = endAngle;
        const startRad = (startAngle * Math.PI) / 180;
        const endRad = (endAngle * Math.PI) / 180;
        const radius = size / 2 - 5;
        const cx = size / 2;
        const cy = size / 2;
        const x1 = cx + radius * Math.cos(startRad);
        const y1 = cy + radius * Math.sin(startRad);
        const x2 = cx + radius * Math.cos(endRad);
        const y2 = cy + radius * Math.sin(endRad);
        const largeArc = angle > 180 ? 1 : 0;
        return {
            key: segment.key,
            color: segment.color,
            d: `M ${cx} ${cy} L ${x1} ${y1} A ${radius} ${radius} 0 ${largeArc} 1 ${x2} ${y2} Z`,
        };
    });
    return (_jsxs("svg", { width: size, height: size, children: [paths.map((path) => (_jsx("path", { d: path.d, fill: path.color }, path.key))), _jsx("circle", { cx: size / 2, cy: size / 2, r: size / 4, fill: "#fff" }), _jsxs("text", { x: size / 2, y: size / 2 - 5, textAnchor: "middle", fontSize: 14, fontWeight: 600, fill: isOverBudget(budget) ? '#F44336' : '#333', children: [Math.round(calculateUsagePercent(budget)), "%"] }), _jsx("text", { x: size / 2, y: size / 2 + 12, textAnchor: "middle", fontSize: 10, fill: "#666", children: "used" })] }));
};
/**
 * Token预算显示组件
 */
export const TokenBudgetDisplay = ({ budget, warningThreshold = 0.9, className, style, }) => {
    const usagePercent = calculateUsagePercent(budget);
    const isOver = isOverBudget(budget);
    const isNear = isNearBudget(budget, warningThreshold);
    const legendItems = [
        { label: 'System Prompt', value: budget.systemPrompt, color: BUDGET_COLORS.systemPrompt },
        { label: 'Recent Dialog', value: budget.recentDialog, color: BUDGET_COLORS.recentDialog },
        { label: 'Tool Schema', value: budget.toolSchema, color: BUDGET_COLORS.toolSchema },
        { label: 'Output Space', value: budget.outputSpace, color: BUDGET_COLORS.outputSpace },
        { label: 'Context Selection', value: budget.contextSelection, color: BUDGET_COLORS.contextSelection },
    ];
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            alignItems: 'center',
            gap: 24,
            padding: 16,
            backgroundColor: isOver ? '#ffebee' : isNear ? '#fff3e0' : '#fafafa',
            borderRadius: 8,
            border: `1px solid ${isOver ? '#F44336' : isNear ? '#FF9800' : '#e0e0e0'}`,
            transition: 'background-color 0.3s, border-color 0.3s',
            ...style,
        }, children: [_jsx(TokenPieChart, { budget: budget, size: 100 }), _jsxs("div", { style: { flex: 1 }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            marginBottom: 12,
                        }, children: [_jsx("span", { style: { fontSize: 14, fontWeight: 600, color: '#333' }, children: "Token Budget" }), _jsxs("span", { style: {
                                    fontSize: 13,
                                    fontWeight: 500,
                                    color: isOver ? '#F44336' : isNear ? '#FF9800' : '#4CAF50',
                                }, children: [budget.used.toLocaleString(), " / ", budget.total.toLocaleString()] })] }), _jsx("div", { style: {
                            height: 8,
                            backgroundColor: '#e0e0e0',
                            borderRadius: 4,
                            overflow: 'hidden',
                            marginBottom: 12,
                        }, children: _jsx("div", { style: {
                                width: `${Math.min(usagePercent, 100)}%`,
                                height: '100%',
                                backgroundColor: isOver ? '#F44336' : isNear ? '#FF9800' : '#4CAF50',
                                borderRadius: 4,
                                transition: 'width 0.3s',
                            } }) }), _jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: '8px 16px' }, children: legendItems.map((item) => (_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 6 }, children: [_jsx("div", { style: {
                                        width: 10,
                                        height: 10,
                                        borderRadius: 2,
                                        backgroundColor: item.color,
                                    } }), _jsxs("span", { style: { fontSize: 11, color: '#666' }, children: [item.label, ": ", item.value.toLocaleString()] })] }, item.label))) }), isOver && (_jsx("div", { style: {
                            marginTop: 12,
                            padding: '8px 12px',
                            backgroundColor: '#F44336',
                            color: '#fff',
                            borderRadius: 4,
                            fontSize: 12,
                        }, children: "\u26A0\uFE0F Token budget exceeded! Please reduce context selection." }))] })] }));
};
/**
 * 动态上下文构建器
 */
export const ContextBuilder = ({ fileTree, astTree, budget, presets = [], onFileSelect, onASTNodeCheck, onSavePreset, onLoadPreset, onDeletePreset, onBuildContext, isLoading = false, className, style, }) => {
    const [selectedFilePath, setSelectedFilePath] = useState();
    const [presetName, setPresetName] = useState('');
    const [showPresetDropdown, setShowPresetDropdown] = useState(false);
    const handleFileClick = useCallback((node) => {
        setSelectedFilePath(node.path);
        onFileSelect?.(node);
    }, [onFileSelect]);
    const handleSavePreset = useCallback(() => {
        if (presetName.trim()) {
            onSavePreset?.(presetName.trim());
            setPresetName('');
        }
    }, [presetName, onSavePreset]);
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: '#fff',
            borderRadius: 12,
            overflow: 'hidden',
            ...style,
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '12px 16px',
                    borderBottom: '1px solid #e0e0e0',
                    backgroundColor: '#fafafa',
                }, children: [_jsx("h2", { style: { margin: 0, fontSize: 16, fontWeight: 600, color: '#333' }, children: "Context Builder" }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 12 }, children: [_jsxs("div", { style: { position: 'relative' }, children: [_jsxs("button", { onClick: () => setShowPresetDropdown(!showPresetDropdown), style: {
                                            padding: '6px 12px',
                                            fontSize: 12,
                                            border: '1px solid #e0e0e0',
                                            borderRadius: 6,
                                            backgroundColor: '#fff',
                                            cursor: 'pointer',
                                        }, children: ["Presets (", presets.length, ") \u25BC"] }), showPresetDropdown && (_jsxs("div", { style: {
                                            position: 'absolute',
                                            top: '100%',
                                            right: 0,
                                            marginTop: 4,
                                            minWidth: 200,
                                            backgroundColor: '#fff',
                                            border: '1px solid #e0e0e0',
                                            borderRadius: 8,
                                            boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
                                            zIndex: 100,
                                        }, children: [_jsxs("div", { style: { padding: 8, borderBottom: '1px solid #e0e0e0' }, children: [_jsx("input", { type: "text", value: presetName, onChange: (e) => setPresetName(e.target.value), placeholder: "Preset name...", style: {
                                                            width: '100%',
                                                            padding: '6px 8px',
                                                            fontSize: 12,
                                                            border: '1px solid #e0e0e0',
                                                            borderRadius: 4,
                                                            marginBottom: 6,
                                                        } }), _jsx("button", { onClick: handleSavePreset, disabled: !presetName.trim(), style: {
                                                            width: '100%',
                                                            padding: '6px 8px',
                                                            fontSize: 11,
                                                            border: 'none',
                                                            borderRadius: 4,
                                                            backgroundColor: presetName.trim() ? '#4CAF50' : '#e0e0e0',
                                                            color: presetName.trim() ? '#fff' : '#999',
                                                            cursor: presetName.trim() ? 'pointer' : 'not-allowed',
                                                        }, children: "Save Current Selection" })] }), presets.length === 0 ? (_jsx("div", { style: { padding: 12, textAlign: 'center', color: '#999', fontSize: 12 }, children: "No presets saved" })) : (presets.map((preset) => (_jsxs("div", { style: {
                                                    display: 'flex',
                                                    alignItems: 'center',
                                                    justifyContent: 'space-between',
                                                    padding: '8px 12px',
                                                    borderBottom: '1px solid #f0f0f0',
                                                }, children: [_jsx("span", { onClick: () => {
                                                            onLoadPreset?.(preset);
                                                            setShowPresetDropdown(false);
                                                        }, style: { fontSize: 12, color: '#333', cursor: 'pointer', flex: 1 }, children: preset.name }), _jsx("button", { onClick: () => onDeletePreset?.(preset), style: {
                                                            padding: '2px 6px',
                                                            fontSize: 10,
                                                            border: 'none',
                                                            borderRadius: 3,
                                                            backgroundColor: '#ffebee',
                                                            color: '#c62828',
                                                            cursor: 'pointer',
                                                        }, children: "\u2715" })] }, preset.id))))] }))] }), _jsx("button", { onClick: onBuildContext, disabled: isLoading, style: {
                                    padding: '6px 16px',
                                    fontSize: 12,
                                    border: 'none',
                                    borderRadius: 6,
                                    backgroundColor: '#1976D2',
                                    color: '#fff',
                                    cursor: isLoading ? 'not-allowed' : 'pointer',
                                    opacity: isLoading ? 0.6 : 1,
                                }, children: isLoading ? 'Building...' : 'Build Context' })] })] }), _jsxs("div", { style: {
                    flex: 1,
                    display: 'flex',
                    gap: 16,
                    padding: 16,
                    overflow: 'hidden',
                }, children: [_jsxs("div", { style: { width: '40%', display: 'flex', flexDirection: 'column' }, children: [_jsx("h3", { style: { margin: '0 0 8px 0', fontSize: 13, fontWeight: 600, color: '#666' }, children: "File Tree" }), _jsx(FileTree, { nodes: fileTree, selectedPath: selectedFilePath, onNodeClick: handleFileClick, style: { flex: 1 } })] }), _jsxs("div", { style: { flex: 1, display: 'flex', flexDirection: 'column' }, children: [_jsxs("h3", { style: { margin: '0 0 8px 0', fontSize: 13, fontWeight: 600, color: '#666' }, children: ["AST Tree ", selectedFilePath && `- ${selectedFilePath.split('/').pop()}`] }), _jsx(ASTTree, { nodes: astTree, onNodeCheck: onASTNodeCheck, style: { flex: 1 } })] })] }), _jsx("div", { style: { padding: '0 16px 16px 16px' }, children: _jsx(TokenBudgetDisplay, { budget: budget }) })] }));
};
export default ContextBuilder;
//# sourceMappingURL=ContextBuilder.js.map