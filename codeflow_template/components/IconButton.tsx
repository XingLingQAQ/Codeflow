import React from 'react';

export interface IconButtonProps {
  icon: React.ReactNode;
  onClick?: () => void;
  tone?: 'subtle' | 'toolbar' | 'primary';
  size?: 'sm' | 'md';
  className?: string;
}

const toneClassMap: Record<NonNullable<IconButtonProps['tone']>, string> = {
  subtle: 'hover:bg-slate-100 text-slate-500',
  toolbar: 'hover:bg-white text-slate-400 hover:text-slate-700 transition-colors',
  primary: 'bg-blue-600 text-white hover:bg-blue-700 transition-colors',
};

const sizeClassMap: Record<NonNullable<IconButtonProps['size']>, string> = {
  sm: 'size-8 rounded',
  md: 'size-9 rounded-full',
};

export const IconButton: React.FC<IconButtonProps> = ({
  icon,
  onClick,
  tone = 'subtle',
  size = 'md',
  className,
}) => (
  <button
    type="button"
    onClick={onClick}
    className={`flex items-center justify-center ${toneClassMap[tone]} ${sizeClassMap[size]} ${className ?? ''}`.trim()}
  >
    {icon}
  </button>
);
