/**
 * ProgressBar - 进度条组件
 * 支持 0-100% 和状态颜色
 */

import React, { useEffect, useState } from 'react';
import {
  colors,
  spacing,
  borderRadius,
  fontSize,
  fontWeight,
  transitions,
  keyframes,
} from './tokens';

export type ProgressStatus = 'default' | 'success' | 'warning' | 'error' | 'info';

export interface ProgressBarProps {
  value: number;
  max?: number;
  status?: ProgressStatus;
  showLabel?: boolean;
  labelPosition?: 'inside' | 'outside' | 'none';
  size?: 'sm' | 'md' | 'lg';
  animated?: boolean;
  striped?: boolean;
  className?: string;
  style?: React.CSSProperties;
}

const getStatusColor = (status: ProgressStatus) => {
  switch (status) {
    case 'success':
      return colors.success.main;
    case 'warning':
      return colors.warning.main;
    case 'error':
      return colors.error.main;
    case 'info':
      return colors.info.main;
    case 'default':
    default:
      return colors.primary[500];
  }
};

const getStatusBgColor = (status: ProgressStatus) => {
  switch (status) {
    case 'success':
      return colors.success.light;
    case 'warning':
      return colors.warning.light;
    case 'error':
      return colors.error.light;
    case 'info':
      return colors.info.light;
    case 'default':
    default:
      return colors.primary[100];
  }
};

const getSizeStyles = (size: ProgressBarProps['size']) => {
  switch (size) {
    case 'sm':
      return { height: 4, fontSize: fontSize.xs };
    case 'lg':
      return { height: 16, fontSize: fontSize.sm };
    case 'md':
    default:
      return { height: 8, fontSize: fontSize.xs };
  }
};

// Inject keyframes for striped animation
const injectStripedKeyframes = () => {
  const styleId = 'progress-striped-keyframes';
  if (typeof document !== 'undefined' && !document.getElementById(styleId)) {
    const style = document.createElement('style');
    style.id = styleId;
    style.textContent = `
      @keyframes progress-stripes {
        from { background-position: 1rem 0; }
        to { background-position: 0 0; }
      }
    `;
    document.head.appendChild(style);
  }
};

export const ProgressBar: React.FC<ProgressBarProps> = ({
  value,
  max = 100,
  status = 'default',
  showLabel = false,
  labelPosition = 'outside',
  size = 'md',
  animated = false,
  striped = false,
  className,
  style,
}) => {
  const [displayValue, setDisplayValue] = useState(0);
  const percentage = Math.min(Math.max((value / max) * 100, 0), 100);
  const sizeStyles = getSizeStyles(size);
  const statusColor = getStatusColor(status);
  const statusBgColor = getStatusBgColor(status);

  useEffect(() => {
    if (striped) {
      injectStripedKeyframes();
    }
  }, [striped]);

  // Animate value change
  useEffect(() => {
    if (animated) {
      const timer = setTimeout(() => {
        setDisplayValue(percentage);
      }, 50);
      return () => clearTimeout(timer);
    } else {
      setDisplayValue(percentage);
    }
  }, [percentage, animated]);

  const stripedStyle: React.CSSProperties = striped
    ? {
        backgroundImage: `linear-gradient(
          45deg,
          rgba(255, 255, 255, 0.15) 25%,
          transparent 25%,
          transparent 50%,
          rgba(255, 255, 255, 0.15) 50%,
          rgba(255, 255, 255, 0.15) 75%,
          transparent 75%,
          transparent
        )`,
        backgroundSize: '1rem 1rem',
        animation: animated ? 'progress-stripes 1s linear infinite' : undefined,
      }
    : {};

  const renderLabel = () => {
    if (!showLabel || labelPosition === 'none') return null;

    const labelText = `${Math.round(percentage)}%`;

    if (labelPosition === 'inside' && size === 'lg') {
      return null; // Will be rendered inside the bar
    }

    return (
      <span
        style={{
          fontSize: sizeStyles.fontSize,
          fontWeight: fontWeight.medium,
          color: colors.slate[600],
          marginLeft: spacing[2],
          minWidth: 36,
          textAlign: 'right',
        }}
      >
        {labelText}
      </span>
    );
  };

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        alignItems: 'center',
        width: '100%',
        ...style,
      }}
    >
      <div
        style={{
          flex: 1,
          height: sizeStyles.height,
          backgroundColor: statusBgColor,
          borderRadius: borderRadius.full,
          overflow: 'hidden',
          position: 'relative',
        }}
        role="progressbar"
        aria-valuenow={value}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-label={`Progress: ${Math.round(percentage)}%`}
      >
        <div
          style={{
            height: '100%',
            width: `${displayValue}%`,
            backgroundColor: statusColor,
            borderRadius: borderRadius.full,
            transition: animated ? 'width 0.3s ease-out' : 'none',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-end',
            paddingRight: size === 'lg' && showLabel && labelPosition === 'inside' ? spacing[2] : 0,
            ...stripedStyle,
          }}
        >
          {size === 'lg' && showLabel && labelPosition === 'inside' && displayValue > 10 && (
            <span
              style={{
                fontSize: sizeStyles.fontSize,
                fontWeight: fontWeight.bold,
                color: '#fff',
              }}
            >
              {Math.round(percentage)}%
            </span>
          )}
        </div>
      </div>
      {labelPosition === 'outside' && renderLabel()}
    </div>
  );
};

// Circular Progress variant
export interface CircularProgressProps {
  value: number;
  max?: number;
  size?: number;
  strokeWidth?: number;
  status?: ProgressStatus;
  showLabel?: boolean;
  className?: string;
  style?: React.CSSProperties;
}

export const CircularProgress: React.FC<CircularProgressProps> = ({
  value,
  max = 100,
  size = 48,
  strokeWidth = 4,
  status = 'default',
  showLabel = true,
  className,
  style,
}) => {
  const percentage = Math.min(Math.max((value / max) * 100, 0), 100);
  const statusColor = getStatusColor(status);
  const statusBgColor = getStatusBgColor(status);

  const radius = (size - strokeWidth) / 2;
  const circumference = radius * 2 * Math.PI;
  const offset = circumference - (percentage / 100) * circumference;

  return (
    <div
      className={className}
      style={{
        position: 'relative',
        width: size,
        height: size,
        ...style,
      }}
      role="progressbar"
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
    >
      <svg
        width={size}
        height={size}
        style={{ transform: 'rotate(-90deg)' }}
      >
        {/* Background circle */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={statusBgColor}
          strokeWidth={strokeWidth}
        />
        {/* Progress circle */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={statusColor}
          strokeWidth={strokeWidth}
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          style={{ transition: 'stroke-dashoffset 0.3s ease-out' }}
        />
      </svg>
      {showLabel && (
        <div
          style={{
            position: 'absolute',
            top: '50%',
            left: '50%',
            transform: 'translate(-50%, -50%)',
            fontSize: size > 40 ? fontSize.sm : fontSize.xs,
            fontWeight: fontWeight.semibold,
            color: colors.slate[700],
          }}
        >
          {Math.round(percentage)}%
        </div>
      )}
    </div>
  );
};

export default ProgressBar;
