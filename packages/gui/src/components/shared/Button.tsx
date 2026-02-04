/**
 * Button - 统一风格按钮组件
 */

import React from 'react';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from './tokens';

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'success';
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
  icon?: React.ReactNode;
  iconPosition?: 'left' | 'right';
  fullWidth?: boolean;
}

const sizeConfig = {
  sm: { padding: '6px 12px', fontSize: 11, iconSize: 14, gap: 4 },
  md: { padding: '8px 16px', fontSize: 13, iconSize: 16, gap: 6 },
  lg: { padding: '12px 24px', fontSize: 14, iconSize: 18, gap: 8 },
};

const variantConfig = {
  primary: {
    backgroundColor: colors.slate[900],
    color: '#fff',
    border: 'none',
    hoverBg: colors.slate[800],
    shadow: shadows.md,
  },
  secondary: {
    backgroundColor: '#fff',
    color: colors.slate[700],
    border: `1px solid ${colors.slate[200]}`,
    hoverBg: colors.slate[50],
    shadow: shadows.sm,
  },
  ghost: {
    backgroundColor: 'transparent',
    color: colors.slate[600],
    border: 'none',
    hoverBg: colors.slate[100],
    shadow: 'none',
  },
  danger: {
    backgroundColor: colors.error.main,
    color: '#fff',
    border: 'none',
    hoverBg: colors.error.dark,
    shadow: shadows.md,
  },
  success: {
    backgroundColor: colors.success.main,
    color: '#fff',
    border: 'none',
    hoverBg: colors.success.dark,
    shadow: shadows.md,
  },
};

export const Button: React.FC<ButtonProps> = ({
  variant = 'primary',
  size = 'md',
  loading = false,
  icon,
  iconPosition = 'left',
  fullWidth = false,
  disabled,
  children,
  style,
  ...props
}) => {
  const sizeStyles = sizeConfig[size];
  const variantStyles = variantConfig[variant];
  const [isHovered, setIsHovered] = React.useState(false);

  return (
    <button
      {...props}
      disabled={disabled || loading}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: sizeStyles.gap,
        padding: sizeStyles.padding,
        fontSize: sizeStyles.fontSize,
        fontWeight: fontWeight.bold,
        borderRadius: borderRadius.xl,
        cursor: disabled || loading ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.6 : 1,
        transition: transitions.fast,
        width: fullWidth ? '100%' : 'auto',
        backgroundColor: isHovered && !disabled ? variantStyles.hoverBg : variantStyles.backgroundColor,
        color: variantStyles.color,
        border: variantStyles.border,
        boxShadow: variantStyles.shadow,
        transform: isHovered && !disabled ? 'translateY(-1px)' : 'none',
        ...style,
      }}
    >
      {loading ? (
        <span
          style={{
            width: sizeStyles.iconSize,
            height: sizeStyles.iconSize,
            border: '2px solid currentColor',
            borderTopColor: 'transparent',
            borderRadius: '50%',
            animation: 'spin 0.6s linear infinite',
          }}
        />
      ) : (
        <>
          {icon && iconPosition === 'left' && icon}
          {children}
          {icon && iconPosition === 'right' && icon}
        </>
      )}
      <style>{`
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </button>
  );
};

export default Button;
