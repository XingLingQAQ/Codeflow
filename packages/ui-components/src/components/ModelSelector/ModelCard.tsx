/**
 * ModelCard - 模型卡片组件
 * 显示模型名称、提供商、成本和能力
 */

import React, { useState } from 'react';
import {
  ModelCardProps,
  PROVIDER_ICONS,
  CAPABILITY_NAMES,
  CAPABILITY_COLORS,
  formatCost,
  formatContextWindow,
} from './types';

export const ModelCard: React.FC<ModelCardProps> = ({
  model,
  isSelected = false,
  onClick,
  showCost = true,
  showCapabilities = true,
  maxCapabilitiesToShow = 3,
}) => {
  const [isHovered, setIsHovered] = useState(false);

  const displayCapabilities = model.capabilities.slice(0, maxCapabilitiesToShow);
  const remainingCount = model.capabilities.length - maxCapabilitiesToShow;

  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
        display: 'flex',
        flexDirection: 'column',
        padding: '12px 14px',
        cursor: onClick ? 'pointer' : 'default',
        backgroundColor: isSelected
          ? 'rgba(33, 150, 243, 0.08)'
          : isHovered
          ? 'rgba(0, 0, 0, 0.03)'
          : 'transparent',
        borderLeft: isSelected ? '3px solid #2196F3' : '3px solid transparent',
        borderBottom: '1px solid #f0f0f0',
        transition: 'background-color 0.15s',
        opacity: model.deprecated ? 0.6 : 1,
      }}
    >
      {/* 顶部行：图标、名称、选中标记 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        {/* Provider 图标 */}
        <span style={{ fontSize: 18 }}>
          {PROVIDER_ICONS[model.provider]}
        </span>

        {/* 模型名称 */}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 14,
              fontWeight: isSelected ? 600 : 500,
              color: '#333',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {model.name}
            {model.deprecated && (
              <span
                style={{
                  marginLeft: 6,
                  fontSize: 10,
                  color: '#999',
                  fontWeight: 400,
                }}
              >
                (deprecated)
              </span>
            )}
          </div>
        </div>

        {/* 成本和上下文 */}
        {showCost && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'flex-end',
              fontSize: 11,
              color: '#666',
            }}
          >
            <span style={{ fontWeight: 500 }}>{formatCost(model.cost)}</span>
            <span>{formatContextWindow(model.contextWindow)} ctx</span>
          </div>
        )}

        {/* 选中标记 */}
        {isSelected && (
          <span style={{ color: '#2196F3', fontSize: 16, marginLeft: 4 }}>✓</span>
        )}
      </div>

      {/* 描述 */}
      {model.description && (
        <div
          style={{
            fontSize: 12,
            color: '#888',
            marginTop: 6,
            marginLeft: 28,
            lineHeight: 1.4,
          }}
        >
          {model.description}
        </div>
      )}

      {/* 能力标签 */}
      {showCapabilities && model.capabilities.length > 0 && (
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 4,
            marginTop: 8,
            marginLeft: 28,
          }}
        >
          {displayCapabilities.map(cap => (
            <span
              key={cap}
              style={{
                fontSize: 10,
                padding: '2px 6px',
                borderRadius: 3,
                backgroundColor: `${CAPABILITY_COLORS[cap]}15`,
                color: CAPABILITY_COLORS[cap],
                fontWeight: 500,
              }}
            >
              {CAPABILITY_NAMES[cap]}
            </span>
          ))}
          {remainingCount > 0 && (
            <span
              style={{
                fontSize: 10,
                padding: '2px 6px',
                borderRadius: 3,
                backgroundColor: '#f0f0f0',
                color: '#666',
              }}
            >
              +{remainingCount}
            </span>
          )}
        </div>
      )}
    </div>
  );
};

export default ModelCard;
