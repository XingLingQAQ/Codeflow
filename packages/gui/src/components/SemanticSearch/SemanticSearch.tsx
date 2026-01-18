/**
 * 语义检索引擎UI组件
 * 三位一体搜索：向量搜索 + 全文搜索 + 图谱关联
 */

import React, { useState, useCallback, useMemo } from 'react';
import {
  SemanticSearchCenterProps,
  SearchInputProps,
  SearchTabsProps,
  SearchResultListProps,
  SearchResultCardProps,
  SearchHistoryProps,
  SearchResultItem,
  SearchType,
  SearchSortField,
  SearchSortOrder,
  SearchHistoryItem,
  SEARCH_TYPE_CONFIG,
  SOURCE_COLORS,
  SEARCH_SORT_OPTIONS,
  highlightText,
  formatTimestamp,
} from './types';

/**
 * 搜索输入框
 */
export const SearchInput: React.FC<SearchInputProps> = ({
  value,
  placeholder = 'Search...',
  isLoading = false,
  onChange,
  onSearch,
  onClear,
  className,
  style,
}) => {
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !isLoading) {
      onSearch();
    }
  };

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '8px 12px',
        backgroundColor: '#fff',
        border: '1px solid #e0e0e0',
        borderRadius: 8,
        ...style,
      }}
    >
      <span style={{ fontSize: 16, color: '#666' }}>🔍</span>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        style={{
          flex: 1,
          border: 'none',
          outline: 'none',
          fontSize: 14,
          color: '#333',
        }}
      />
      {value && (
        <button
          onClick={onClear}
          style={{
            padding: '4px 8px',
            border: 'none',
            background: 'none',
            cursor: 'pointer',
            fontSize: 14,
            color: '#999',
          }}
        >
          ✕
        </button>
      )}
      <button
        onClick={onSearch}
        disabled={isLoading || !value.trim()}
        style={{
          padding: '6px 16px',
          border: 'none',
          borderRadius: 6,
          backgroundColor: value.trim() && !isLoading ? '#1976D2' : '#e0e0e0',
          color: value.trim() && !isLoading ? '#fff' : '#999',
          fontSize: 13,
          cursor: value.trim() && !isLoading ? 'pointer' : 'not-allowed',
        }}
      >
        {isLoading ? 'Searching...' : 'Search'}
      </button>
    </div>
  );
};

/**
 * 搜索标签页
 */
export const SearchTabs: React.FC<SearchTabsProps> = ({
  activeTab,
  onTabChange,
  counts = {},
  className,
  style,
}) => {
  return (
    <div
      className={className}
      style={{
        display: 'flex',
        gap: 4,
        padding: 4,
        backgroundColor: '#f5f5f5',
        borderRadius: 8,
        ...style,
      }}
    >
      {(Object.keys(SEARCH_TYPE_CONFIG) as SearchType[]).map((type) => {
        const config = SEARCH_TYPE_CONFIG[type];
        const isActive = activeTab === type;
        const count = counts[type];

        return (
          <button
            key={type}
            onClick={() => onTabChange(type)}
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 6,
              padding: '10px 16px',
              border: 'none',
              borderRadius: 6,
              backgroundColor: isActive ? '#fff' : 'transparent',
              color: isActive ? config.color : '#666',
              fontSize: 13,
              fontWeight: isActive ? 600 : 400,
              cursor: 'pointer',
              boxShadow: isActive ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
              transition: 'all 0.2s',
            }}
          >
            <span>{config.icon}</span>
            <span>{config.label}</span>
            {count !== undefined && count > 0 && (
              <span
                style={{
                  padding: '2px 6px',
                  fontSize: 10,
                  backgroundColor: isActive ? config.color : '#e0e0e0',
                  color: isActive ? '#fff' : '#666',
                  borderRadius: 10,
                }}
              >
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
};

/**
 * 搜索结果卡片
 */
export const SearchResultCard: React.FC<SearchResultCardProps> = ({
  result,
  query,
  onClick,
  onAddToContext,
}) => {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
        padding: '14px 16px',
        backgroundColor: '#fff',
        border: '1px solid #e0e0e0',
        borderRadius: 8,
        cursor: 'pointer',
        boxShadow: isHovered ? '0 2px 8px rgba(0,0,0,0.08)' : 'none',
        transition: 'box-shadow 0.2s',
      }}
    >
      {/* 头部 */}
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
              backgroundColor: SOURCE_COLORS[result.source],
              color: '#fff',
            }}
          >
            {result.source}
          </span>
          <span
            style={{
              fontSize: 10,
              padding: '2px 6px',
              borderRadius: 3,
              backgroundColor: SEARCH_TYPE_CONFIG[result.type].color,
              color: '#fff',
            }}
          >
            {SEARCH_TYPE_CONFIG[result.type].icon}
          </span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: 11, color: '#999' }}>
            {formatTimestamp(result.timestamp)}
          </span>
          <span
            style={{
              fontSize: 12,
              fontWeight: 600,
              color: result.score >= 0.8 ? '#4CAF50' : result.score >= 0.5 ? '#FF9800' : '#999',
            }}
          >
            {Math.round(result.score * 100)}%
          </span>
        </div>
      </div>

      {/* 标题 */}
      <h4
        style={{
          margin: '0 0 6px 0',
          fontSize: 14,
          fontWeight: 600,
          color: '#333',
        }}
        dangerouslySetInnerHTML={{ __html: highlightText(result.title, query) }}
      />

      {/* 预览内容 */}
      <p
        style={{
          margin: 0,
          fontSize: 13,
          color: '#666',
          lineHeight: 1.5,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          display: '-webkit-box',
          WebkitLineClamp: 2,
          WebkitBoxOrient: 'vertical',
        }}
        dangerouslySetInnerHTML={{ __html: highlightText(result.preview, query) }}
      />

      {/* 操作按钮 */}
      {isHovered && (
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            marginTop: 10,
            paddingTop: 10,
            borderTop: '1px solid #f0f0f0',
          }}
        >
          <button
            onClick={(e) => {
              e.stopPropagation();
              onAddToContext?.();
            }}
            style={{
              padding: '6px 12px',
              fontSize: 11,
              border: 'none',
              borderRadius: 4,
              backgroundColor: '#e3f2fd',
              color: '#1976D2',
              cursor: 'pointer',
            }}
          >
            + Add to Context
          </button>
        </div>
      )}

      {/* 高亮样式 */}
      <style>
        {`
          mark {
            background-color: #fff59d;
            padding: 0 2px;
            border-radius: 2px;
          }
        `}
      </style>
    </div>
  );
};

/**
 * 搜索结果列表
 */
export const SearchResultList: React.FC<SearchResultListProps> = ({
  results,
  query,
  isLoading = false,
  sortField,
  sortOrder,
  onSortChange,
  onResultClick,
  onAddToContext,
  className,
  style,
}) => {
  const sortedResults = useMemo(() => {
    return [...results].sort((a, b) => {
      const aVal = a[sortField];
      const bVal = b[sortField];
      return sortOrder === 'asc' ? aVal - bVal : bVal - aVal;
    });
  }, [results, sortField, sortOrder]);

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        ...style,
      }}
    >
      {/* 排序控制 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 12,
        }}
      >
        <span style={{ fontSize: 13, color: '#666' }}>
          {results.length} results
        </span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 11, color: '#999' }}>Sort by:</span>
          {SEARCH_SORT_OPTIONS.map((opt) => (
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

      {/* 结果列表 */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {isLoading ? (
          <div
            style={{
              padding: 40,
              textAlign: 'center',
              color: '#999',
              fontSize: 13,
            }}
          >
            Searching...
          </div>
        ) : sortedResults.length === 0 ? (
          <div
            style={{
              padding: 40,
              textAlign: 'center',
              color: '#999',
              fontSize: 13,
            }}
          >
            No results found
          </div>
        ) : (
          sortedResults.map((result) => (
            <SearchResultCard
              key={result.id}
              result={result}
              query={query}
              onClick={() => onResultClick?.(result)}
              onAddToContext={() => onAddToContext?.(result)}
            />
          ))
        )}
      </div>
    </div>
  );
};

/**
 * 搜索历史
 */
export const SearchHistory: React.FC<SearchHistoryProps> = ({
  history,
  onHistoryClick,
  onClearHistory,
  className,
  style,
}) => {
  if (history.length === 0) return null;

  return (
    <div
      className={className}
      style={{
        padding: 12,
        backgroundColor: '#fafafa',
        borderRadius: 8,
        ...style,
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
        <span style={{ fontSize: 12, fontWeight: 600, color: '#666' }}>
          Recent Searches
        </span>
        {onClearHistory && (
          <button
            onClick={onClearHistory}
            style={{
              padding: '2px 8px',
              fontSize: 10,
              border: 'none',
              borderRadius: 3,
              backgroundColor: '#ffebee',
              color: '#c62828',
              cursor: 'pointer',
            }}
          >
            Clear
          </button>
        )}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
        {history.slice(0, 10).map((item) => (
          <button
            key={item.id}
            onClick={() => onHistoryClick(item)}
            style={{
              padding: '4px 10px',
              fontSize: 11,
              border: '1px solid #e0e0e0',
              borderRadius: 4,
              backgroundColor: '#fff',
              color: '#666',
              cursor: 'pointer',
            }}
          >
            {SEARCH_TYPE_CONFIG[item.type].icon} {item.query}
          </button>
        ))}
      </div>
    </div>
  );
};

/**
 * 语义搜索中心
 */
export const SemanticSearchCenter: React.FC<SemanticSearchCenterProps> = ({
  onSearch,
  onAddToContext,
  onExportMarkdown,
  history = [],
  onHistoryClick,
  onClearHistory,
  className,
  style,
}) => {
  const [query, setQuery] = useState('');
  const [activeTab, setActiveTab] = useState<SearchType>('vector');
  const [isLoading, setIsLoading] = useState(false);
  const [results, setResults] = useState<SearchResultItem[]>([]);
  const [sortField, setSortField] = useState<SearchSortField>('score');
  const [sortOrder, setSortOrder] = useState<SearchSortOrder>('desc');

  const handleSearch = useCallback(async () => {
    if (!query.trim()) return;

    setIsLoading(true);
    try {
      const searchResults = await onSearch(query, activeTab);
      setResults(searchResults);
    } catch (error) {
      console.error('Search failed:', error);
      setResults([]);
    } finally {
      setIsLoading(false);
    }
  }, [query, activeTab, onSearch]);

  const handleHistoryClick = useCallback(
    (item: SearchHistoryItem) => {
      setQuery(item.query);
      setActiveTab(item.type);
      onHistoryClick?.(item);
    },
    [onHistoryClick]
  );

  const handleExport = useCallback(() => {
    onExportMarkdown?.(results);
  }, [results, onExportMarkdown]);

  // 按类型统计结果数量
  const resultCounts = useMemo(() => {
    const counts: Record<SearchType, number> = { vector: 0, fulltext: 0, graph: 0 };
    results.forEach((r) => {
      counts[r.type]++;
    });
    return counts;
  }, [results]);

  // 过滤当前标签页的结果
  const filteredResults = useMemo(() => {
    return results.filter((r) => r.type === activeTab);
  }, [results, activeTab]);

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
          padding: '12px 16px',
          borderBottom: '1px solid #e0e0e0',
          backgroundColor: '#fafafa',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 12,
          }}
        >
          <h2 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: '#333' }}>
            Semantic Search Center
          </h2>
          {results.length > 0 && (
            <button
              onClick={handleExport}
              style={{
                padding: '6px 12px',
                fontSize: 12,
                border: '1px solid #e0e0e0',
                borderRadius: 6,
                backgroundColor: '#fff',
                color: '#666',
                cursor: 'pointer',
              }}
            >
              📄 Export Markdown
            </button>
          )}
        </div>

        {/* 搜索输入 */}
        <SearchInput
          value={query}
          placeholder="Enter your search query..."
          isLoading={isLoading}
          onChange={setQuery}
          onSearch={handleSearch}
          onClear={() => setQuery('')}
        />

        {/* 搜索历史 */}
        {history.length > 0 && !results.length && (
          <SearchHistory
            history={history}
            onHistoryClick={handleHistoryClick}
            onClearHistory={onClearHistory}
            style={{ marginTop: 12 }}
          />
        )}
      </div>

      {/* 标签页 */}
      <div style={{ padding: '12px 16px 0 16px' }}>
        <SearchTabs
          activeTab={activeTab}
          onTabChange={setActiveTab}
          counts={resultCounts}
        />
      </div>

      {/* 结果列表 */}
      <div style={{ flex: 1, padding: 16, overflowY: 'auto' }}>
        <SearchResultList
          results={filteredResults}
          query={query}
          isLoading={isLoading}
          sortField={sortField}
          sortOrder={sortOrder}
          onSortChange={(field, order) => {
            setSortField(field);
            setSortOrder(order);
          }}
          onResultClick={(result) => console.log('Result clicked:', result)}
          onAddToContext={onAddToContext}
        />
      </div>
    </div>
  );
};

export default SemanticSearchCenter;
