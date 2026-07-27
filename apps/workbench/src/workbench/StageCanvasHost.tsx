import { Suspense, lazy } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import type { StageType } from '../services-bridge/flows';
import { Spinner } from '../ui';
import { canvasMorph } from '../lib/motion';

const canvasMap: Record<string, React.LazyExoticComponent<React.ComponentType>> = {
  idea: lazy(() => import('../stages/IdeaCanvas')),
  design: lazy(() => import('../stages/DesignCanvas')),
  planning: lazy(() => import('../stages/PlanningCanvas')),
  research: lazy(() => import('../stages/ResearchCanvas')),
  coding: lazy(() => import('../stages/CodingCanvas')),
  review: lazy(() => import('../stages/ReviewCanvas')),
  submit: lazy(() => import('../stages/SubmitCanvas')),
};

function CanvasFallback() {
  return (
    <div className="grid h-full place-items-center">
      <Spinner size={22} />
    </div>
  );
}

interface StageCanvasHostProps {
  stage: StageType;
}

export function StageCanvasHost({ stage }: StageCanvasHostProps) {
  const Canvas = canvasMap[stage];

  return (
    <AnimatePresence mode="popLayout" initial={false}>
      <motion.div
        key={stage}
        className="h-full overflow-auto"
        initial={canvasMorph.initial}
        animate={canvasMorph.animate}
        exit={canvasMorph.exit}
        transition={canvasMorph.transition}
      >
        <Suspense fallback={<CanvasFallback />}>
          {Canvas ? (
            <Canvas />
          ) : (
            <div className="grid h-full place-items-center text-[13px] text-ink-mute">
              未知阶段：{stage}
            </div>
          )}
        </Suspense>
      </motion.div>
    </AnimatePresence>
  );
}
