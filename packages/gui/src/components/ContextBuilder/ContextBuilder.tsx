/**
 * 动态上下文构建器组件
 * 左侧文件树 + 右侧AST树 + 底部Token预算显示
 */

import React, { useState, useCallback, useMemo } from 'react';
import {
  ContextBuilderProps,
  FileTreeProps,
  ASTTreeProps,
  TokenBudgetDisplayProps,
  FileTreeNode,
  ASTTreeNode,
  TokenBudget,
  AST_TYPE_ICONS,
  AST_TYPE_COLORS,
  BUDGET_COLORS,
  calculateUsagePercent,
  isOverBudget,
  isNearBudget,
} from './types';

/**
 * 文件树节点组件
 */
const FileTreeNodeComponent: React.FC<{
  node: FileTreeNode;
  depth: number;
  selectedPath?: string;
  onNodeClick?: (node: FileTreeNode) => void;
  onNodeExpand?: (node: FileTreeNode) => void;
}> = ({ node, depth, selectedPath, onNodeClick, onNodeExpand }) => {
  const isSelected = node.path === selectedPath;
  const hasChildren = node.children && node.children.length > 0;

  return (
    <div>
      <div
        onClick={() => {
          if (node.type === 'directory') {
            onNodeExpand?.(node);
          } else {
            onNodeClick?.(node);
          }
        }}
        style={{
          display: 'flex',
          alignItems: 'center',
          padding: '6px 8px',
          paddingLeft: 8 + depth * 16,
          cursor: 'pointer',
          backgroundColor: isSelected ? '#e3f2fd' : 'transparent',
          borderLeft: isSelected ? '3px solid #2196F3' : '3px solid transparent',
          transition: 'background-color 0.15s',
        }}
        onMouseEnter={(e) => {
          if (!isSelected) {
            e.currentTarget.style.backgroundColor = '#f5f5f5';
          }
        }}
        onMouseLeave={(e) => {
          if (!isSelected) {
            e.currentTarget.style.backgroundColor = 'transparent';
          }
        }}
      >
        {/* 展开/折叠图标 */}
        {hasChildren && (
          <span
            style={{
              marginRight: 4,
              fontSize: 10,
              color: '#666',
              transform: node.isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s',
            }}
          >
            ▶
          </span>
        )}
        {!hasChildren && <span style={{ width: 14 }} />}

        {/* 文件/文件夹图标 */}
        <span style={{ marginRight: 6, fontSize: 14 }}>
          {node.type === 'directory' ? (node.isExpanded ? '📂' : '📁') : '📄'}
        </span>

        {/* 名称 */}
        <span
          style={{
            flex: 1,
            fontSize: 13,
            color: '#333',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {node.name}
        </span>

        {/* Token计数 */}
        {node.tokenCount !== undefined && node.tokenCount > 0 && (
          <span style={{ fontSize: 10, color: '#999', marginLeft: 8 }}>
            {node.tokenCount.toLocaleString()}
          </span>
        )}
      </div>

      {/* 子节点 */}
      {hasChildren && node.isExpanded && (
        <div>
          {node.children!.map((child) => (
            <FileTreeNodeComponent
              key={child.id}
              node={child}
              depth={depth + 1}
              selectedPath={selectedPath}
              onNodeClick={onNodeClick}
              onNodeExpand={onNodeExpand}
            />
          ))}
        </div>
      )}
    </div>
  );
};

/**
 * 文件树组件
 */
export const FileTree: React.FC<FileTreeProps> = ({
  nodes,
  selectedPath,
  onNodeClick,
  onNodeExpand,
  className,
  style,
}) => {
  return (
    <div
      className={className}
      style={{
        flex: 1,
        overflowY: 'auto',
        backgroundColor: '#fafafa',
        borderRadius: 8,
        border: '1px solid #e0e0e0',
        ...style,
      }}
    >
      {nodes.length === 0 ? (
        <div
          style={{
            padding: 20,
            textAlign: 'center',
            color: '#999',
            fontSize: 13,
          }}
        >
          No files loaded
        </div>
      ) : (
        nodes.map((node) => (
          <FileTreeNodeComponent
            key={node.id}
            node={node}
            depth={0}
            selectedPath={selectedPath}
            onNodeClick={onNodeClick}
            onNodeExpand={onNodeExpand}
          />
        ))
      )}
    </div>
  );
};

/**
 * AST树节点组件
 */
const ASTTreeNodeComponent: React.FC<{
  node: ASTTreeNode;
  depth: number;
  onNodeCheck?: (node: ASTTreeNode, checked: boolean) => void;
  onNodeExpand?: (node: ASTTreeNode) => void;
}> = ({ node, depth, onNodeCheck, onNodeExpand }) => {
  const hasChildren = node.children && node.children.length > 0;
  const icon = AST_TYPE_ICONS[node.type];
  const color = AST_TYPE_COLORS[node.type];

  return (
    <div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          padding: '6px 8px',
          paddingLeft: 8 + depth * 20,
          backgroundColor: node.isChecked ? '#e8f5e9' : 'transparent',
          transition: 'background-color 0.15s',
        }}
      >
        {/* 复选框 */}
        <input
          type="checkbox"
          checked={node.isChecked || false}
          ref={(el) => {
            if (el) el.indeterminate = node.isIndeterminate || false;
          }}
          onChange={(e) => onNodeCheck?.(node, e.target.checked)}
          style={{ marginRight: 8, cursor: 'pointer' }}
        />

        {/* 展开/折叠图标 */}
        {hasChildren && (
          <span
            onClick={() => onNodeExpand?.(node)}
            style={{
              marginRight: 4,
              fontSize: 10,
              color: '#666',
              cursor: 'pointer',
              transform: node.isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s',
            }}
          >
            ▶
          </span>
        )}
        {!hasChildren && <span style={{ width: 14 }} />}

        {/* 类型图标 */}
        <span
          style={{
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
          }}
        >
          {icon}
        </span>

        {/* 名称 */}
        <span
          style={{
            flex: 1,
            fontSize: 13,
            color: '#333',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {node.name}
        </span>

        {/* 行号范围 */}
        <span style={{ fontSize: 10, color: '#999', marginLeft: 8 }}>
          L{node.startLine}-{node.endLine}
        </span>

        {/* Token计数 */}
        <span
          style={{
            fontSize: 10,
            color: node.isChecked ? '#4CAF50' : '#999',
            marginLeft: 8,
            minWidth: 50,
            textAlign: 'right',
          }}
        >
          {node.tokenCount.toLocaleString()} tk
        </span>
      </div>

      {/* 子节点 */}
      {hasChildren && node.isExpanded && (
        <div>
          {node.children!.map((child) => (
            <ASTTreeNodeComponent
              key={child.id}
              node={child}
              depth={depth + 1}
              onNodeCheck={onNodeCheck}
              onNodeExpand={onNodeExpand}
            />
          ))}
        </div>
      )}
    </div>
  );
};

/**
 * AST树组件
 */
export const ASTTree: React.FC<ASTTreeProps> = ({
  nodes,
  onNodeCheck,
  onNodeExpand,
  onSelectAll,
  onDeselectAll,
  className,
  style,
}) => {
  const totalNodes = useMemo(() => {
    const count = (items: ASTTreeNode[]): number => {
      return items.reduce((acc, item) => {
        return acc + 1 + (item.children ? count(item.children) : 0);
      }, 0);
    };
    return count(nodes);
  }, [nodes]);

  const checkedCount = useMemo(() => {
    const count = (items: ASTTreeNode[]): number => {
      return items.reduce((acc, item) => {
        const selfCount = item.isChecked ? 1 : 0;
        const childCount = item.children ? count(item.children) : 0;
        return acc + selfCount + childCount;
      }, 0);
    };
    return count(nodes);
  }, [nodes]);

  return (
    <div
      className={className}
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        backgroundColor: '#fafafa',
        borderRadius: 8,
        border: '1px solid #e0e0e0',
        overflow: 'hidden',
        ...style,
      }}
    >
      {/* 工具栏 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '8px 12px',
          borderBottom: '1px solid #e0e0e0',
          backgroundColor: '#fff',
        }}
      >
        <span style={{ fontSize: 12, color: '#666' }}>
          {checkedCount} / {totalNodes} selected
        </span>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            onClick={onSelectAll}
            style={{
              padding: '4px 10px',
              fontSize: 11,
              border: 'none',
              borderRadius: 4,
              backgroundColor: '#e3f2fd',
              color: '#1976D2',
              cursor: 'pointer',
            }}
          >
            Select All
          </button>
          <button
            onClick={onDeselectAll}
            style={{
              padding: '4px 10px',
              fontSize: 11,
              border: 'none',
              borderRadius: 4,
              backgroundColor: '#ffebee',
              color: '#c62828',
              cursor: 'pointer',
            }}
          >
            Deselect All
          </button>
        </div>
      </div>

      {/* 节点列表 */}
      <div style={{ flex: 1, overflowY: 'auto' }}>
        {nodes.length === 0 ? (
          <div
            style={{
              padding: 20,
              textAlign: 'center',
              color: '#999',
              fontSize: 13,
            }}
          >
            Select a file to view AST
          </div>
        ) : (
          nodes.map((node) => (
            <ASTTreeNodeComponent
              key={node.id}
              node={node}
              depth={0}
              onNodeCheck={onNodeCheck}
              onNodeExpand={onNodeExpand}
            />
          ))
        )}
      </div>
    </div>
  );
};

/**
 * Token预算饼图组件
 */
const TokenPieChart: React.FC<{ budget: TokenBudget; size?: number }> = ({
  budget,
  size = 120,
}) => {
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

  return (
    <svg width={size} height={size}>
      {paths.map((path) => (
        <path key={path.key} d={path.d} fill={path.color} />
      ))}
      {/* 中心圆 */}
      <circle cx={size / 2} cy={size / 2} r={size / 4} fill="#fff" />
      {/* 中心文字 */}
      <text
        x={size / 2}
        y={size / 2 - 5}
        textAnchor="middle"
        fontSize={14}
        fontWeight={600}
        fill={isOverBudget(budget) ? '#F44336' : '#333'}
      >
        {Math.round(calculateUsagePercent(budget))}%
      </text>
      <text
        x={size / 2}
        y={size / 2 + 12}
        textAnchor="middle"
        fontSize={10}
        fill="#666"
      >
        used
      </text>
    </svg>
  );
};

/**
 * Token预算显示组件
 */
export const TokenBudgetDisplay: React.FC<TokenBudgetDisplayProps> = ({
  budget,
  warningThreshold = 0.9,
  className,
  style,
}) => {
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

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 24,
        padding: 16,
        backgroundColor: isOver ? '#ffebee' : isNear ? '#fff3e0' : '#fafafa',
        borderRadius: 8,
        border: `1px solid ${isOver ? '#F44336' : isNear ? '#FF9800' : '#e0e0e0'}`,
        transition: 'background-color 0.3s, border-color 0.3s',
        ...style,
      }}
    >
      {/* 饼图 */}
      <TokenPieChart budget={budget} size={100} />

      {/* 图例 */}
      <div style={{ flex: 1 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 12,
          }}
        >
          <span style={{ fontSize: 14, fontWeight: 600, color: '#333' }}>
            Token Budget
          </span>
          <span
            style={{
              fontSize: 13,
              fontWeight: 500,
              color: isOver ? '#F44336' : isNear ? '#FF9800' : '#4CAF50',
            }}
          >
            {budget.used.toLocaleString()} / {budget.total.toLocaleString()}
          </span>
        </div>

        {/* 进度条 */}
        <div
          style={{
            height: 8,
            backgroundColor: '#e0e0e0',
            borderRadius: 4,
            overflow: 'hidden',
            marginBottom: 12,
          }}
        >
          <div
            style={{
              width: `${Math.min(usagePercent, 100)}%`,
              height: '100%',
              backgroundColor: isOver ? '#F44336' : isNear ? '#FF9800' : '#4CAF50',
              borderRadius: 4,
              transition: 'width 0.3s',
            }}
          />
        </div>

        {/* 图例列表 */}
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px 16px' }}>
          {legendItems.map((item) => (
            <div
              key={item.label}
              style={{ display: 'flex', alignItems: 'center', gap: 6 }}
            >
              <div
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: 2,
                  backgroundColor: item.color,
                }}
              />
              <span style={{ fontSize: 11, color: '#666' }}>
                {item.label}: {item.value.toLocaleString()}
              </span>
            </div>
          ))}
        </div>

        {/* 警告信息 */}
        {isOver && (
          <div
            style={{
              marginTop: 12,
              padding: '8px 12px',
              backgroundColor: '#F44336',
              color: '#fff',
              borderRadius: 4,
              fontSize: 12,
            }}
          >
            ⚠️ Token budget exceeded! Please reduce context selection.
          </div>
        )}
      </div>
    </div>
  );
};

/**
 * 动态上下文构建器
 */
export const ContextBuilder: React.FC<ContextBuilderProps> = ({
  fileTree,
  astTree,
  budget,
  presets = [],
  onFileSelect,
  onASTNodeCheck,
  onSavePreset,
  onLoadPreset,
  onDeletePreset,
  onBuildContext,
  isLoading = false,
  className,
  style,
}) => {
  const [selectedFilePath, setSelectedFilePath] = useState<string | undefined>();
  const [presetName, setPresetName] = useState('');
  const [showPresetDropdown, setShowPresetDropdown] = useState(false);

  const handleFileClick = useCallback(
    (node: FileTreeNode) => {
      setSelectedFilePath(node.path);
      onFileSelect?.(node);
    },
    [onFileSelect]
  );

  const handleSavePreset = useCallback(() => {
    if (presetName.trim()) {
      onSavePreset?.(presetName.trim());
      setPresetName('');
    }
  }, [presetName, onSavePreset]);

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
          Context Builder
        </h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          {/* 预设管理 */}
          <div style={{ position: 'relative' }}>
            <button
              onClick={() => setShowPresetDropdown(!showPresetDropdown)}
              style={{
                padding: '6px 12px',
                fontSize: 12,
                border: '1px solid #e0e0e0',
                borderRadius: 6,
                backgroundColor: '#fff',
                cursor: 'pointer',
              }}
            >
              Presets ({presets.length}) ▼
            </button>
            {showPresetDropdown && (
              <div
                style={{
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
                }}
              >
                {/* 保存新预设 */}
                <div style={{ padding: 8, borderBottom: '1px solid #e0e0e0' }}>
                  <input
                    type="text"
                    value={presetName}
                    onChange={(e) => setPresetName(e.target.value)}
                    placeholder="Preset name..."
                    style={{
                      width: '100%',
                      padding: '6px 8px',
                      fontSize: 12,
                      border: '1px solid #e0e0e0',
                      borderRadius: 4,
                      marginBottom: 6,
                    }}
                  />
                  <button
                    onClick={handleSavePreset}
                    disabled={!presetName.trim()}
                    style={{
                      width: '100%',
                      padding: '6px 8px',
                      fontSize: 11,
                      border: 'none',
                      borderRadius: 4,
                      backgroundColor: presetName.trim() ? '#4CAF50' : '#e0e0e0',
                      color: presetName.trim() ? '#fff' : '#999',
                      cursor: presetName.trim() ? 'pointer' : 'not-allowed',
                    }}
                  >
                    Save Current Selection
                  </button>
                </div>
                {/* 预设列表 */}
                {presets.length === 0 ? (
                  <div style={{ padding: 12, textAlign: 'center', color: '#999', fontSize: 12 }}>
                    No presets saved
                  </div>
                ) : (
                  presets.map((preset) => (
                    <div
                      key={preset.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        padding: '8px 12px',
                        borderBottom: '1px solid #f0f0f0',
                      }}
                    >
                      <span
                        onClick={() => {
                          onLoadPreset?.(preset);
                          setShowPresetDropdown(false);
                        }}
                        style={{ fontSize: 12, color: '#333', cursor: 'pointer', flex: 1 }}
                      >
                        {preset.name}
                      </span>
                      <button
                        onClick={() => onDeletePreset?.(preset)}
                        style={{
                          padding: '2px 6px',
                          fontSize: 10,
                          border: 'none',
                          borderRadius: 3,
                          backgroundColor: '#ffebee',
                          color: '#c62828',
                          cursor: 'pointer',
                        }}
                      >
                        ✕
                      </button>
                    </div>
                  ))
                )}
              </div>
            )}
          </div>

          {/* 构建按钮 */}
          <button
            onClick={onBuildContext}
            disabled={isLoading}
            style={{
              padding: '6px 16px',
              fontSize: 12,
              border: 'none',
              borderRadius: 6,
              backgroundColor: '#1976D2',
              color: '#fff',
              cursor: isLoading ? 'not-allowed' : 'pointer',
              opacity: isLoading ? 0.6 : 1,
            }}
          >
            {isLoading ? 'Building...' : 'Build Context'}
          </button>
        </div>
      </div>

      {/* 主内容区：双栏布局 */}
      <div
        style={{
          flex: 1,
          display: 'flex',
          gap: 16,
          padding: 16,
          overflow: 'hidden',
        }}
      >
        {/* 左侧：文件树 */}
        <div style={{ width: '40%', display: 'flex', flexDirection: 'column' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: 13, fontWeight: 600, color: '#666' }}>
            File Tree
          </h3>
          <FileTree
            nodes={fileTree}
            selectedPath={selectedFilePath}
            onNodeClick={handleFileClick}
            style={{ flex: 1 }}
          />
        </div>

        {/* 右侧：AST树 */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: 13, fontWeight: 600, color: '#666' }}>
            AST Tree {selectedFilePath && `- ${selectedFilePath.split('/').pop()}`}
          </h3>
          <ASTTree
            nodes={astTree}
            onNodeCheck={onASTNodeCheck}
            style={{ flex: 1 }}
          />
        </div>
      </div>

      {/* 底部：Token预算 */}
      <div style={{ padding: '0 16px 16px 16px' }}>
        <TokenBudgetDisplay budget={budget} />
      </div>
    </div>
  );
};

export default ContextBuilder;
