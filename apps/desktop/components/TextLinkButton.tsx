import React from 'react';

export interface TextLinkButtonProps {
  children: React.ReactNode;
  onClick?: () => void;
  className?: string;
}

export const TextLinkButton: React.FC<TextLinkButtonProps> = ({ children, onClick, className }) => (
  <button
    type="button"
    onClick={onClick}
    className={`hover:text-blue-600 transition-colors ${className ?? ''}`.trim()}
  >
    {children}
  </button>
);
