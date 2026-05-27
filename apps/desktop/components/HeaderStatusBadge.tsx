import React from 'react';

export interface HeaderStatusBadgeProps {
  label: React.ReactNode;
  className?: string;
  size?: 'sm' | 'xs';
}

const sizeClassMap: Record<NonNullable<HeaderStatusBadgeProps['size']>, string> = {
  sm: 'text-[10px] px-2 py-0.5 rounded-full',
  xs: 'text-[9px] px-1.5 py-0.5 rounded',
};

export const HeaderStatusBadge: React.FC<HeaderStatusBadgeProps> = ({ label, className, size = 'sm' }) => (
  <span className={`font-bold border ${sizeClassMap[size]} ${className ?? ''}`.trim()}>
    {label}
  </span>
);
