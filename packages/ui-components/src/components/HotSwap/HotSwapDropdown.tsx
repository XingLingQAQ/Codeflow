/**
 * 模型热切换下拉菜单组件
 */

import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  HotSwapDropdownProps,
  ModelOption,
  StatusIndicatorProps,
  STATUS_COLORS,
  PROVIDER_ICONS,
} from './types';

/**
 * 状态指示器
 */
const StatusIndicator: React.FC<StatusIndicatorProps> = ({ status, size = 'small' }) => {
  const sizes = {
    small: 8,
    medium: 10,
    large: 12,
  };

  return (
    <span
      style={{
        display: 'inline-block',
        width: sizes[size],
        height: sizes[size],
        borderRadius: '50%',
        backgroundColor: STATUS_COLORS[status],
        animation: status === 'switching' ? 'pulse 1s infinite' : undefined,
      }}
    />
  );
};

/**
 * 模型选项项
 */
const ModelOptionItem: React.FC<{
  model: ModelOption;
  isSelected: boolean;
  onClick: () => void;
}> = ({ model, isSelected, onClick }) => {
  const [isHovered, setIsHovered] = useState(false);

  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
        display: 'flex',
        alignItems: 'center',
        padding: '10px 12px',
        cursor: model.status === 'offline' ? 'not-allowed' : 'pointer',
        backgroundColor: isSelected
          ? 'rgba(33, 150, 243, 0.1)'
          : isHovered
          ? 'rgba(0, 0, 0, 0.04)'
          : 'transparent',
        opacity: model.status === 'offline' ? 0.5 : 1,
        borderLeft: isSelected ? '3px solid #2196F3' : '3px solid transparent',
        transition: 'background-color 0.15s',
      }}
    >
      {/* Provider 图标 */}
      <span style={{ fontSize: 16, marginRight: 10 }}>
        {PROVIDER_ICONS[model.provider] || PROVIDER_ICONS.custom}
      </span>

      {/* 模型信息 */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
          }}
        >
          <span
            style={{
              fontSize: 13,
              fontWeight: isSelected ? 600 : 400,
              color: '#333',
            }}
          >
            {model.name}
          </span>
          <StatusIndicator status={model.status} />
        </div>
        <div
          style={{
            fontSize: 11,
            color: '#666',
            marginTop: 2,
          }}
        >
          {Math.round(model.contextWindow / 1000)}K context
          {model.capabilities.length > 0 && (
            <span style={{ marginLeft: 8 }}>
              {model.capabilities.slice(0, 2).join(' · ')}
            </span>
          )}
        </div>
      </div>

      {/* 选中标记 */}
      {isSelected && (
        <span style={{ color: '#2196F3', fontSize: 14 }}>✓</span>
      )}
    </div>
  );
};

/**
 * 热切换下拉菜单
 */
export const HotSwapDropdown: React.FC<HotSwapDropdownProps> = ({
  models,
  currentModelId,
  disabled = false,
  loading = false,
  className,
  style,
  onModelSelect,
  onRetry,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const currentModel = models.find(m => m.id === currentModelId);

  // 点击外部关闭
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handleSelect = useCallback(
    (modelId: string) => {
      const model = models.find(m => m.id === modelId);
      if (model?.status === 'offline') return;

      onModelSelect(modelId);
      setIsOpen(false);
    },
    [models, onModelSelect]
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsOpen(false);
      } else if (event.key === 'Enter' || event.key === ' ') {
        setIsOpen(!isOpen);
      }
    },
    [isOpen]
  );

  return (
    <div
      ref={containerRef}
      className={className}
      style={{
        position: 'relative',
        display: 'inline-block',
        ...style,
      }}
    >
      {/* 触发按钮 */}
      <button
        onClick={() => !disabled && setIsOpen(!isOpen)}
        onKeyDown={handleKeyDown}
        disabled={disabled}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '8px 12px',
          backgroundColor: '#fff',
          border: '1px solid #ddd',
          borderRadius: 6,
          cursor: disabled ? 'not-allowed' : 'pointer',
          opacity: disabled ? 0.6 : 1,
          minWidth: 180,
          fontSize: 13,
        }}
      >
        {loading ? (
          <span style={{ color: '#666' }}>Switching...</span>
        ) : currentModel ? (
          <>
            <span>{PROVIDER_ICONS[currentModel.provider] || '⚙️'}</span>
            <span style={{ flex: 1, textAlign: 'left' }}>{currentModel.name}</span>
            <StatusIndicator status={currentModel.status} />
          </>
        ) : (
          <span style={{ color: '#999' }}>Select model</span>
        )}
        <span
          style={{
            marginLeft: 'auto',
            transform: isOpen ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.2s',
          }}
        >
          ▼
        </span>
      </button>

      {/* 下拉菜单 */}
      {isOpen && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            marginTop: 4,
            backgroundColor: '#fff',
            border: '1px solid #ddd',
            borderRadius: 6,
            boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
            zIndex: 1000,
            maxHeight: 300,
            overflowY: 'auto',
          }}
        >
          {/* 模型列表 */}
          {models.map(model => (
            <ModelOptionItem
              key={model.id}
              model={model}
              isSelected={model.id === currentModelId}
              onClick={() => handleSelect(model.id)}
            />
          ))}

          {/* 重试按钮 */}
          {onRetry && currentModel?.status === 'degraded' && (
            <div
              style={{
                borderTop: '1px solid #eee',
                padding: '8px 12px',
              }}
            >
              <button
                onClick={() => {
                  onRetry();
                  setIsOpen(false);
                }}
                style={{
                  width: '100%',
                  padding: '8px',
                  backgroundColor: '#f5f5f5',
                  border: '1px solid #ddd',
                  borderRadius: 4,
                  cursor: 'pointer',
                  fontSize: 12,
                  color: '#666',
                }}
              >
                🔄 Retry current model
              </button>
            </div>
          )}
        </div>
      )}

      {/* 脉冲动画样式 */}
      <style>
        {`
          @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
          }
        `}
      </style>
    </div>
  );
};

export default HotSwapDropdown;
