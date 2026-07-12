import React from 'react';

export interface LabeledTextItemProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  meta?: React.ReactNode;
  className?: string;
  titleClassName?: string;
  descriptionClassName?: string;
}

export const LabeledTextItem: React.FC<LabeledTextItemProps> = ({
  title,
  description,
  meta,
  className,
  titleClassName,
  descriptionClassName,
}) => (
  <div className={className}>
    <p className={titleClassName ?? 'font-semibold text-slate-700'}>{title}</p>
    {description ? <p className={descriptionClassName ?? 'text-xs text-slate-400 mt-1'}>{description}</p> : null}
    {meta ? <div className="mt-1">{meta}</div> : null}
  </div>
);
