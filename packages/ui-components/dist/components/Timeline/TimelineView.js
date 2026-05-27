import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Git 时间轴组件
 * 按时间倒序展示快照历史，支持大量条目虚拟滚动
 */
import { useState, useCallback, useMemo, useRef, useEffect } from 'react';
import { DEFAULT_TIMELINE_CONFIG, TRIGGER_COLORS, TRIGGER_LABELS, groupEntriesByDate, formatRelativeTime, } from './types';
/**
 * 单个时间轴条目组件
 */
const TimelineItem = ({ entry, isActive, compact, showFileCount, onClick, onDoubleClick, onRollbackClick }) => {
    const triggerColor = TRIGGER_COLORS[entry.trigger];
    const triggerLabel = TRIGGER_LABELS[entry.trigger];
    return (_jsxs("div", { onClick: onClick, onDoubleClick: onDoubleClick, style: {
            display: 'flex',
            alignItems: 'flex-start',
            padding: compact ? '8px 12px' : '12px 16px',
            borderLeft: `3px solid ${isActive ? '#2196F3' : 'transparent'}`,
            backgroundColor: isActive ? 'rgba(33, 150, 243, 0.08)' : 'transparent',
            cursor: 'pointer',
            transition: 'background-color 0.15s',
        }, onMouseEnter: (e) => {
            if (!isActive) {
                e.currentTarget.style.backgroundColor = 'rgba(0, 0, 0, 0.04)';
            }
        }, onMouseLeave: (e) => {
            if (!isActive) {
                e.currentTarget.style.backgroundColor = 'transparent';
            }
        }, children: [_jsx("div", { style: {
                    width: 12,
                    height: 12,
                    borderRadius: '50%',
                    backgroundColor: triggerColor,
                    marginRight: 12,
                    marginTop: 4,
                    flexShrink: 0,
                } }), _jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            marginBottom: 4,
                        }, children: [_jsx("code", { style: {
                                    fontSize: 12,
                                    fontFamily: 'monospace',
                                    color: '#1976D2',
                                    backgroundColor: 'rgba(25, 118, 210, 0.1)',
                                    padding: '2px 6px',
                                    borderRadius: 3,
                                }, children: entry.shortHash }), _jsx("span", { style: { fontSize: 11, color: '#999' }, children: formatRelativeTime(entry.timestamp) })] }), _jsx("div", { style: {
                            fontSize: 13,
                            color: '#333',
                            lineHeight: 1.4,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: compact ? 'nowrap' : 'normal',
                            maxHeight: compact ? 'none' : 40,
                        }, children: entry.message }), !compact && (_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            gap: 12,
                            marginTop: 6,
                            fontSize: 11,
                            color: '#666',
                        }, children: [_jsx("span", { style: {
                                    backgroundColor: triggerColor,
                                    color: '#fff',
                                    padding: '1px 6px',
                                    borderRadius: 3,
                                    fontSize: 10,
                                }, children: triggerLabel }), showFileCount && (_jsxs("span", { children: [entry.filesChanged, " file", entry.filesChanged !== 1 ? 's' : ''] })), _jsxs("span", { children: [entry.messageCount, " msgs"] }), entry.tags && entry.tags.length > 0 && (_jsx("span", { style: { color: '#9C27B0' }, children: entry.tags.join(', ') }))] })), !isActive && (_jsx("button", { onClick: onRollbackClick, style: {
                            marginTop: 8,
                            padding: '4px 12px',
                            fontSize: 11,
                            backgroundColor: '#fff',
                            border: '1px solid #F44336',
                            color: '#F44336',
                            borderRadius: 4,
                            cursor: 'pointer',
                            opacity: 0,
                            transition: 'opacity 0.15s',
                        }, onMouseEnter: (e) => {
                            e.currentTarget.style.backgroundColor = '#F44336';
                            e.currentTarget.style.color = '#fff';
                        }, onMouseLeave: (e) => {
                            e.currentTarget.style.backgroundColor = '#fff';
                            e.currentTarget.style.color = '#F44336';
                        }, className: "rollback-btn", children: "Rollback to here" }))] })] }));
};
/**
 * 日期分组头
 */
const DateHeader = ({ date }) => (_jsx("div", { style: {
        padding: '8px 16px',
        backgroundColor: '#f5f5f5',
        fontSize: 12,
        fontWeight: 600,
        color: '#666',
        borderBottom: '1px solid #e0e0e0',
        position: 'sticky',
        top: 0,
        zIndex: 1,
    }, children: date }));
/**
 * Git 时间轴主组件
 */
export const TimelineView = ({ entries, activeEntryId, config: userConfig, width = 300, height = '100%', className, style, onEntryClick, onEntryDoubleClick, onRollbackRequest, onLoadMore, loading = false, hasMore = false, emptyMessage = 'No snapshots yet', }) => {
    const containerRef = useRef(null);
    const [hoveredId, setHoveredId] = useState(null);
    const config = useMemo(() => ({ ...DEFAULT_TIMELINE_CONFIG, ...userConfig }), [userConfig]);
    // 按时间倒序排列
    const sortedEntries = useMemo(() => [...entries].sort((a, b) => b.timestamp - a.timestamp), [entries]);
    // 按日期分组
    const groupedEntries = useMemo(() => (config.groupByDate ? groupEntriesByDate(sortedEntries) : null), [sortedEntries, config.groupByDate]);
    // 处理条目点击
    const handleEntryClick = useCallback((entry, e) => {
        if (onEntryClick) {
            onEntryClick({ entry, originalEvent: e });
        }
    }, [onEntryClick]);
    // 处理条目双击
    const handleEntryDoubleClick = useCallback((entry, e) => {
        if (onEntryDoubleClick) {
            onEntryDoubleClick({ entry, originalEvent: e });
        }
    }, [onEntryDoubleClick]);
    // 处理回滚请求
    const handleRollbackClick = useCallback((entry, e) => {
        e.stopPropagation();
        if (onRollbackRequest) {
            onRollbackRequest({
                targetEntry: entry,
                options: {
                    rollbackGit: true,
                    rollbackConversation: true,
                    rollbackVector: true,
                    rollbackGraph: true,
                },
            });
        }
    }, [onRollbackRequest]);
    // 无限滚动检测
    useEffect(() => {
        const container = containerRef.current;
        if (!container || !hasMore || loading)
            return;
        const handleScroll = () => {
            const { scrollTop, scrollHeight, clientHeight } = container;
            if (scrollHeight - scrollTop - clientHeight < 100) {
                onLoadMore?.();
            }
        };
        container.addEventListener('scroll', handleScroll);
        return () => container.removeEventListener('scroll', handleScroll);
    }, [hasMore, loading, onLoadMore]);
    // 渲染条目
    const renderEntry = (entry) => (_jsxs("div", { onMouseEnter: () => setHoveredId(entry.id), onMouseLeave: () => setHoveredId(null), style: { position: 'relative' }, children: [_jsx(TimelineItem, { entry: entry, isActive: entry.id === activeEntryId, compact: config.compactMode, showFileCount: config.showFileList, onClick: (e) => handleEntryClick(entry, e), onDoubleClick: (e) => handleEntryDoubleClick(entry, e), onRollbackClick: (e) => handleRollbackClick(entry, e) }), _jsx("div", { style: {
                    position: 'absolute',
                    left: 21,
                    top: 24,
                    bottom: 0,
                    width: 2,
                    backgroundColor: '#e0e0e0',
                } }), _jsx("style", { children: `
          .rollback-btn {
            opacity: ${hoveredId === entry.id ? 1 : 0} !important;
          }
        ` })] }, entry.id));
    // 空状态
    if (entries.length === 0 && !loading) {
        return (_jsx("div", { className: className, style: {
                width,
                height,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: '#fafafa',
                color: '#999',
                fontSize: 14,
                ...style,
            }, children: emptyMessage }));
    }
    return (_jsxs("div", { className: className, style: {
            width,
            height,
            display: 'flex',
            flexDirection: 'column',
            backgroundColor: '#fff',
            borderLeft: '1px solid #e0e0e0',
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: '12px 16px',
                    borderBottom: '1px solid #e0e0e0',
                    fontWeight: 600,
                    fontSize: 14,
                    color: '#333',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }, children: [_jsx("span", { children: "Git Timeline" }), _jsx("span", { style: {
                            fontSize: 12,
                            fontWeight: 400,
                            color: '#666',
                            backgroundColor: '#f0f0f0',
                            padding: '2px 8px',
                            borderRadius: 10,
                        }, children: entries.length })] }), _jsxs("div", { ref: containerRef, style: {
                    flex: 1,
                    overflowY: 'auto',
                    overflowX: 'hidden',
                }, children: [config.groupByDate && groupedEntries ? (Array.from(groupedEntries.entries()).map(([date, dateEntries]) => (_jsxs("div", { children: [_jsx(DateHeader, { date: date }), dateEntries.map(renderEntry)] }, date)))) : (sortedEntries.map(renderEntry)), loading && (_jsx("div", { style: {
                            padding: 16,
                            textAlign: 'center',
                            color: '#666',
                            fontSize: 13,
                        }, children: "Loading..." })), hasMore && !loading && (_jsx("div", { style: {
                            padding: 12,
                            textAlign: 'center',
                        }, children: _jsx("button", { onClick: onLoadMore, style: {
                                padding: '6px 16px',
                                fontSize: 12,
                                backgroundColor: '#f5f5f5',
                                border: '1px solid #ddd',
                                borderRadius: 4,
                                cursor: 'pointer',
                                color: '#666',
                            }, children: "Load more" }) }))] })] }));
};
export default TimelineView;
//# sourceMappingURL=TimelineView.js.map