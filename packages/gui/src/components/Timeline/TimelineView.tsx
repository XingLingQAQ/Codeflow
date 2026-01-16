/**
 * Git 时间轴组件
 * 按时间倒序展示快照历史，支持大量条目虚拟滚动
 */

import React, { useState, useCallback, useMemo, useRef, useEffect } from 'react';
import {
  TimelineViewProps,
  TimelineEntry,
  TimelineEntryClickEvent,
  DEFAULT_TIMELINE_CONFIG,
  TRIGGER_COLORS,
  TRIGGER_LABELS,
  groupEntriesByDate,
  formatRelativeTime,
} from './types';

/**
 * 单个时间轴条目组件
 */
const TimelineItem: React.FC<{
  entry: TimelineEntry;
  isActive: boolean;
  compact: boolean;
  showFileCount: boolean;
  onClick: (e: React.MouseEvent) => void;
  onDoubleClick: (e: React.MouseEvent) => void;
  onRollbackClick: (e: React.MouseEvent) => void;
}> = ({ entry, isActive, compact, showFileCount, onClick, onDoubleClick, onRollbackClick }) => {
  const triggerColor = TRIGGER_COLORS[entry.trigger];
  const triggerLabel = TRIGGER_LABELS[entry.trigger];

  return (
    <div
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        padding: compact ? '8px 12px' : '12px 16px',
        borderLeft: `3px solid ${isActive ? '#2196F3' : 'transparent'}`,
        backgroundColor: isActive ? 'rgba(33, 150, 243, 0.08)' : 'transparent',
        cursor: 'pointer',
        transition: 'background-color 0.15s',
      }}
      onMouseEnter={(e) => {
        if (!isActive) {
          e.currentTarget.style.backgroundColor = 'rgba(0, 0, 0, 0.04)';
        }
      }}
      onMouseLeave={(e) => {
        if (!isActive) {
          e.currentTarget.style.backgroundColor = 'transparent';
        }
      }}
    >
      {/* 时间线节点 */}
      <div
        style={{
          width: 12,
          height: 12,
          borderRadius: '50%',
          backgroundColor: triggerColor,
          marginRight: 12,
          marginTop: 4,
          flexShrink: 0,
        }}
      />

      {/* 内容区 */}
      <div style={{ flex: 1, minWidth: 0 }}>
        {/* 第一行：hash + 时间 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 4,
          }}
        >
          <code
            style={{
              fontSize: 12,
              fontFamily: 'monospace',
              color: '#1976D2',
              backgroundColor: 'rgba(25, 118, 210, 0.1)',
              padding: '2px 6px',
              borderRadius: 3,
            }}
          >
            {entry.shortHash}
          </code>
          <span style={{ fontSize: 11, color: '#999' }}>
            {formatRelativeTime(entry.timestamp)}
          </span>
        </div>

        {/* 第二行：消息 */}
        <div
          style={{
            fontSize: 13,
            color: '#333',
            lineHeight: 1.4,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: compact ? 'nowrap' : 'normal',
            maxHeight: compact ? 'none' : 40,
          }}
        >
          {entry.message}
        </div>

        {/* 第三行：元数据 */}
        {!compact && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              marginTop: 6,
              fontSize: 11,
              color: '#666',
            }}
          >
            <span
              style={{
                backgroundColor: triggerColor,
                color: '#fff',
                padding: '1px 6px',
                borderRadius: 3,
                fontSize: 10,
              }}
            >
              {triggerLabel}
            </span>
            {showFileCount && (
              <span>
                {entry.filesChanged} file{entry.filesChanged !== 1 ? 's' : ''}
              </span>
            )}
            <span>{entry.messageCount} msgs</span>
            {entry.tags && entry.tags.length > 0 && (
              <span style={{ color: '#9C27B0' }}>
                {entry.tags.join(', ')}
              </span>
            )}
          </div>
        )}

        {/* 回滚按钮（悬停显示） */}
        {!isActive && (
          <button
            onClick={onRollbackClick}
            style={{
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
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.backgroundColor = '#F44336';
              e.currentTarget.style.color = '#fff';
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.backgroundColor = '#fff';
              e.currentTarget.style.color = '#F44336';
            }}
            className="rollback-btn"
          >
            Rollback to here
          </button>
        )}
      </div>
    </div>
  );
};

/**
 * 日期分组头
 */
const DateHeader: React.FC<{ date: string }> = ({ date }) => (
  <div
    style={{
      padding: '8px 16px',
      backgroundColor: '#f5f5f5',
      fontSize: 12,
      fontWeight: 600,
      color: '#666',
      borderBottom: '1px solid #e0e0e0',
      position: 'sticky',
      top: 0,
      zIndex: 1,
    }}
  >
    {date}
  </div>
);

/**
 * Git 时间轴主组件
 */
export const TimelineView: React.FC<TimelineViewProps> = ({
  entries,
  activeEntryId,
  config: userConfig,
  width = 300,
  height = '100%',
  className,
  style,
  onEntryClick,
  onEntryDoubleClick,
  onRollbackRequest,
  onLoadMore,
  loading = false,
  hasMore = false,
  emptyMessage = 'No snapshots yet',
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [hoveredId, setHoveredId] = useState<string | null>(null);

  const config = useMemo(
    () => ({ ...DEFAULT_TIMELINE_CONFIG, ...userConfig }),
    [userConfig]
  );

  // 按时间倒序排列
  const sortedEntries = useMemo(
    () => [...entries].sort((a, b) => b.timestamp - a.timestamp),
    [entries]
  );

  // 按日期分组
  const groupedEntries = useMemo(
    () => (config.groupByDate ? groupEntriesByDate(sortedEntries) : null),
    [sortedEntries, config.groupByDate]
  );

  // 处理条目点击
  const handleEntryClick = useCallback(
    (entry: TimelineEntry, e: React.MouseEvent) => {
      if (onEntryClick) {
        onEntryClick({ entry, originalEvent: e });
      }
    },
    [onEntryClick]
  );

  // 处理条目双击
  const handleEntryDoubleClick = useCallback(
    (entry: TimelineEntry, e: React.MouseEvent) => {
      if (onEntryDoubleClick) {
        onEntryDoubleClick({ entry, originalEvent: e });
      }
    },
    [onEntryDoubleClick]
  );

  // 处理回滚请求
  const handleRollbackClick = useCallback(
    (entry: TimelineEntry, e: React.MouseEvent) => {
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
    },
    [onRollbackRequest]
  );

  // 无限滚动检测
  useEffect(() => {
    const container = containerRef.current;
    if (!container || !hasMore || loading) return;

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
  const renderEntry = (entry: TimelineEntry) => (
    <div
      key={entry.id}
      onMouseEnter={() => setHoveredId(entry.id)}
      onMouseLeave={() => setHoveredId(null)}
      style={{ position: 'relative' }}
    >
      <TimelineItem
        entry={entry}
        isActive={entry.id === activeEntryId}
        compact={config.compactMode}
        showFileCount={config.showFileList}
        onClick={(e) => handleEntryClick(entry, e)}
        onDoubleClick={(e) => handleEntryDoubleClick(entry, e)}
        onRollbackClick={(e) => handleRollbackClick(entry, e)}
      />
      {/* 时间线连接线 */}
      <div
        style={{
          position: 'absolute',
          left: 21,
          top: 24,
          bottom: 0,
          width: 2,
          backgroundColor: '#e0e0e0',
        }}
      />
      {/* 悬停时显示回滚按钮 */}
      <style>
        {`
          .rollback-btn {
            opacity: ${hoveredId === entry.id ? 1 : 0} !important;
          }
        `}
      </style>
    </div>
  );

  // 空状态
  if (entries.length === 0 && !loading) {
    return (
      <div
        className={className}
        style={{
          width,
          height,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: '#fafafa',
          color: '#999',
          fontSize: 14,
          ...style,
        }}
      >
        {emptyMessage}
      </div>
    );
  }

  return (
    <div
      className={className}
      style={{
        width,
        height,
        display: 'flex',
        flexDirection: 'column',
        backgroundColor: '#fff',
        borderLeft: '1px solid #e0e0e0',
        ...style,
      }}
    >
      {/* 头部 */}
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid #e0e0e0',
          fontWeight: 600,
          fontSize: 14,
          color: '#333',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span>Git Timeline</span>
        <span
          style={{
            fontSize: 12,
            fontWeight: 400,
            color: '#666',
            backgroundColor: '#f0f0f0',
            padding: '2px 8px',
            borderRadius: 10,
          }}
        >
          {entries.length}
        </span>
      </div>

      {/* 时间轴列表 */}
      <div
        ref={containerRef}
        style={{
          flex: 1,
          overflowY: 'auto',
          overflowX: 'hidden',
        }}
      >
        {config.groupByDate && groupedEntries ? (
          Array.from(groupedEntries.entries()).map(([date, dateEntries]) => (
            <div key={date}>
              <DateHeader date={date} />
              {dateEntries.map(renderEntry)}
            </div>
          ))
        ) : (
          sortedEntries.map(renderEntry)
        )}

        {/* 加载更多指示器 */}
        {loading && (
          <div
            style={{
              padding: 16,
              textAlign: 'center',
              color: '#666',
              fontSize: 13,
            }}
          >
            Loading...
          </div>
        )}

        {hasMore && !loading && (
          <div
            style={{
              padding: 12,
              textAlign: 'center',
            }}
          >
            <button
              onClick={onLoadMore}
              style={{
                padding: '6px 16px',
                fontSize: 12,
                backgroundColor: '#f5f5f5',
                border: '1px solid #ddd',
                borderRadius: 4,
                cursor: 'pointer',
                color: '#666',
              }}
            >
              Load more
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export default TimelineView;
