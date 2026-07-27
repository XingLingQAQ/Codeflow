import { useCallback, useRef } from 'react';
import { cn } from '../lib/cn';

interface ResizeHandleProps {
  direction: 'horizontal' | 'vertical';
  onResize: (delta: number) => void;
  onDoubleClick?: () => void;
  className?: string;
}

export function ResizeHandle({ direction, onResize, onDoubleClick, className }: ResizeHandleProps) {
  const dragging = useRef(false);
  const lastPos = useRef(0);

  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      e.currentTarget.setPointerCapture(e.pointerId);
      dragging.current = true;
      lastPos.current = direction === 'horizontal' ? e.clientX : e.clientY;
    },
    [direction],
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!dragging.current) return;
      const pos = direction === 'horizontal' ? e.clientX : e.clientY;
      const delta = pos - lastPos.current;
      lastPos.current = pos;
      if (delta !== 0) onResize(delta);
    },
    [direction, onResize],
  );

  const onPointerUp = useCallback(() => {
    dragging.current = false;
  }, []);

  const isH = direction === 'horizontal';

  return (
    <div
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onDoubleClick={onDoubleClick}
      className={cn(
        'group relative z-10 flex-shrink-0 select-none',
        isH ? 'w-1.5 cursor-col-resize' : 'h-1.5 cursor-row-resize',
        className,
      )}
    >
      <div
        className={cn(
          'absolute bg-line transition-colors group-hover:bg-ink/40 group-active:bg-ink/60',
          isH ? 'left-[3px] top-0 h-full w-px' : 'left-0 top-[3px] h-px w-full',
        )}
      />
    </div>
  );
}
