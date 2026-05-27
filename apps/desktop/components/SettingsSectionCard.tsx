import React from 'react';

export interface SettingsSectionCardProps {
  icon: React.ReactNode;
  iconClassName: string;
  title: string;
  description: string;
  children: React.ReactNode;
  className?: string;
  headerClassName?: string;
  contentClassName?: string;
}

export const SettingsSectionCard: React.FC<SettingsSectionCardProps> = ({
  icon,
  iconClassName,
  title,
  description,
  children,
  className,
  headerClassName,
  contentClassName,
}) => (
  <section className={`bg-white rounded-2xl p-6 md:p-8 border border-slate-200 shadow-sm ${className ?? ''}`.trim()}>
    <div className={`flex items-start gap-4 ${headerClassName ?? 'mb-6'}`.trim()}>
      <div className={`p-2 rounded-xl ${iconClassName}`.trim()}>
        {icon}
      </div>
      <div>
        <h2 className="text-lg font-bold text-slate-900">{title}</h2>
        <p className="text-sm text-slate-500">{description}</p>
      </div>
    </div>
    <div className={contentClassName ?? ''}>{children}</div>
  </section>
);
