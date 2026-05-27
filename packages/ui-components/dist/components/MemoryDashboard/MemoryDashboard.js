import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * 记忆管理仪表盘组件
 * 双栏布局：左侧 STM（短期记忆池），右侧 LTM（长期规则库）
 * 支持拖拽归档、惊喜度评分显示、排序
 */
import { useState, useCallback, useMemo } from 'react';
import { getSurpriseColor, getHeatColor, SORT_OPTIONS, } from './types';
/**
 * 惊喜度颜色条
 */
export const SurpriseBar = ({ value, width = 60, height = 6, showLabel = false, }) => {
    const color = getSurpriseColor(value);
    const percentage = Math.round(value * 100);
    return (_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 6 }, children: [_jsx("div", { style: {
                    width,
                    height,
                    backgroundColor: '#e0e0e0',
                    borderRadius: height / 2,
                    overflow: 'hidden',
                }, children: _jsx("div", { style: {
                        width: `${percentage}%`,
                        height: '100%',
                        backgroundColor: color,
                        borderRadius: height / 2,
                        transition: 'width 0.3s ease',
                    } }) }), showLabel && (_jsxs("span", { style: { fontSize: 10, color: '#666', minWidth: 28 }, children: [percentage, "%"] }))] }));
};
/**
 * 记忆条目卡片
 */
export const MemoryItemCard = ({ item, isDragging = false, onClick, onDoubleClick, onDragStart, onTogglePermanent, onDelete, }) => {
    const [isHovered, setIsHovered] = useState(false);
    const sourceColors = {
        user: '#2196F3',
        assistant: '#9C27B0',
        system: '#607D8B',
    };
    const formatTime = (ts) => {
        const date = new Date(ts);
        return date.toLocaleString('zh-CN', {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
        });
    };
    return (_jsxs("div", { draggable: true, onClick: onClick, onDoubleClick: onDoubleClick, onDragStart: (e) => {
            e.dataTransfer.setData('text/plain', item.id);
            e.dataTransfer.effectAllowed = 'move';
            onDragStart?.();
        }, onMouseEnter: () => setIsHovered(true), onMouseLeave: () => setIsHovered(false), style: {
            padding: '12px 14px',
            marginBottom: 8,
            backgroundColor: isDragging ? '#f5f5f5' : '#fff',
            border: `1px solid ${item.isPermanent ? '#4CAF50' : '#e0e0e0'}`,
            borderRadius: 8,
            cursor: 'grab',
            opacity: isDragging ? 0.5 : 1,
            boxShadow: isHovered ? '0 2px 8px rgba(0,0,0,0.1)' : 'none',
            transition: 'box-shadow 0.2s, opacity 0.2s',
            position: 'relative',
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: 8,
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 3,
                                    backgroundColor: sourceColors[item.source] || '#666',
                                    color: '#fff',
                                }, children: item.source }), item.isPermanent && (_jsx("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 3,
                                    backgroundColor: '#4CAF50',
                                    color: '#fff',
                                }, children: "\u2605 Permanent" }))] }), _jsx("span", { style: { fontSize: 11, color: '#999' }, children: formatTime(item.timestamp) })] }), _jsx("div", { style: {
                    fontSize: 13,
                    color: '#333',
                    lineHeight: 1.5,
                    marginBottom: 10,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    display: '-webkit-box',
                    WebkitLineClamp: 2,
                    WebkitBoxOrient: 'vertical',
                }, children: item.content }), _jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 16 }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 4 }, children: [_jsx("span", { style: { fontSize: 10, color: '#999' }, children: "Heat" }), _jsx("div", { style: {
                                            width: 8,
                                            height: 8,
                                            borderRadius: '50%',
                                            backgroundColor: getHeatColor(item.heat),
                                        } })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 4 }, children: [_jsx("span", { style: { fontSize: 10, color: '#999' }, children: "Surprise" }), _jsx(SurpriseBar, { value: item.surprise, width: 40, height: 4 })] })] }), isHovered && (_jsxs("div", { style: { display: 'flex', gap: 4 }, children: [_jsx("button", { onClick: (e) => {
                                    e.stopPropagation();
                                    onTogglePermanent?.();
                                }, title: item.isPermanent ? 'Remove permanent' : 'Mark permanent', style: {
                                    padding: '4px 8px',
                                    fontSize: 11,
                                    border: 'none',
                                    borderRadius: 4,
                                    backgroundColor: item.isPermanent ? '#ffebee' : '#e8f5e9',
                                    color: item.isPermanent ? '#c62828' : '#2e7d32',
                                    cursor: 'pointer',
                                }, children: item.isPermanent ? '☆' : '★' }), _jsx("button", { onClick: (e) => {
                                    e.stopPropagation();
                                    onDelete?.();
                                }, title: "Delete", style: {
                                    padding: '4px 8px',
                                    fontSize: 11,
                                    border: 'none',
                                    borderRadius: 4,
                                    backgroundColor: '#ffebee',
                                    color: '#c62828',
                                    cursor: 'pointer',
                                }, children: "\u2715" })] }))] })] }));
};
/**
 * 记忆池面板
 */
export const MemoryPool = ({ title, type, items, sortField, sortOrder, onSortChange, onItemClick, onItemDoubleClick, onItemDragStart, onItemDrop, onTogglePermanent, onDeleteItem, isDropTarget = false, isDragging = false, className, style, }) => {
    const [dragOverActive, setDragOverActive] = useState(false);
    const handleDragOver = useCallback((e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        setDragOverActive(true);
    }, []);
    const handleDragLeave = useCallback(() => {
        setDragOverActive(false);
    }, []);
    const handleDrop = useCallback((e) => {
        e.preventDefault();
        setDragOverActive(false);
        const itemId = e.dataTransfer.getData('text/plain');
        const item = items.find((i) => i.id === itemId);
        if (item && item.type !== type) {
            onItemDrop?.(item, type);
        }
    }, [items, type, onItemDrop]);
    const sortedItems = useMemo(() => {
        return [...items].sort((a, b) => {
            const aVal = a[sortField];
            const bVal = b[sortField];
            const diff = sortOrder === 'asc' ? aVal - bVal : bVal - aVal;
            return diff;
        });
    }, [items, sortField, sortOrder]);
    return (_jsxs("div", { className: className, style: {
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            backgroundColor: '#fafafa',
            borderRadius: 12,
            border: dragOverActive ? '2px dashed #2196F3' : '1px solid #e0e0e0',
            overflow: 'hidden',
            transition: 'border 0.2s',
            ...style,
        }, onDragOver: handleDragOver, onDragLeave: handleDragLeave, onDrop: handleDrop, children: [_jsxs("div", { style: {
                    padding: '14px 16px',
                    backgroundColor: type === 'stm' ? '#e3f2fd' : '#f3e5f5',
                    borderBottom: '1px solid #e0e0e0',
                }, children: [_jsx("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            marginBottom: 10,
                        }, children: _jsxs("h3", { style: { margin: 0, fontSize: 15, fontWeight: 600, color: '#333' }, children: [title, _jsxs("span", { style: {
                                        marginLeft: 8,
                                        fontSize: 12,
                                        fontWeight: 400,
                                        color: '#666',
                                    }, children: ["(", items.length, ")"] })] }) }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: { fontSize: 11, color: '#666' }, children: "Sort by:" }), SORT_OPTIONS.map((opt) => (_jsxs("button", { onClick: () => {
                                    if (sortField === opt.field) {
                                        onSortChange(opt.field, sortOrder === 'asc' ? 'desc' : 'asc');
                                    }
                                    else {
                                        onSortChange(opt.field, 'desc');
                                    }
                                }, style: {
                                    padding: '4px 10px',
                                    fontSize: 11,
                                    border: 'none',
                                    borderRadius: 4,
                                    backgroundColor: sortField === opt.field ? '#1976D2' : '#e0e0e0',
                                    color: sortField === opt.field ? '#fff' : '#666',
                                    cursor: 'pointer',
                                    transition: 'background-color 0.2s',
                                }, children: [opt.label, sortField === opt.field && (_jsx("span", { style: { marginLeft: 4 }, children: sortOrder === 'asc' ? '↑' : '↓' }))] }, opt.field)))] })] }), _jsx("div", { style: {
                    flex: 1,
                    padding: 12,
                    overflowY: 'auto',
                    minHeight: 200,
                }, children: sortedItems.length === 0 ? (_jsx("div", { style: {
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        height: '100%',
                        color: '#999',
                        fontSize: 13,
                    }, children: isDragging
                        ? `Drop here to ${type === 'ltm' ? 'archive' : 'restore'}`
                        : 'No memories' })) : (sortedItems.map((item) => (_jsx(MemoryItemCard, { item: item, onClick: () => onItemClick?.(item), onDoubleClick: () => onItemDoubleClick?.(item), onDragStart: () => onItemDragStart?.(item), onTogglePermanent: () => onTogglePermanent?.(item), onDelete: () => onDeleteItem?.(item) }, item.id)))) }), dragOverActive && (_jsx("div", { style: {
                    position: 'absolute',
                    inset: 0,
                    backgroundColor: 'rgba(33, 150, 243, 0.1)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    pointerEvents: 'none',
                }, children: _jsx("span", { style: {
                        padding: '12px 24px',
                        backgroundColor: '#2196F3',
                        color: '#fff',
                        borderRadius: 8,
                        fontSize: 14,
                        fontWeight: 500,
                    }, children: type === 'ltm' ? 'Archive to LTM' : 'Restore to STM' }) }))] }));
};
/**
 * 记忆管理仪表盘
 */
export const MemoryDashboard = ({ stmItems, ltmItems, onArchive, onRestore, onDelete, onTogglePermanent, onNavigateToMessage, onRefresh, isLoading = false, className, style, }) => {
    const [stmSort, setStmSort] = useState({
        field: 'timestamp',
        order: 'desc',
    });
    const [ltmSort, setLtmSort] = useState({
        field: 'surprise',
        order: 'desc',
    });
    const [draggingItem, setDraggingItem] = useState(null);
    const handleItemDrop = useCallback((item, targetType) => {
        if (item.type === 'stm' && targetType === 'ltm') {
            onArchive?.(item);
        }
        else if (item.type === 'ltm' && targetType === 'stm') {
            onRestore?.(item);
        }
        setDraggingItem(null);
    }, [onArchive, onRestore]);
    const handleDoubleClick = useCallback((item) => {
        onNavigateToMessage?.(item.sessionId, item.messageIndex);
    }, [onNavigateToMessage]);
    // 合并所有 items 用于跨池拖拽查找
    const allItems = useMemo(() => [...stmItems, ...ltmItems], [stmItems, ltmItems]);
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
                }, children: [_jsx("h2", { style: { margin: 0, fontSize: 16, fontWeight: 600, color: '#333' }, children: "Memory Dashboard" }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 12 }, children: [_jsxs("span", { style: { fontSize: 12, color: '#666' }, children: ["STM: ", stmItems.length, " | LTM: ", ltmItems.length] }), _jsx("button", { onClick: onRefresh, disabled: isLoading, style: {
                                    padding: '6px 12px',
                                    fontSize: 12,
                                    border: 'none',
                                    borderRadius: 6,
                                    backgroundColor: '#1976D2',
                                    color: '#fff',
                                    cursor: isLoading ? 'not-allowed' : 'pointer',
                                    opacity: isLoading ? 0.6 : 1,
                                }, children: isLoading ? 'Loading...' : 'Refresh' })] })] }), _jsxs("div", { style: {
                    flex: 1,
                    display: 'flex',
                    gap: 16,
                    padding: 16,
                    overflow: 'hidden',
                }, children: [_jsx(MemoryPool, { title: "Short-Term Memory (STM)", type: "stm", items: stmItems, sortField: stmSort.field, sortOrder: stmSort.order, onSortChange: (field, order) => setStmSort({ field, order }), onItemDoubleClick: handleDoubleClick, onItemDragStart: setDraggingItem, onItemDrop: handleItemDrop, onTogglePermanent: onTogglePermanent, onDeleteItem: onDelete, isDragging: !!draggingItem, style: { position: 'relative' } }), _jsx(MemoryPool, { title: "Long-Term Memory (LTM)", type: "ltm", items: ltmItems, sortField: ltmSort.field, sortOrder: ltmSort.order, onSortChange: (field, order) => setLtmSort({ field, order }), onItemDoubleClick: handleDoubleClick, onItemDragStart: setDraggingItem, onItemDrop: handleItemDrop, onTogglePermanent: onTogglePermanent, onDeleteItem: onDelete, isDragging: !!draggingItem, style: { position: 'relative' } })] }), _jsx("div", { style: {
                    padding: '10px 16px',
                    borderTop: '1px solid #e0e0e0',
                    backgroundColor: '#fafafa',
                    fontSize: 11,
                    color: '#999',
                    textAlign: 'center',
                }, children: "Drag memories between pools to archive/restore \u2022 Double-click to navigate to original message" })] }));
};
export default MemoryDashboard;
//# sourceMappingURL=MemoryDashboard.js.map