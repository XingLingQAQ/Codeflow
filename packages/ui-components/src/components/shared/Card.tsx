/**
 * Card - 统一风格卡片组件
 */

import React from 'react';
import { colors, borderRadius, shadows, spacing, transitions } from './tokens';

export interface CardProps {
  children: React.ReactNode;
  variant?: 'default' | 'outlined' | 'elevated' | 'glass';
  padding?: 'none' | 'sm' | 'md' | 'lg';
  hoverable?: boolean;
  selected?: boolean;
  onClick?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

const paddingConfig = {
  none: 0,
  sm: spacing[3],
  md: spacing[4],
  lg: spacing[6],
};

export const Card: React.FC<CardProps> = ({
  children,
  variant = 'default',
  padding = 'md',
  hoverable = false,
  selected = false,
  onClick,
  className,
  style,
}) => {
  const [isHovered, setIsHovered] = React.useState(false);

  const getVariantStyles = (): React.CSSProperties => {
    const base: React.CSSProperties = {
      borderRadius: borderRadius['2xl'],
      transition: transitions.normal,
      cursor: onClick || hoverable ? 'pointer' : 'default',
    };

    switch (variant) {
      case 'outlined':
        return {
          ...base,
          backgroundColor: '#fff',
          border: `1px solid ${selected ? colors.primary[400] : colors.slate[200]}`,
          boxShadow: selected ? `0 0 0 3px ${colors.primary[100]}` : 'none',
        };
      case 'elevated':
        return {
          ...base,
          backgroundColor: '#fff',
          border: 'none',
          boxShadow: isHovered && hoverable ? shadows.xl : shadows.lg,
        };
      case 'glass':
        return {
          ...base,
          backgroundColor: 'rgba(255, 255, 255, 0.8)',
          backdropFilter: 'blur(12px)',
          border: '1px solid rgba(255, 255, 255, 0.5)',
          boxShadow: shadows.lg,
        };
      default:
        return {
          ...base,
          backgroundColor: selected ? colors.primary[50] : '#fff',
          border: `1px solid ${selected ? colors.primary[300] : isHovered && hoverable ? colors.primary[200] : colors.slate[200]}`,
          boxShadow: isHovered && hoverable ? shadows.lg : shadows.sm,
        };
    }
  };

  return (
    <div
      className={className}
      onClick={onClick}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
        ...getVariantStyles(),
        padding: paddingConfig[padding],
        transform: isHovered && hoverable ? 'translateY(-2px)' : 'none',
        ...style,
      }}
    >
      {children}
    </div>
  );
};

export const CardHeader: React.FC<{
  children: React.ReactNode;
  action?: React.ReactNode;
  style?: React.CSSProperties;
}> = ({ children, action, style }) => (
  <div
    style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'space-between',
      marginBottom: spacing[3],
      ...style,
    }}
  >
    {children}
    {action}
  </div>
);

export const CardContent: React.FC<{
  children: React.ReactNode;
  style?: React.CSSProperties;
}> = ({ children, style }) => (
  <div style={style}>{children}</div>
);

export const CardFooter: React.FC<{
  children: React.ReactNode;
  style?: React.CSSProperties;
}> = ({ children, style }) => (
  <div
    style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'flex-end',
      gap: spacing[2],
      marginTop: spacing[4],
      paddingTop: spacing[3],
      borderTop: `1px solid ${colors.slate[100]}`,
      ...style,
    }}
  >
    {children}
  </div>
);

export default Card;
