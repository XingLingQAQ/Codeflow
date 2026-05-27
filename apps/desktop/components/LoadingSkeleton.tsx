import React from 'react';

export type SkeletonVariant = 'card' | 'list' | 'text';

export interface LoadingSkeletonProps {
  variant?: SkeletonVariant;
  count?: number;
}

const Pulse: React.FC<{ className?: string }> = ({ className = '' }) => (
  <div className={`animate-pulse bg-slate-200 rounded ${className}`} />
);

const CardSkeleton = () => (
  <div className="bg-white rounded-xl border border-slate-200/60 p-5 space-y-3">
    <div className="flex items-center gap-3">
      <Pulse className="size-10 rounded-full" />
      <div className="flex-1 space-y-2">
        <Pulse className="h-4 w-3/4" />
        <Pulse className="h-3 w-1/2" />
      </div>
    </div>
    <Pulse className="h-3 w-full" />
    <Pulse className="h-3 w-5/6" />
    <div className="flex gap-2 pt-2">
      <Pulse className="h-6 w-16 rounded-full" />
      <Pulse className="h-6 w-16 rounded-full" />
    </div>
  </div>
);

const ListSkeleton = () => (
  <div className="flex items-center gap-3 py-3 px-4">
    <Pulse className="size-8 rounded-lg" />
    <div className="flex-1 space-y-2">
      <Pulse className="h-4 w-2/3" />
      <Pulse className="h-3 w-1/3" />
    </div>
    <Pulse className="h-4 w-12" />
  </div>
);

const TextSkeleton = () => (
  <div className="space-y-2 py-2">
    <Pulse className="h-4 w-full" />
    <Pulse className="h-4 w-5/6" />
    <Pulse className="h-4 w-4/6" />
  </div>
);

export const LoadingSkeleton: React.FC<LoadingSkeletonProps> = ({ variant = 'card', count = 3 }) => {
  const Component = variant === 'card' ? CardSkeleton : variant === 'list' ? ListSkeleton : TextSkeleton;
  const containerClass = variant === 'card'
    ? 'grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 p-4'
    : 'divide-y divide-slate-100';

  return (
    <div className={containerClass}>
      {Array.from({ length: count }, (_, i) => (
        <Component key={i} />
      ))}
    </div>
  );
};
