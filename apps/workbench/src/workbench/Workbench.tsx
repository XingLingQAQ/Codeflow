import { useEffect, useState } from 'react';
import { useParams, Navigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { AnimatePresence, motion } from 'motion/react';
import { TopBar } from './TopBar';
import { FlowRail } from './FlowRail';
import { StageCanvasHost } from './StageCanvasHost';
import { AgentCompanion } from './AgentCompanion';
import { BottomPanel, STAGE_DEFAULT_TAB } from './BottomPanel';
import { StatusBar } from './StatusBar';
import { ResizeHandle } from './ResizeHandle';
import { useLayoutStore } from '../stores/layout';
import { useShellStore } from '../stores/shell';
import { useActivityStore } from '../stores/activity';
import { useFlows, useProjects, qk } from '../lib/queries';
import { subscribe as wsSubscribe } from '../services-bridge/ws';
import { isModEvent } from '../lib/platform';
import { isStageSlug, STAGE_BY_TYPE, DEFAULT_STAGE } from '../stages/stageMeta';
import { EASE_FLOW } from '../lib/motion';
import { EmptyState } from '../ui';
import { MonitorSmartphone } from 'lucide-react';
import type { StageType } from '../services-bridge/flows';

export default function Workbench() {
  const { projectId, stage } = useParams<{ projectId: string; stage: string }>();
  const qc = useQueryClient();
  const flowsQ = useFlows(projectId, !!projectId);
  // The mock fixture flow for proj-nebula-001 has id flow-nebula-main; prefer
  // that one when present so the workbench shows the mid-coding flow rather
  // than whichever the API happened to return first.
  const flows = flowsQ.data?.items ?? [];
  const flow = flows.find((f) => f.status === 'active') ?? flows[0];
  const projectsQ = useProjects();
  const projectTitle = projectsQ.data?.projects.find((p) => p.id === projectId)?.title;
  const wsConnected = useShellStore((s) => s.wsConnected);
  const setLoc = useLayoutStore((s) => s.setWorkbenchLocation);

  const companionCollapsed = useLayoutStore((s) => s.companionCollapsed);
  const bottomCollapsed = useLayoutStore((s) => s.bottomCollapsed);
  const rightWidth = useLayoutStore((s) => s.rightWidth);
  const bottomHeight = useLayoutStore((s) => s.bottomHeight);
  const setRightWidth = useLayoutStore((s) => s.setRightWidth);
  const setBottomHeight = useLayoutStore((s) => s.setBottomHeight);
  const toggleCompanion = useLayoutStore((s) => s.toggleCompanion);
  const toggleBottom = useLayoutStore((s) => s.toggleBottom);
  const setBottomTab = useLayoutStore((s) => s.setBottomTab);
  // Drag-resize writes sizes every pointermove; skip the slide transition then.
  const [resizing, setResizing] = useState(false);

  useEffect(() => {
    if (projectId && stage && isStageSlug(stage)) {
      setLoc(projectId, stage);
      // Stage-linked default bottom-panel page (workbench-and-shell §2).
      setBottomTab(STAGE_DEFAULT_TAB[stage] ?? 'logs');
    }
  }, [projectId, stage, setLoc, setBottomTab]);

  // Global panel shortcuts: mod+B flow rail, mod+J bottom panel, mod+\ companion.
  // All are mod-combos, so they stay safe while an input/textarea has focus.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!isModEvent(e) || e.altKey || e.shiftKey || e.repeat) return;
      const k = e.key.toLowerCase();
      const layout = useLayoutStore.getState();
      if (k === 'b') {
        e.preventDefault();
        layout.toggleFlowRail();
      } else if (k === 'j') {
        e.preventDefault();
        layout.toggleBottom();
      } else if (k === '\\') {
        e.preventDefault();
        layout.toggleCompanion();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Realtime: one per-project flow topic feeds both the flow queries and the
  // activity timeline (FlowProgress = structure, timeline = event stream).
  useEffect(() => {
    if (!projectId) return;
    return wsSubscribe(`flow:project:${projectId}`, (frame) => {
      qc.invalidateQueries({ queryKey: qk.flows(projectId) });
      const d = frame.data ?? {};
      useActivityStore.getState().push({
        id: String(d.event_id ?? `flow-${Date.now().toString(36)}`),
        kind: 'flow',
        title: String(d.event_type ?? frame.content ?? 'flow_event'),
        detail: typeof d.message === 'string' ? d.message : undefined,
        stageId: typeof d.stage_id === 'string' && d.stage_id ? d.stage_id : undefined,
        at: typeof d.timestamp === 'string' ? d.timestamp : new Date().toISOString(),
      });
    });
  }, [projectId, qc]);

  // Narrow screens: fold the flow rail once per session below 1280px so the
  // canvas keeps priority; below 768px the workbench is not usable — guide.
  useEffect(() => {
    if (window.innerWidth >= 1280) return;
    try {
      if (sessionStorage.getItem('codeflow.autocollapse') === '1') return;
      sessionStorage.setItem('codeflow.autocollapse', '1');
    } catch {
      /* ignore */
    }
    const layout = useLayoutStore.getState();
    if (!layout.flowRailCollapsed) layout.toggleFlowRail();
  }, []);

  const [tooNarrow, setTooNarrow] = useState(() => window.innerWidth < 768);
  useEffect(() => {
    const mql = window.matchMedia('(max-width: 767px)');
    const onChange = () => setTooNarrow(mql.matches);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);

  if (!projectId) return <Navigate to="/projects" replace />;
  if (!stage || !isStageSlug(stage)) {
    return <Navigate to={`/workbench/${projectId}/${DEFAULT_STAGE}`} replace />;
  }

  if (tooNarrow) {
    return (
      <div className="grid h-full place-items-center p-6">
        <EmptyState
          icon={<MonitorSmartphone size={22} />}
          title="屏幕宽度不足"
          description="工作台需要至少 768px 的视口宽度；建议在 1280px 及以上的桌面环境使用。移动端远程审批将由手机伴侣应用提供。"
        />
      </div>
    );
  }

  const validStage = stage as StageType;
  const stageMeta = STAGE_BY_TYPE[validStage];

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <TopBar
        projectId={projectId}
        projectTitle={projectTitle}
        flow={flow}
        connected={wsConnected}
      />

      <div className="flex min-h-0 flex-1 overflow-hidden">
        <FlowRail projectId={projectId} stages={flow?.stages} />

        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <div className="flex min-h-0 flex-1 overflow-hidden">
            <div className="min-w-0 flex-1 overflow-hidden">
              <StageCanvasHost stage={validStage} />
            </div>

            <AnimatePresence initial={false}>
              {!companionCollapsed && (
                <motion.div
                  key="companion"
                  className="flex shrink-0 overflow-hidden"
                  initial={{ width: 0 }}
                  animate={{ width: rightWidth + 6 }}
                  exit={{ width: 0 }}
                  transition={resizing ? { duration: 0 } : { duration: 0.25, ease: EASE_FLOW }}
                >
                  <ResizeHandle
                    direction="horizontal"
                    onResize={(d) => setRightWidth(rightWidth - d)}
                    onDragStateChange={setResizing}
                    onDoubleClick={toggleCompanion}
                  />
                  <div style={{ width: rightWidth }} className="shrink-0 overflow-hidden">
                    <AgentCompanion projectId={projectId} stage={validStage} stageName={stageMeta?.label} />
                  </div>
                </motion.div>
              )}
            </AnimatePresence>
          </div>

          <AnimatePresence initial={false}>
            {!bottomCollapsed && (
              <motion.div
                key="bottom-panel"
                className="shrink-0 overflow-hidden"
                initial={{ height: 0 }}
                animate={{ height: bottomHeight + 6 }}
                exit={{ height: 0 }}
                transition={resizing ? { duration: 0 } : { duration: 0.25, ease: EASE_FLOW }}
              >
                <ResizeHandle
                  direction="vertical"
                  onResize={(d) => setBottomHeight(bottomHeight - d)}
                  onDragStateChange={setResizing}
                  onDoubleClick={toggleBottom}
                />
                <div style={{ height: bottomHeight }} className="overflow-hidden">
                  <BottomPanel projectId={projectId} />
                </div>
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      </div>

      <StatusBar
        projectId={projectId}
        stage={stageMeta?.label}
        stageSlug={validStage}
        currentStage={flow?.stages.find((s) => s.type === validStage)}
        connected={wsConnected}
      />
    </div>
  );
}
