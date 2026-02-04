/**
 * CustomSelect - 自定义下拉选择器
 * 统一风格的下拉组件，替代原生 select
 */

import React, { useState, useRef, useEffect, useCallback } from 'react';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from './tokens';

export interface SelectOption {
  value: string;
  label: string;
  icon?: React.ReactNode;
  description?: string;
  disabled?: boolean;
  color?: string;
}

export interface CustomSelectProps {
  options: SelectOption[];
  value?: string;
  onChange?: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  loading?: boolean;
  searchable?: boolean;
  clearable?: boolean;
  size?: 'sm' | 'md' | 'lg';
  variant?: 'default' | 'outlined' | 'filled';
  error?: boolean;
  helperText?: string;
  label?: string;
  className?: string;
  style?: React.CSSProperties;
  dropdownStyle?: React.CSSProperties;
  renderOption?: (option: SelectOption, isSelected: boolean) => React.ReactNode;
  renderValue?: (option: SelectOption) => React.ReactNode;
}

const sizeConfig = {
  sm: { padding: '6px 10px', fontSize: 12, iconSize: 14 },
  md: { padding: '8px 12px', fontSize: 13, iconSize: 16 },
  lg: { padding: '10px 14px', fontSize: 14, iconSize: 18 },
};

export const CustomSelect: React.FC<CustomSelectProps> = ({
  options,
  value,
  onChange,
  placeholder = 'Select...',
  disabled = false,
  loading = false,
  searchable = false,
  clearable = false,
  size = 'md',
  variant = 'default',
  error = false,
  helperText,
  label,
  className,
  style,
  dropdownStyle,
  renderOption,
  renderValue,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const selectedOption = options.find(opt => opt.value === value);
  const sizeStyles = sizeConfig[size];

  // 筛选选项
  const filteredOptions = searchable && searchQuery
    ? options.filter(opt =>
        opt.label.toLowerCase().includes(searchQuery.toLowerCase()) ||
        opt.description?.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : options;

  // 点击外部关闭
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
        setSearchQuery('');
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // 键盘导航
  const handleKeyDown = useCallback((event: React.KeyboardEvent) => {
    if (disabled) return;

    switch (event.key) {
      case 'Enter':
        event.preventDefault();
        if (isOpen && highlightedIndex >= 0 && filteredOptions[highlightedIndex]) {
          const opt = filteredOptions[highlightedIndex];
          if (!opt.disabled) {
            onChange?.(opt.value);
            setIsOpen(false);
            setSearchQuery('');
          }
        } else {
          setIsOpen(!isOpen);
        }
        break;
      case 'Escape':
        setIsOpen(false);
        setSearchQuery('');
        break;
      case 'ArrowDown':
        event.preventDefault();
        if (!isOpen) {
          setIsOpen(true);
        } else {
          setHighlightedIndex(prev =>
            prev < filteredOptions.length - 1 ? prev + 1 : 0
          );
        }
        break;
      case 'ArrowUp':
        event.preventDefault();
        if (isOpen) {
          setHighlightedIndex(prev =>
            prev > 0 ? prev - 1 : filteredOptions.length - 1
          );
        }
        break;
    }
  }, [disabled, isOpen, highlightedIndex, filteredOptions, onChange]);

  const handleSelect = (opt: SelectOption) => {
    if (opt.disabled) return;
    onChange?.(opt.value);
    setIsOpen(false);
    setSearchQuery('');
  };

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation();
    onChange?.('');
  };

  const getVariantStyles = () => {
    const base = {
      display: 'flex',
      alignItems: 'center',
      gap: spacing[2],
      padding: sizeStyles.padding,
      fontSize: sizeStyles.fontSize,
      fontWeight: fontWeight.medium,
      borderRadius: borderRadius.xl,
      cursor: disabled ? 'not-allowed' : 'pointer',
      opacity: disabled ? 0.6 : 1,
      transition: transitions.fast,
      minWidth: 180,
    };

    switch (variant) {
      case 'outlined':
        return {
          ...base,
          backgroundColor: 'transparent',
          border: `1px solid ${error ? colors.error.main : isOpen ? colors.primary[400] : colors.slate[300]}`,
          color: colors.slate[700],
        };
      case 'filled':
        return {
          ...base,
          backgroundColor: colors.slate[100],
          border: `1px solid transparent`,
          color: colors.slate[700],
        };
      default:
        return {
          ...base,
          backgroundColor: '#fff',
          border: `1px solid ${error ? colors.error.main : isOpen ? colors.primary[400] : colors.slate[200]}`,
          boxShadow: isOpen ? `0 0 0 3px ${colors.primary[100]}` : shadows.sm,
          color: colors.slate[700],
        };
    }
  };

  return (
    <div
      ref={containerRef}
      className={className}
      style={{ position: 'relative', display: 'inline-block', ...style }}
    >
      {/* Label */}
      {label && (
        <label
          style={{
            display: 'block',
            marginBottom: spacing[1.5],
            fontSize: fontSize.xs,
            fontWeight: fontWeight.bold,
            color: colors.slate[700],
            textTransform: 'uppercase',
            letterSpacing: '0.05em',
          }}
        >
          {label}
        </label>
      )}

      {/* Trigger */}
      <div
        onClick={() => !disabled && setIsOpen(!isOpen)}
        onKeyDown={handleKeyDown}
        tabIndex={disabled ? -1 : 0}
        style={getVariantStyles()}
        role="combobox"
        aria-expanded={isOpen}
        aria-haspopup="listbox"
      >
        {loading ? (
          <span style={{ color: colors.slate[400] }}>Loading...</span>
        ) : selectedOption ? (
          renderValue ? (
            renderValue(selectedOption)
          ) : (
            <>
              {selectedOption.icon && (
                <span style={{ display: 'flex', alignItems: 'center' }}>
                  {selectedOption.icon}
                </span>
              )}
              <span style={{ flex: 1 }}>{selectedOption.label}</span>
              {selectedOption.color && (
                <span
                  style={{
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    backgroundColor: selectedOption.color,
                  }}
                />
              )}
            </>
          )
        ) : (
          <span style={{ color: colors.slate[400], flex: 1 }}>{placeholder}</span>
        )}

        {/* Clear button */}
        {clearable && selectedOption && !disabled && (
          <button
            onClick={handleClear}
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 16,
              height: 16,
              borderRadius: '50%',
              backgroundColor: colors.slate[200],
              border: 'none',
              cursor: 'pointer',
              fontSize: 10,
              color: colors.slate[500],
            }}
          >
            ×
          </button>
        )}

        {/* Arrow */}
        <span
          style={{
            marginLeft: 'auto',
            transform: isOpen ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: transitions.fast,
            fontSize: 10,
            color: colors.slate[400],
          }}
        >
          ▼
        </span>
      </div>

      {/* Helper text */}
      {helperText && (
        <span
          style={{
            display: 'block',
            marginTop: spacing[1],
            fontSize: fontSize.xs,
            color: error ? colors.error.main : colors.slate[500],
          }}
        >
          {helperText}
        </span>
      )}

      {/* Dropdown */}
      {isOpen && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            marginTop: spacing[1],
            backgroundColor: '#fff',
            border: `1px solid ${colors.slate[200]}`,
            borderRadius: borderRadius.xl,
            boxShadow: shadows.xl,
            zIndex: 1000,
            overflow: 'hidden',
            animation: 'slideDown 0.15s ease',
            ...dropdownStyle,
          }}
          role="listbox"
        >
          {/* Search input */}
          {searchable && (
            <div style={{ padding: spacing[2], borderBottom: `1px solid ${colors.slate[100]}` }}>
              <input
                ref={inputRef}
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search..."
                autoFocus
                style={{
                  width: '100%',
                  padding: `${spacing[1.5]}px ${spacing[2.5]}px`,
                  fontSize: fontSize.sm,
                  border: `1px solid ${colors.slate[200]}`,
                  borderRadius: borderRadius.lg,
                  backgroundColor: colors.slate[50],
                  outline: 'none',
                }}
              />
            </div>
          )}

          {/* Options list */}
          <div style={{ maxHeight: 280, overflowY: 'auto' }}>
            {filteredOptions.length === 0 ? (
              <div
                style={{
                  padding: spacing[4],
                  textAlign: 'center',
                  color: colors.slate[400],
                  fontSize: fontSize.sm,
                }}
              >
                No options found
              </div>
            ) : (
              filteredOptions.map((opt, index) => {
                const isSelected = opt.value === value;
                const isHighlighted = index === highlightedIndex;

                if (renderOption) {
                  return (
                    <div
                      key={opt.value}
                      onClick={() => handleSelect(opt)}
                      onMouseEnter={() => setHighlightedIndex(index)}
                    >
                      {renderOption(opt, isSelected)}
                    </div>
                  );
                }

                return (
                  <div
                    key={opt.value}
                    onClick={() => handleSelect(opt)}
                    onMouseEnter={() => setHighlightedIndex(index)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: spacing[2.5],
                      padding: `${spacing[2.5]}px ${spacing[3]}px`,
                      cursor: opt.disabled ? 'not-allowed' : 'pointer',
                      opacity: opt.disabled ? 0.5 : 1,
                      backgroundColor: isSelected
                        ? colors.primary[50]
                        : isHighlighted
                        ? colors.slate[50]
                        : 'transparent',
                      borderLeft: isSelected ? `3px solid ${colors.primary[500]}` : '3px solid transparent',
                      transition: transitions.fast,
                    }}
                    role="option"
                    aria-selected={isSelected}
                  >
                    {opt.icon && (
                      <span
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          width: 24,
                          height: 24,
                          borderRadius: borderRadius.md,
                          backgroundColor: opt.color ? `${opt.color}15` : colors.slate[100],
                          color: opt.color || colors.slate[600],
                        }}
                      >
                        {opt.icon}
                      </span>
                    )}
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div
                        style={{
                          fontSize: fontSize.sm,
                          fontWeight: isSelected ? fontWeight.semibold : fontWeight.medium,
                          color: isSelected ? colors.primary[700] : colors.slate[700],
                        }}
                      >
                        {opt.label}
                      </div>
                      {opt.description && (
                        <div
                          style={{
                            fontSize: fontSize.xs,
                            color: colors.slate[500],
                            marginTop: 2,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {opt.description}
                        </div>
                      )}
                    </div>
                    {isSelected && (
                      <span style={{ color: colors.primary[500], fontSize: 14 }}>✓</span>
                    )}
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      {/* Animation keyframes */}
      <style>{`
        @keyframes slideDown {
          from {
            opacity: 0;
            transform: translateY(-8px);
          }
          to {
            opacity: 1;
            transform: translateY(0);
          }
        }
      `}</style>
    </div>
  );
};

export default CustomSelect;
