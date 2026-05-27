/**
 * ModelSearch - 模型搜索组件
 */

import React from 'react';
import { ModelSearchProps } from './types';

export const ModelSearch: React.FC<ModelSearchProps> = ({
  value,
  onChange,
  placeholder = 'Search models...',
  className,
}) => {
  return (
    <div
      className={className}
      style={{
        padding: '8px 12px',
        borderBottom: '1px solid #eee',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          backgroundColor: '#f5f5f5',
          borderRadius: 6,
          padding: '6px 10px',
        }}
      >
        <span style={{ color: '#999', marginRight: 8 }}>🔍</span>
        <input
          type="text"
          value={value}
          onChange={e => onChange(e.target.value)}
          placeholder={placeholder}
          style={{
            flex: 1,
            border: 'none',
            backgroundColor: 'transparent',
            fontSize: 13,
            outline: 'none',
            color: '#333',
          }}
        />
        {value && (
          <button
            onClick={() => onChange('')}
            style={{
              border: 'none',
              backgroundColor: 'transparent',
              cursor: 'pointer',
              color: '#999',
              fontSize: 14,
              padding: 0,
              marginLeft: 4,
            }}
          >
            ✕
          </button>
        )}
      </div>
    </div>
  );
};

export default ModelSearch;
