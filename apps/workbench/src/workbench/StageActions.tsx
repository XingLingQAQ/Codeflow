import { useState } from 'react';
import { toast } from 'sonner';
import { ArrowRight, CircleSlash, ShieldAlert, Swords, Undo2 } from 'lucide-react';
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  StatusPill,
  Textarea,
} from '../ui';
import { useAdvanceStage, useSkipStage, useLoopFlow, useDecideGate, useProjectFlow } from '../lib/queries';
import { STAGE_BY_TYPE, STAGE_STATUS_LABEL, stageTone } from '../stages/stageMeta';
import type { Flow, Gate, Stage } from '../services-bridge/flows';
import { cn } from '../lib/cn';

const GATE_KIND_LABEL: Record<string, string> = {
  human_approval: '人工审批',
  agent_check: 'Agent 检查',
  auto: '自动',
};

const GATE_PHASE_LABEL: Record<string, string> = {
  enter: '进入',
  exit: '出口',
};

/** The first unpassed non-auto gate — what a waiting_gate stage is blocked on. */
export function blockingGate(stage: Stage | undefined): Gate | undefined {
  return stage?.gates?.find((g) => !g.passed && g.kind !== 'auto');
}

function errMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

interface GateDecisionPanelProps {
  projectId: string;
  flow: Flow;
  stage: Stage;
  /** Called after a decision lands (dialogs close themselves). */
  onDecided?: () => void;
}

/**
 * Approve / reject controls for a waiting gate. Rejection requires a reason;
 * when the gate escalates on failure, a debate warning is shown first.
 */
function GateDecisionPanel({ projectId, flow, stage, onDecided }: GateDecisionPanelProps) {
  const gate = blockingGate(stage);
  const decideMut = useDecideGate(projectId);
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState('');

  if (!gate) {
    return <p className="text-[12px] leading-relaxed text-ink-mute">该阶段没有待处理的 Gate。</p>;
  }

  const escalates = gate.on_fail === 'escalate_to_debate';

  const decide = (decision: 'approve' | 'reject') => {
    decideMut.mutate(
      { flowId: flow.id, gateId: gate.id, decision, reason: reason.trim() || undefined },
      {
        onSuccess: () => {
          toast.success(decision === 'approve' ? `已批准 Gate：${stage.name} 可继续推进` : `已驳回 Gate：${stage.name}`);
          setRejecting(false);
          setReason('');
          onDecided?.();
        },
        onError: (err) => toast.error(`Gate 决策失败：${errMessage(err)}`),
      },
    );
  };

  return (
    <div className="space-y-2.5">
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge tone="warn" className="gap-1">
          <ShieldAlert size={11} /> {GATE_KIND_LABEL[gate.kind] ?? gate.kind}
        </Badge>
        <Badge tone="neutral">{GATE_PHASE_LABEL[gate.phase] ?? gate.phase} Gate</Badge>
        {gate.on_fail && (
          <Badge tone={escalates ? 'danger' : 'neutral'} className="px-1.5 py-px font-mono text-[11px]">
            on_fail: {gate.on_fail}
          </Badge>
        )}
      </div>
      <p className="text-[12px] leading-relaxed text-ink-dim">
        阶段「{stage.name}」在等待审批（Gate <span className="font-mono">{gate.id}</span>
        ）。批准后阶段恢复进行中，可继续推进。
      </p>
      {escalates && (
        <p className="flex items-start gap-1.5 rounded-lg bg-warn/10 px-2.5 py-2 text-[12px] leading-relaxed text-warn">
          <Swords size={13} className="mt-0.5 shrink-0" />
          该 Gate 配置为失败升级：驳回将升级为多方辩论（escalate_to_debate）。
        </p>
      )}
      {rejecting ? (
        <div className="space-y-2">
          <Textarea
            rows={2}
            autoFocus
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="必填：说明驳回原因…"
            className="text-[12px]"
          />
          <div className="flex justify-end gap-1.5">
            <Button variant="ghost" size="sm" onClick={() => setRejecting(false)}>
              取消
            </Button>
            <Button
              variant="danger"
              size="sm"
              disabled={!reason.trim()}
              loading={decideMut.isPending}
              onClick={() => decide('reject')}
            >
              确认驳回
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex gap-1.5">
          <Button
            variant="primary"
            size="sm"
            className="flex-1"
            loading={decideMut.isPending}
            onClick={() => decide('approve')}
          >
            批准
          </Button>
          <Button variant="danger" size="sm" className="flex-1" onClick={() => setRejecting(true)}>
            驳回
          </Button>
        </div>
      )}
    </div>
  );
}

export interface GateApprovalDialogProps {
  projectId: string;
  flow?: Flow;
  stage?: Stage;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Gate approval dialog — opened from FlowProgress waiting_gate nodes. */
export function GateApprovalDialog({ projectId, flow, stage, open, onOpenChange }: GateApprovalDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(92vw,440px)]">
        <DialogTitle>Gate 审批</DialogTitle>
        <DialogDescription>
          {stage ? `阶段「${stage.name}」正在等待 Gate 决策。` : '没有待审批的阶段。'}
        </DialogDescription>
        {flow && stage && (
          <div className="mt-4">
            <GateDecisionPanel
              projectId={projectId}
              flow={flow}
              stage={stage}
              onDecided={() => onOpenChange(false)}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * Current-stage lifecycle card at the bottom of the flow rail: advance / skip
 * (optional stages) / loop back from review, flipping into a gate approval
 * panel while the stage is waiting_gate.
 */
export function StageActions({ projectId }: { projectId: string }) {
  const { flow } = useProjectFlow(projectId);
  const advanceMut = useAdvanceStage(projectId);
  const skipMut = useSkipStage(projectId);
  const loopMut = useLoopFlow(projectId);
  const [loopOpen, setLoopOpen] = useState(false);
  const [loopReason, setLoopReason] = useState('');

  const current = flow?.stages.find((s) => s.status === 'active' || s.status === 'waiting_gate');

  if (!flow || flow.status !== 'active' || !current) {
    return (
      <div className="border-t border-line px-3.5 py-3">
        <p className="text-[11px] leading-relaxed text-ink-mute">
          {flow?.status === 'completed' ? '工作流已完成 🎉' : '当前没有进行中的工作流。'}
        </p>
      </div>
    );
  }

  const meta = STAGE_BY_TYPE[current.type];
  const loopTarget = current.type === 'review' ? flow.stages.find((s) => s.type === 'coding') : undefined;
  const loopAllowed =
    !!loopTarget && (flow.loops ?? []).some((l) => l.from === 'review' && l.to === 'coding');

  const advance = () =>
    advanceMut.mutate(
      { flowId: flow.id, stageId: current.id },
      {
        onSuccess: (updated) => {
          const next = updated.stages.find((s) => s.status === 'active' || s.status === 'waiting_gate');
          toast.success(
            updated.status === 'completed'
              ? '工作流已全部完成'
              : `已完成「${current.name}」${next ? `，进入「${next.name}」` : ''}`,
          );
        },
        onError: (err) => toast.error(errMessage(err)),
      },
    );

  const skip = () =>
    skipMut.mutate(
      { flowId: flow.id, stageId: current.id },
      {
        onSuccess: () => toast.success(`已跳过可选阶段「${current.name}」`),
        onError: (err) => toast.error(errMessage(err)),
      },
    );

  const submitLoop = () => {
    if (!loopTarget || !loopReason.trim()) return;
    loopMut.mutate(
      { flowId: flow.id, fromStageId: current.id, toStageId: loopTarget.id, reason: loopReason.trim() },
      {
        onSuccess: () => {
          toast.success('已回环到编码阶段，后续阶段已重置');
          setLoopOpen(false);
          setLoopReason('');
        },
        onError: (err) => toast.error(`回环失败：${errMessage(err)}`),
      },
    );
  };

  const waiting = current.status === 'waiting_gate';

  return (
    <div className="border-t border-line px-3 py-3" data-testid="stage-actions">
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1.5 text-[12px] font-medium text-ink">
          <span
            className={cn(
              'grid size-5 shrink-0 place-items-center rounded-md text-[10px] font-bold',
              '[background:var(--btn-primary-bg)] [color:var(--btn-primary-fg)]',
            )}
          >
            {meta?.index ?? '·'}
          </span>
          <span className="truncate">{current.name}</span>
        </span>
        <StatusPill
          tone={stageTone(current.status)}
          pulse={current.status === 'active'}
          label={STAGE_STATUS_LABEL[current.status]}
        />
      </div>

      {waiting ? (
        <GateDecisionPanel projectId={projectId} flow={flow} stage={current} />
      ) : (
        <div className="space-y-1.5">
          <Button
            variant="primary"
            size="sm"
            className="w-full"
            loading={advanceMut.isPending}
            onClick={advance}
            data-testid="stage-advance"
          >
            完成并推进 <ArrowRight size={13} />
          </Button>
          {current.optional && (
            <Button
              variant="ghost"
              size="sm"
              className="w-full"
              loading={skipMut.isPending}
              onClick={skip}
            >
              <CircleSlash size={13} /> 跳过该阶段
            </Button>
          )}
          {loopTarget && (
            <Button
              variant="ghost"
              size="sm"
              className="w-full"
              disabled={!loopAllowed}
              title={loopAllowed ? undefined : '该工作流未声明 review → coding 回环'}
              onClick={() => setLoopOpen(true)}
            >
              <Undo2 size={13} /> 回到编码返工
            </Button>
          )}
        </div>
      )}

      {/* Loop-back reason dialog (review → coding). */}
      <Dialog open={loopOpen} onOpenChange={setLoopOpen}>
        <DialogContent className="w-[min(92vw,420px)]">
          <DialogTitle>回环到编码阶段</DialogTitle>
          <DialogDescription>
            评审未通过时可回到编码返工：之后的阶段将重置为未开始，相关产物标记为过期。请填写返工原因。
          </DialogDescription>
          <Textarea
            rows={3}
            autoFocus
            value={loopReason}
            onChange={(e) => setLoopReason(e.target.value)}
            placeholder="必填：说明需要返工的问题…"
            className="mt-4 text-[13px]"
          />
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setLoopOpen(false)}>
              取消
            </Button>
            <Button
              variant="primary"
              size="sm"
              disabled={!loopReason.trim()}
              loading={loopMut.isPending}
              onClick={submitLoop}
            >
              <Undo2 size={13} /> 确认回环
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
