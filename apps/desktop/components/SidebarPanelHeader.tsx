import React from 'react';

export interface SidebarPanelHeaderProps {
  icon: React.ReactNode;
  title: React.ReactNode;
  status?: React.ReactNode;
  className?: string;
  titleClassName?: string;
}

export const SidebarPanelHeader: React.FC<SidebarPanelHeaderProps> = ({
  icon,
  title,
  status,
  className,
  titleClassName,
}) => (
  <div className={`flex justify-between items-center ${className ?? ''}`.trim()}>
    <h3 className={`font-bold text-slate-700 text-sm flex items-center gap-2 ${titleClassName ?? ''}`.trim()}>
      {icon}
      {title}
    </h3>
    {status}
  </div>
);
