/**
 * Badge - 统一风格徽章组件
 */

import React from 'react';
import { colors, borderRadius, fontSize, fontWeight, spacing } from './tokens';

export interface BadgeProps {
  children: React.ReactNode;
  variant?: 'default' | 'primary' | 'success' | 'warning' | 'error' | 'info';
  size?: 'sm' | 'md' | 'lg';
  dot?: boolean;
  dotColor?: string;
  icon?: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
}

const sizeConfig = {
  sm: { padding: '2px 6px', fontSize: 9, dotSize: 4 },
  md: { padding: '3px 8px', fontSize: 10, dotSize: 6 },
  lg: { padding: '4px 10px', fontSize: 11, dotSize: 8 },
};

const variantConfig = {
  default: { bg: colors.slate[100], color: colors.slate[600], border: colors.slate[200] },
  primary: { bg: colors.primary[50], color: colors.primary[700], border: colors.primary[200] },
  success: { bg: colors.success.light, color: colors.success.dark, border: '#86efac' },
  warning: { bg: colors.warning.light, color: colors.warning.dark, border: '#fcd34d' },
  error: { bg: colors.error.light, color: colors.error.dark, border: '#fca5a5' },
  info: { bg: colors.info.light, color: colors.info.dark, border: colors.primary[200] },
};

export const Badge: React.FC<BadgeProps> = ({
  children,
  variant = 'default',
  size = 'md',
  dot = false,
  dotColor,
  icon,
  className,
  style,
}) => {
  const sizeStyles = sizeConfig[size];
  const variantStyles = variantConfig[variant];

  return (
    <span
      className={className}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: spacing[1],
        padding: sizeStyles.padding,
        fontSize: sizeStyles.fontSize,
        fontWeight: fontWeight.bold,
        textTransform: 'uppercase',
        letterSpacing: '0.05em',
        borderRadius: borderRadius.full,
        backgroundColor: variantStyles.bg,
        color: variantStyles.color,
        border: `1px solid ${variantStyles.border}`,
        ...style,
      }}
    >
      {dot && (
        <span
          style={{
            width: sizeStyles.dotSize,
            height: sizeStyles.dotSize,
            borderRadius: '50%',
            backgroundColor: dotColor || variantStyles.color,
          }}
        />
      )}
      {icon}
      {children}
    </span>
  );
};

export const StatusBadge: React.FC<{
  status: 'active' | 'inactive' | 'pending' | 'completed' | 'error';
  label?: string;
  size?: 'sm' | 'md' | 'lg';
}> = ({ status, label, size = 'md' }) => {
  const statusConfig = {
    active: { variant: 'success' as const, label: 'Active', dot: true, animate: true },
    inactive: { variant: 'default' as const, label: 'Inactive', dot: true, animate: false },
    pending: { variant: 'warning' as const, label: 'Pending', dot: true, animate: true },
    completed: { variant: 'primary' as const, label: 'Completed', dot: false, animate: false },
    error: { variant: 'error' as const, label: 'Error', dot: true, animate: false },
  };

  const config = statusConfig[status];

  return (
    <Badge variant={config.variant} size={size} dot={config.dot}>
      {config.animate && (
        <span
          style={{
            width: 6,
            height: 6,
            borderRadius: '50%',
            backgroundColor: 'currentColor',
            animation: 'pulse 1.5s infinite',
          }}
        />
      )}
      {label || config.label}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }
      `}</style>
    </Badge>
  );
};

export default Badge;
