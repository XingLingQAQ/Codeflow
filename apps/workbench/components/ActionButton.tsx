import React from 'react';

export interface ActionButtonProps {
  children: React.ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  tone?: 'primary' | 'secondary' | 'accent';
  size?: 'sm' | 'lg' | 'inline';
  className?: string;
}

const toneClassMap: Record<NonNullable<ActionButtonProps['tone']>, string> = {
  primary: 'bg-slate-900 text-white hover:bg-slate-800',
  secondary: 'border border-slate-200 bg-white text-slate-700',
  accent: 'border border-blue-200 bg-white text-blue-600 hover:bg-blue-50',
};

const sizeClassMap: Record<NonNullable<ActionButtonProps['size']>, string> = {
  sm: 'px-3 py-1.5 rounded-lg text-xs',
  lg: 'py-3 rounded-xl text-sm',
  inline: 'px-2.5 py-1 rounded-lg text-[11px] inline-flex items-center gap-1',
};

export const ActionButton: React.FC<ActionButtonProps> = ({
  children,
  onClick,
  disabled = false,
  tone = 'primary',
  size = 'sm',
  className,
}) => (
  <button
    type="button"
    onClick={onClick}
    disabled={disabled}
    className={`font-bold transition-all disabled:opacity-50 ${toneClassMap[tone]} ${sizeClassMap[size]} ${className ?? ''}`.trim()}
  >
    {children}
  </button>
);
