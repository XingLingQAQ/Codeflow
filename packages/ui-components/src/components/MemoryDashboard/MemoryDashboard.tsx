/**
 * 记忆管理仪表盘组件
 * 双栏布局：左侧 STM（短期记忆池），右侧 LTM（长期规则库）
 * 支持拖拽归档、惊喜度评分显示、排序
 */

import React, { useState, useCallback, useMemo } from 'react';
import {
  MemoryDashboardProps,
  MemoryPoolProps,
  MemoryItemCardProps,
  SurpriseBarProps,
  MemoryItem,
  MemoryType,
  SortField,
  SortOrder,
  getSurpriseColor,
  getHeatColor,
  SORT_OPTIONS,
} from './types';

/**
 * 惊喜度颜色条
 */
export const SurpriseBar: React.FC<SurpriseBarProps> = ({
  value,
  width = 60,
  height = 6,
  showLabel = false,
}) => {
  const color = getSurpriseColor(value);
  const percentage = Math.round(value * 100);

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      <div
        style={{
          width,
          height,
          backgroundColor: '#e0e0e0',
          borderRadius: height / 2,
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            width: `${percentage}%`,
            height: '100%',
            backgroundColor: color,
            borderRadius: height / 2,
            transition: 'width 0.3s ease',
          }}
        />
      </div>
      {showLabel && (
        <span style={{ fontSize: 10, color: '#666', minWidth: 28 }}>
          {percentage}%
        </span>
      )}
    </div>
  );
};

/**
 * 记忆条目卡片
 */
export const MemoryItemCard: React.FC<MemoryItemCardProps> = ({
  item,
  isDragging = false,
  onClick,
  onDoubleClick,
  onDragStart,
  onTogglePermanent,
  onDelete,
}) => {
  const [isHovered, setIsHovered] = useState(false);

  const sourceColors: Record<string, string> = {
    user: '#2196F3',
    assistant: '#9C27B0',
    system: '#607D8B',
  };

  const formatTime = (ts: number) => {
    const date = new Date(ts);
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  return (
    <div
      draggable
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onDragStart={(e) => {
        e.dataTransfer.setData('text/plain', item.id);
        e.dataTransfer.effectAllowed = 'move';
        onDragStart?.();
      }}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
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
      }}
    >
      {/* 头部：来源标签 + 时间 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 8,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span
            style={{
              fontSize: 10,
              padding: '2px 6px',
              borderRadius: 3,
              backgroundColor: sourceColors[item.source] || '#666',
              color: '#fff',
            }}
          >
            {item.source}
          </span>
          {item.isPermanent && (
            <span
              style={{
                fontSize: 10,
                padding: '2px 6px',
                borderRadius: 3,
                backgroundColor: '#4CAF50',
                color: '#fff',
              }}
            >
              ★ Permanent
            </span>
          )}
        </div>
        <span style={{ fontSize: 11, color: '#999' }}>
          {formatTime(item.timestamp)}
        </span>
      </div>

      {/* 内容预览 */}
      <div
        style={{
          fontSize: 13,
          color: '#333',
          lineHeight: 1.5,
          marginBottom: 10,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          display: '-webkit-box',
          WebkitLineClamp: 2,
          WebkitBoxOrient: 'vertical',
        }}
      >
        {item.content}
      </div>

      {/* 底部：指标 + 操作 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          {/* 热度 */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <span style={{ fontSize: 10, color: '#999' }}>Heat</span>
            <div
              style={{
                width: 8,
                height: 8,
                borderRadius: '50%',
                backgroundColor: getHeatColor(item.heat),
              }}
            />
          </div>
          {/* 惊喜度 */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <span style={{ fontSize: 10, color: '#999' }}>Surprise</span>
            <SurpriseBar value={item.surprise} width={40} height={4} />
          </div>
        </div>

        {/* 操作按钮 */}
        {isHovered && (
          <div style={{ display: 'flex', gap: 4 }}>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onTogglePermanent?.();
              }}
              title={item.isPermanent ? 'Remove permanent' : 'Mark permanent'}
              style={{
                padding: '4px 8px',
                fontSize: 11,
                border: 'none',
                borderRadius: 4,
                backgroundColor: item.isPermanent ? '#ffebee' : '#e8f5e9',
                color: item.isPermanent ? '#c62828' : '#2e7d32',
                cursor: 'pointer',
              }}
            >
              {item.isPermanent ? '☆' : '★'}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onDelete?.();
              }}
              title="Delete"
              style={{
                padding: '4px 8px',
                fontSize: 11,
                border: 'none',
                borderRadius: 4,
                backgroundColor: '#ffebee',
                color: '#c62828',
                cursor: 'pointer',
              }}
            >
              ✕
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

/**
 * 记忆池面板
 */
export const MemoryPool: React.FC<MemoryPoolProps> = ({
  title,
  type,
  items,
  sortField,
  sortOrder,
  onSortChange,
  onItemClick,
  onItemDoubleClick,
  onItemDragStart,
  onItemDrop,
  onTogglePermanent,
  onDeleteItem,
  isDropTarget = false,
  isDragging = false,
  className,
  style,
}) => {
  const [dragOverActive, setDragOverActive] = useState(false);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    setDragOverActive(true);
  }, []);

  const handleDragLeave = useCallback(() => {
    setDragOverActive(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragOverActive(false);
      const itemId = e.dataTransfer.getData('text/plain');
      const item = items.find((i) => i.id === itemId);
      if (item && item.type !== type) {
        onItemDrop?.(item, type);
      }
    },
    [items, type, onItemDrop]
  );

  const sortedItems = useMemo(() => {
    return [...items].sort((a, b) => {
      const aVal = a[sortField];
      const bVal = b[sortField];
      const diff = sortOrder === 'asc' ? aVal - bVal : bVal - aVal;
      return diff;
    });
  }, [items, sortField, sortOrder]);

  return (
    <div
      className={className}
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        backgroundColor: '#fafafa',
        borderRadius: 12,
        border: dragOverActive ? '2px dashed #2196F3' : '1px solid #e0e0e0',
        overflow: 'hidden',
        transition: 'border 0.2s',
        ...style,
      }}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {/* 头部 */}
      <div
        style={{
          padding: '14px 16px',
          backgroundColor: type === 'stm' ? '#e3f2fd' : '#f3e5f5',
          borderBottom: '1px solid #e0e0e0',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 10,
          }}
        >
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: '#333' }}>
            {title}
            <span
              style={{
                marginLeft: 8,
                fontSize: 12,
                fontWeight: 400,
                color: '#666',
              }}
            >
              ({items.length})
            </span>
          </h3>
        </div>

        {/* 排序控制 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 11, color: '#666' }}>Sort by:</span>
          {SORT_OPTIONS.map((opt) => (
            <button
              key={opt.field}
              onClick={() => {
                if (sortField === opt.field) {
                  onSortChange(opt.field, sortOrder === 'asc' ? 'desc' : 'asc');
                } else {
                  onSortChange(opt.field, 'desc');
                }
              }}
              style={{
                padding: '4px 10px',
                fontSize: 11,
                border: 'none',
                borderRadius: 4,
                backgroundColor: sortField === opt.field ? '#1976D2' : '#e0e0e0',
                color: sortField === opt.field ? '#fff' : '#666',
                cursor: 'pointer',
                transition: 'background-color 0.2s',
              }}
            >
              {opt.label}
              {sortField === opt.field && (
                <span style={{ marginLeft: 4 }}>
                  {sortOrder === 'asc' ? '↑' : '↓'}
                </span>
              )}
            </button>
          ))}
        </div>
      </div>

      {/* 内容区 */}
      <div
        style={{
          flex: 1,
          padding: 12,
          overflowY: 'auto',
          minHeight: 200,
        }}
      >
        {sortedItems.length === 0 ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              color: '#999',
              fontSize: 13,
            }}
          >
            {isDragging
              ? `Drop here to ${type === 'ltm' ? 'archive' : 'restore'}`
              : 'No memories'}
          </div>
        ) : (
          sortedItems.map((item) => (
            <MemoryItemCard
              key={item.id}
              item={item}
              onClick={() => onItemClick?.(item)}
              onDoubleClick={() => onItemDoubleClick?.(item)}
              onDragStart={() => onItemDragStart?.(item)}
              onTogglePermanent={() => onTogglePermanent?.(item)}
              onDelete={() => onDeleteItem?.(item)}
            />
          ))
        )}
      </div>

      {/* 拖拽提示 */}
      {dragOverActive && (
        <div
          style={{
            position: 'absolute',
            inset: 0,
            backgroundColor: 'rgba(33, 150, 243, 0.1)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            pointerEvents: 'none',
          }}
        >
          <span
            style={{
              padding: '12px 24px',
              backgroundColor: '#2196F3',
              color: '#fff',
              borderRadius: 8,
              fontSize: 14,
              fontWeight: 500,
            }}
          >
            {type === 'ltm' ? 'Archive to LTM' : 'Restore to STM'}
          </span>
        </div>
      )}
    </div>
  );
};

/**
 * 记忆管理仪表盘
 */
export const MemoryDashboard: React.FC<MemoryDashboardProps> = ({
  stmItems,
  ltmItems,
  onArchive,
  onRestore,
  onDelete,
  onTogglePermanent,
  onNavigateToMessage,
  onRefresh,
  isLoading = false,
  className,
  style,
}) => {
  const [stmSort, setStmSort] = useState<{ field: SortField; order: SortOrder }>({
    field: 'timestamp',
    order: 'desc',
  });
  const [ltmSort, setLtmSort] = useState<{ field: SortField; order: SortOrder }>({
    field: 'surprise',
    order: 'desc',
  });
  const [draggingItem, setDraggingItem] = useState<MemoryItem | null>(null);

  const handleItemDrop = useCallback(
    (item: MemoryItem, targetType: MemoryType) => {
      if (item.type === 'stm' && targetType === 'ltm') {
        onArchive?.(item);
      } else if (item.type === 'ltm' && targetType === 'stm') {
        onRestore?.(item);
      }
      setDraggingItem(null);
    },
    [onArchive, onRestore]
  );

  const handleDoubleClick = useCallback(
    (item: MemoryItem) => {
      onNavigateToMessage?.(item.sessionId, item.messageIndex);
    },
    [onNavigateToMessage]
  );

  // 合并所有 items 用于跨池拖拽查找
  const allItems = useMemo(() => [...stmItems, ...ltmItems], [stmItems, ltmItems]);

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        backgroundColor: '#fff',
        borderRadius: 12,
        overflow: 'hidden',
        ...style,
      }}
    >
      {/* 顶部工具栏 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '12px 16px',
          borderBottom: '1px solid #e0e0e0',
          backgroundColor: '#fafafa',
        }}
      >
        <h2 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: '#333' }}>
          Memory Dashboard
        </h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: 12, color: '#666' }}>
            STM: {stmItems.length} | LTM: {ltmItems.length}
          </span>
          <button
            onClick={onRefresh}
            disabled={isLoading}
            style={{
              padding: '6px 12px',
              fontSize: 12,
              border: 'none',
              borderRadius: 6,
              backgroundColor: '#1976D2',
              color: '#fff',
              cursor: isLoading ? 'not-allowed' : 'pointer',
              opacity: isLoading ? 0.6 : 1,
            }}
          >
            {isLoading ? 'Loading...' : 'Refresh'}
          </button>
        </div>
      </div>

      {/* 双栏布局 */}
      <div
        style={{
          flex: 1,
          display: 'flex',
          gap: 16,
          padding: 16,
          overflow: 'hidden',
        }}
      >
        {/* 左侧：STM 短期记忆池 */}
        <MemoryPool
          title="Short-Term Memory (STM)"
          type="stm"
          items={stmItems}
          sortField={stmSort.field}
          sortOrder={stmSort.order}
          onSortChange={(field, order) => setStmSort({ field, order })}
          onItemDoubleClick={handleDoubleClick}
          onItemDragStart={setDraggingItem}
          onItemDrop={handleItemDrop}
          onTogglePermanent={onTogglePermanent}
          onDeleteItem={onDelete}
          isDragging={!!draggingItem}
          style={{ position: 'relative' }}
        />

        {/* 右侧：LTM 长期规则库 */}
        <MemoryPool
          title="Long-Term Memory (LTM)"
          type="ltm"
          items={ltmItems}
          sortField={ltmSort.field}
          sortOrder={ltmSort.order}
          onSortChange={(field, order) => setLtmSort({ field, order })}
          onItemDoubleClick={handleDoubleClick}
          onItemDragStart={setDraggingItem}
          onItemDrop={handleItemDrop}
          onTogglePermanent={onTogglePermanent}
          onDeleteItem={onDelete}
          isDragging={!!draggingItem}
          style={{ position: 'relative' }}
        />
      </div>

      {/* 底部提示 */}
      <div
        style={{
          padding: '10px 16px',
          borderTop: '1px solid #e0e0e0',
          backgroundColor: '#fafafa',
          fontSize: 11,
          color: '#999',
          textAlign: 'center',
        }}
      >
        Drag memories between pools to archive/restore • Double-click to navigate to original message
      </div>
    </div>
  );
};

export default MemoryDashboard;
