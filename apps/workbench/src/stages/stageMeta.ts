import { Lightbulb, PenTool, ListChecks, Microscope, Code2, GitPullRequest, Send } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { StageType, StageStatus } from '../services-bridge/flows';

export interface StageMeta {
  /** floweng stage type + route slug */
  type: StageType;
  index: number;
  label: string;
  en: string;
  blurb: string;
  icon: LucideIcon;
}

/** The seven product stages, in flow order (workbench-and-shell §3). */
export const STAGES: StageMeta[] = [
  { type: 'idea', index: 1, label: '意图', en: 'Idea', blurb: '澄清目标与约束', icon: Lightbulb },
  { type: 'design', index: 2, label: '设计', en: 'Design', blurb: '架构与方案辩论', icon: PenTool },
  { type: 'planning', index: 3, label: '规划', en: 'Planning', blurb: '任务分解与排期', icon: ListChecks },
  { type: 'research', index: 4, label: '调研', en: 'Research', blurb: '多源检索与取证', icon: Microscope },
  { type: 'coding', index: 5, label: '编码', en: 'Coding', blurb: '实现与守卫', icon: Code2 },
  { type: 'review', index: 6, label: '评审', en: 'Review', blurb: 'Diff 与 Critic', icon: GitPullRequest },
  { type: 'submit', index: 7, label: '提交', en: 'Submit', blurb: '分组提交与归档', icon: Send },
];

export const STAGE_BY_TYPE: Record<string, StageMeta> = Object.fromEntries(
  STAGES.map((s) => [s.type, s]),
);

export const DEFAULT_STAGE = STAGES[0].type;

export function isStageSlug(slug: string | undefined): slug is StageType {
  return !!slug && slug in STAGE_BY_TYPE;
}

/** Visual tone for a stage status, used by pills + flow nodes. */
export function stageTone(status: StageStatus): 'neutral' | 'accent' | 'success' | 'warn' {
  switch (status) {
    case 'done':
      return 'success';
    case 'active':
      return 'accent';
    case 'waiting_gate':
      return 'warn';
    default:
      return 'neutral';
  }
}

export const STAGE_STATUS_LABEL: Record<StageStatus, string> = {
  pending: '未开始',
  active: '进行中',
  waiting_gate: '待审批',
  done: '已完成',
  skipped: '已跳过',
};
