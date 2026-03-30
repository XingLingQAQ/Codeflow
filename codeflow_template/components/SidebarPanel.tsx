import React from 'react';

export interface SidebarPanelProps {
  children: React.ReactNode;
  className?: string;
}

export const SidebarPanel: React.FC<SidebarPanelProps> = ({ children, className }) => (
  <div className={`flex flex-col ${className ?? ''}`.trim()}>{children}</div>
);
