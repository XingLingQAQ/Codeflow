/**
 * ModelSelector - 模型选择器组件
 * 支持搜索、筛选和选择模型
 */

import React, { useState, useRef, useEffect, useCallback, useMemo } from 'react';
import { ModelCapability, ModelProvider } from '@codeflow/core';
import {
  ModelSelectorProps,
  ModelOption,
  PROVIDER_ICONS,
  formatCost,
  formatContextWindow,
} from './types';
import { ModelCard } from './ModelCard';
import { ModelFilter } from './ModelFilter';
import { ModelSearch } from './ModelSearch';

export const ModelSelector: React.FC<ModelSelectorProps> = ({
  models,
  selectedModelId,
  onSelect,
  disabled = false,
  loading = false,
  placeholder = 'Select a model',
  className,
  style,
  showCost = true,
  showCapabilities = true,
  maxCapabilitiesToShow = 3,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedProvider, setSelectedProvider] = useState<ModelProvider | undefined>();
  const [selectedCapabilities, setSelectedCapabilities] = useState<ModelCapability[]>([]);
  const [showFilters, setShowFilters] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const selectedModel = models.find(m => m.id === selectedModelId);

  // 获取可用的提供商和能力
  const availableProviders = useMemo(() => {
    const providers = new Set<ModelProvider>();
    models.forEach(m => providers.add(m.provider));
    return Array.from(providers);
  }, [models]);

  const availableCapabilities = useMemo(() => {
    const caps = new Set<ModelCapability>();
    models.forEach(m => m.capabilities.forEach(c => caps.add(c)));
    return Array.from(caps);
  }, [models]);

  // 筛选模型
  const filteredModels = useMemo(() => {
    return models.filter(model => {
      // 搜索过滤
      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        const matchesName = model.name.toLowerCase().includes(query);
        const matchesProvider = model.provider.toLowerCase().includes(query);
        const matchesDescription = model.description?.toLowerCase().includes(query);
        if (!matchesName && !matchesProvider && !matchesDescription) {
          return false;
        }
      }

      // 提供商过滤
      if (selectedProvider && model.provider !== selectedProvider) {
        return false;
      }

      // 能力过滤
      if (selectedCapabilities.length > 0) {
        const hasAllCaps = selectedCapabilities.every(cap =>
          model.capabilities.includes(cap)
        );
        if (!hasAllCaps) {
          return false;
        }
      }

      return true;
    });
  }, [models, searchQuery, selectedProvider, selectedCapabilities]);

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
      onSelect(modelId);
      setIsOpen(false);
    },
    [onSelect]
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Escape') {
        setIsOpen(false);
      } else if (event.key === 'Enter' || event.key === ' ') {
        if (!disabled) {
          setIsOpen(!isOpen);
        }
      }
    },
    [isOpen, disabled]
  );

  const hasActiveFilters = selectedProvider || selectedCapabilities.length > 0;

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
          minWidth: 220,
          fontSize: 13,
          textAlign: 'left',
        }}
      >
        {loading ? (
          <span style={{ color: '#666' }}>Loading...</span>
        ) : selectedModel ? (
          <>
            <span>{PROVIDER_ICONS[selectedModel.provider]}</span>
            <span style={{ flex: 1 }}>{selectedModel.name}</span>
            {showCost && (
              <span style={{ fontSize: 11, color: '#666' }}>
                {formatCost(selectedModel.cost)}
              </span>
            )}
          </>
        ) : (
          <span style={{ color: '#999', flex: 1 }}>{placeholder}</span>
        )}
        <span
          style={{
            marginLeft: 'auto',
            transform: isOpen ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.2s',
            fontSize: 10,
          }}
        >
          ▼
        </span>
      </button>

      {/* 下拉面板 */}
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
            borderRadius: 8,
            boxShadow: '0 4px 16px rgba(0, 0, 0, 0.12)',
            zIndex: 1000,
            minWidth: 320,
            maxHeight: 480,
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          {/* 搜索栏 */}
          <ModelSearch
            value={searchQuery}
            onChange={setSearchQuery}
            placeholder="Search models..."
          />

          {/* 筛选切换按钮 */}
          <div
            style={{
              padding: '6px 12px',
              borderBottom: '1px solid #eee',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <button
              onClick={() => setShowFilters(!showFilters)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 4,
                padding: '4px 8px',
                fontSize: 12,
                border: '1px solid #ddd',
                borderRadius: 4,
                backgroundColor: showFilters || hasActiveFilters ? '#f0f7ff' : '#fff',
                color: hasActiveFilters ? '#2196F3' : '#666',
                cursor: 'pointer',
              }}
            >
              <span>🔧</span>
              <span>Filters</span>
              {hasActiveFilters && (
                <span
                  style={{
                    backgroundColor: '#2196F3',
                    color: '#fff',
                    borderRadius: '50%',
                    width: 16,
                    height: 16,
                    fontSize: 10,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                  }}
                >
                  {(selectedProvider ? 1 : 0) + selectedCapabilities.length}
                </span>
              )}
            </button>
            <span style={{ fontSize: 11, color: '#999' }}>
              {filteredModels.length} of {models.length} models
            </span>
          </div>

          {/* 筛选面板 */}
          {showFilters && (
            <div style={{ borderBottom: '1px solid #eee' }}>
              <ModelFilter
                providers={availableProviders}
                capabilities={availableCapabilities}
                selectedProvider={selectedProvider}
                selectedCapabilities={selectedCapabilities}
                onProviderChange={setSelectedProvider}
                onCapabilitiesChange={setSelectedCapabilities}
              />
            </div>
          )}

          {/* 模型列表 */}
          <div
            style={{
              flex: 1,
              overflowY: 'auto',
              maxHeight: showFilters ? 240 : 340,
            }}
          >
            {filteredModels.length === 0 ? (
              <div
                style={{
                  padding: 24,
                  textAlign: 'center',
                  color: '#999',
                  fontSize: 13,
                }}
              >
                No models match your criteria
              </div>
            ) : (
              filteredModels.map(model => (
                <ModelCard
                  key={model.id}
                  model={model}
                  isSelected={model.id === selectedModelId}
                  onClick={() => handleSelect(model.id)}
                  showCost={showCost}
                  showCapabilities={showCapabilities}
                  maxCapabilitiesToShow={maxCapabilitiesToShow}
                />
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default ModelSelector;
