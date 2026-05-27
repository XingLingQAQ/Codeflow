/**
 * ModelFilter - 模型筛选组件
 * 支持按提供商和能力筛选
 */

import React from 'react';
import { ModelCapability, ModelProvider } from '@codeflow/core';
import {
  ModelFilterProps,
  PROVIDER_ICONS,
  PROVIDER_NAMES,
  CAPABILITY_NAMES,
  CAPABILITY_COLORS,
} from './types';

export const ModelFilter: React.FC<ModelFilterProps> = ({
  providers,
  capabilities,
  selectedProvider,
  selectedCapabilities = [],
  onProviderChange,
  onCapabilitiesChange,
  className,
}) => {
  const handleCapabilityToggle = (cap: ModelCapability) => {
    if (selectedCapabilities.includes(cap)) {
      onCapabilitiesChange(selectedCapabilities.filter(c => c !== cap));
    } else {
      onCapabilitiesChange([...selectedCapabilities, cap]);
    }
  };

  return (
    <div className={className} style={{ padding: '8px 12px' }}>
      {/* 提供商筛选 */}
      <div style={{ marginBottom: 12 }}>
        <div
          style={{
            fontSize: 11,
            fontWeight: 600,
            color: '#666',
            marginBottom: 6,
            textTransform: 'uppercase',
          }}
        >
          Provider
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
          <button
            onClick={() => onProviderChange(undefined)}
            style={{
              padding: '4px 10px',
              fontSize: 12,
              border: '1px solid #ddd',
              borderRadius: 4,
              backgroundColor: !selectedProvider ? '#2196F3' : '#fff',
              color: !selectedProvider ? '#fff' : '#333',
              cursor: 'pointer',
              transition: 'all 0.15s',
            }}
          >
            All
          </button>
          {providers.map(provider => (
            <button
              key={provider}
              onClick={() => onProviderChange(provider)}
              style={{
                padding: '4px 10px',
                fontSize: 12,
                border: '1px solid #ddd',
                borderRadius: 4,
                backgroundColor: selectedProvider === provider ? '#2196F3' : '#fff',
                color: selectedProvider === provider ? '#fff' : '#333',
                cursor: 'pointer',
                transition: 'all 0.15s',
                display: 'flex',
                alignItems: 'center',
                gap: 4,
              }}
            >
              <span>{PROVIDER_ICONS[provider]}</span>
              <span>{PROVIDER_NAMES[provider]}</span>
            </button>
          ))}
        </div>
      </div>

      {/* 能力筛选 */}
      <div>
        <div
          style={{
            fontSize: 11,
            fontWeight: 600,
            color: '#666',
            marginBottom: 6,
            textTransform: 'uppercase',
          }}
        >
          Capabilities
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
          {capabilities.map(cap => {
            const isSelected = selectedCapabilities.includes(cap);
            return (
              <button
                key={cap}
                onClick={() => handleCapabilityToggle(cap)}
                style={{
                  padding: '4px 8px',
                  fontSize: 11,
                  border: `1px solid ${isSelected ? CAPABILITY_COLORS[cap] : '#ddd'}`,
                  borderRadius: 4,
                  backgroundColor: isSelected ? `${CAPABILITY_COLORS[cap]}15` : '#fff',
                  color: isSelected ? CAPABILITY_COLORS[cap] : '#666',
                  cursor: 'pointer',
                  transition: 'all 0.15s',
                  fontWeight: isSelected ? 500 : 400,
                }}
              >
                {CAPABILITY_NAMES[cap]}
              </button>
            );
          })}
        </div>
      </div>

      {/* 清除筛选 */}
      {(selectedProvider || selectedCapabilities.length > 0) && (
        <button
          onClick={() => {
            onProviderChange(undefined);
            onCapabilitiesChange([]);
          }}
          style={{
            marginTop: 12,
            padding: '6px 12px',
            fontSize: 12,
            border: 'none',
            borderRadius: 4,
            backgroundColor: '#f5f5f5',
            color: '#666',
            cursor: 'pointer',
            width: '100%',
          }}
        >
          Clear Filters
        </button>
      )}
    </div>
  );
};

export default ModelFilter;
