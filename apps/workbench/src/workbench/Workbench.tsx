import { useEffect } from 'react';
import { useParams, Navigate } from 'react-router-dom';
import { TopBar } from './TopBar';
import { FlowRail } from './FlowRail';
import { StageCanvasHost } from './StageCanvasHost';
import { AgentCompanion } from './AgentCompanion';
import { BottomPanel } from './BottomPanel';
import { StatusBar } from './StatusBar';
import { ResizeHandle } from './ResizeHandle';
import { useLayoutStore } from '../stores/layout';
import { useFlows } from '../lib/queries';
import { isStageSlug, STAGE_BY_TYPE, DEFAULT_STAGE } from '../stages/stageMeta';
import type { StageType } from '../services-bridge/flows';

export default function Workbench() {
  const { projectId, stage } = useParams<{ projectId: string; stage: string }>();
  const flowsQ = useFlows(projectId, !!projectId);
  // The mock fixture flow for proj-nebula-001 has id flow-nebula-main; prefer
  // that one when present so the workbench shows the mid-coding flow rather
  // than whichever the API happened to return first.
  const flows = flowsQ.data?.items ?? [];
  const flow = flows.find((f) => f.status === 'active') ?? flows[0];
  const setLoc = useLayoutStore((s) => s.setWorkbenchLocation);

  const companionCollapsed = useLayoutStore((s) => s.companionCollapsed);
  const bottomCollapsed = useLayoutStore((s) => s.bottomCollapsed);
  const rightWidth = useLayoutStore((s) => s.rightWidth);
  const bottomHeight = useLayoutStore((s) => s.bottomHeight);
  const setRightWidth = useLayoutStore((s) => s.setRightWidth);
  const setBottomHeight = useLayoutStore((s) => s.setBottomHeight);
  const toggleCompanion = useLayoutStore((s) => s.toggleCompanion);
  const toggleBottom = useLayoutStore((s) => s.toggleBottom);

  useEffect(() => {
    if (projectId && stage && isStageSlug(stage)) {
      setLoc(projectId, stage);
    }
  }, [projectId, stage, setLoc]);

  if (!projectId) return <Navigate to="/projects" replace />;
  if (!stage || !isStageSlug(stage)) {
    return <Navigate to={`/workbench/${projectId}/${DEFAULT_STAGE}`} replace />;
  }

  const validStage = stage as StageType;
  const stageMeta = STAGE_BY_TYPE[validStage];

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <TopBar
        projectId={projectId}
        stages={flow?.stages}
        connected={false}
      />

      <div className="flex min-h-0 flex-1 overflow-hidden">
        <FlowRail projectId={projectId} stages={flow?.stages} />

        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <div className="flex min-h-0 flex-1 overflow-hidden">
            <div className="min-w-0 flex-1 overflow-hidden">
              <StageCanvasHost stage={validStage} />
            </div>

            {!companionCollapsed && (
              <>
                <ResizeHandle
                  direction="horizontal"
                  onResize={(d) => setRightWidth(rightWidth - d)}
                  onDoubleClick={toggleCompanion}
                />
                <div style={{ width: rightWidth }} className="shrink-0 overflow-hidden">
                  <AgentCompanion stageName={stageMeta?.label} />
                </div>
              </>
            )}
          </div>

          {!bottomCollapsed && (
            <>
              <ResizeHandle
                direction="vertical"
                onResize={(d) => setBottomHeight(bottomHeight - d)}
                onDoubleClick={toggleBottom}
              />
              <div style={{ height: bottomHeight }} className="shrink-0 overflow-hidden">
                <BottomPanel />
              </div>
            </>
          )}
        </div>
      </div>

      <StatusBar stage={stageMeta?.label} connected={false} />
    </div>
  );
}
