import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { StageType } from '../services-bridge/flows';

export type BottomTab = 'terminal' | 'logs' | 'audit' | 'problems' | 'guard';

function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n));
}

const DEFAULTS = { leftWidth: 208, rightWidth: 320, bottomHeight: 200 };

interface LayoutState {
  navRailExpanded: boolean;
  flowRailCollapsed: boolean;
  companionCollapsed: boolean;
  bottomCollapsed: boolean;
  leftWidth: number;
  rightWidth: number;
  bottomHeight: number;
  bottomTab: BottomTab;
  /** Last workbench location, so the nav rail + palette can jump back. */
  lastProjectId?: string;
  lastStage: StageType;
  setWorkbenchLocation: (projectId: string, stage: StageType) => void;
  toggleNavRail: () => void;
  toggleFlowRail: () => void;
  toggleCompanion: () => void;
  toggleBottom: () => void;
  setLeftWidth: (n: number) => void;
  setRightWidth: (n: number) => void;
  setBottomHeight: (n: number) => void;
  setBottomTab: (t: BottomTab) => void;
  resetSizes: () => void;
}

export const useLayoutStore = create<LayoutState>()(
  persist(
    (set) => ({
      navRailExpanded: false,
      flowRailCollapsed: false,
      companionCollapsed: false,
      bottomCollapsed: false,
      ...DEFAULTS,
      bottomTab: 'logs',
      lastProjectId: undefined,
      lastStage: 'idea',
      setWorkbenchLocation: (lastProjectId, lastStage) => set({ lastProjectId, lastStage }),
      toggleNavRail: () => set((s) => ({ navRailExpanded: !s.navRailExpanded })),
      toggleFlowRail: () => set((s) => ({ flowRailCollapsed: !s.flowRailCollapsed })),
      toggleCompanion: () => set((s) => ({ companionCollapsed: !s.companionCollapsed })),
      toggleBottom: () => set((s) => ({ bottomCollapsed: !s.bottomCollapsed })),
      setLeftWidth: (n) => set({ leftWidth: clamp(n, 176, 340) }),
      setRightWidth: (n) => set({ rightWidth: clamp(n, 260, 480) }),
      setBottomHeight: (n) => set({ bottomHeight: clamp(n, 120, 520) }),
      setBottomTab: (bottomTab) => set({ bottomTab }),
      resetSizes: () => set({ ...DEFAULTS }),
    }),
    { name: 'codeflow.layout', version: 1 },
  ),
);
