/**
 * Toggle - 统一风格开关组件
 */

import React from 'react';
import { colors, borderRadius, transitions } from './tokens';

export interface ToggleProps {
  checked?: boolean;
  onChange?: (checked: boolean) => void;
  disabled?: boolean;
  size?: 'sm' | 'md' | 'lg';
  label?: string;
  labelPosition?: 'left' | 'right';
  className?: string;
  style?: React.CSSProperties;
}

const sizeConfig = {
  sm: { width: 32, height: 18, thumbSize: 14 },
  md: { width: 44, height: 24, thumbSize: 18 },
  lg: { width: 56, height: 30, thumbSize: 24 },
};

export const Toggle: React.FC<ToggleProps> = ({
  checked = false,
  onChange,
  disabled = false,
  size = 'md',
  label,
  labelPosition = 'right',
  className,
  style,
}) => {
  const sizeStyles = sizeConfig[size];

  const handleClick = () => {
    if (!disabled) {
      onChange?.(!checked);
    }
  };

  const toggle = (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={handleClick}
      style={{
        position: 'relative',
        width: sizeStyles.width,
        height: sizeStyles.height,
        borderRadius: borderRadius.full,
        backgroundColor: checked ? colors.primary[500] : colors.slate[200],
        border: 'none',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.6 : 1,
        transition: transitions.fast,
        padding: 0,
      }}
    >
      <span
        style={{
          position: 'absolute',
          top: (sizeStyles.height - sizeStyles.thumbSize) / 2,
          left: checked
            ? sizeStyles.width - sizeStyles.thumbSize - (sizeStyles.height - sizeStyles.thumbSize) / 2
            : (sizeStyles.height - sizeStyles.thumbSize) / 2,
          width: sizeStyles.thumbSize,
          height: sizeStyles.thumbSize,
          borderRadius: '50%',
          backgroundColor: '#fff',
          boxShadow: '0 1px 3px rgba(0, 0, 0, 0.2)',
          transition: transitions.fast,
        }}
      />
    </button>
  );

  if (!label) return toggle;

  return (
    <label
      className={className}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        cursor: disabled ? 'not-allowed' : 'pointer',
        ...style,
      }}
    >
      {labelPosition === 'left' && (
        <span style={{ fontSize: 13, color: colors.slate[700] }}>{label}</span>
      )}
      {toggle}
      {labelPosition === 'right' && (
        <span style={{ fontSize: 13, color: colors.slate[700] }}>{label}</span>
      )}
    </label>
  );
};

export default Toggle;
