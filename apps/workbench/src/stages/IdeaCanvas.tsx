import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import { ArrowRight, MessageCircleQuestion, ThumbsUp, ThumbsDown } from 'lucide-react';
import { Button, Textarea, Card, Tooltip } from '../ui';
import { useProjectFlow, useAdvanceStage } from '../lib/queries';
import { useDraftsStore } from '../stores/drafts';
import { staggerContainer, staggerItem } from '../lib/motion';

const AGENT_QUESTIONS = [
  '目标用户群体是谁？有无规模约束？',
  '技术栈有特定要求吗（语言/框架/部署）？',
  '是否需要与现有系统集成？哪些端点？',
];

export default function IdeaCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const draft = useDraftsStore((s) => s.byProject[projectId]) ?? '';
  const setDraft = useDraftsStore((s) => s.setDraft);
  const [dismissed, setDismissed] = useState<Set<number>>(() => new Set());

  const { flow } = useProjectFlow(projectId);
  const ideaStage = flow?.stages.find((s) => s.type === 'idea');
  const canAdvance = flow?.status === 'active' && ideaStage?.status === 'active';
  const advanceMut = useAdvanceStage(projectId);

  const finish = () => {
    if (!flow || !ideaStage || !canAdvance) return;
    advanceMut.mutate(
      { flowId: flow.id, stageId: ideaStage.id },
      {
        onSuccess: (updated) => {
          const next = updated.stages.find((s) => s.status === 'active' || s.status === 'waiting_gate');
          toast.success(`意图澄清完成${next ? `，进入「${next.name}」` : ''}`);
          if (next) navigate(`/workbench/${projectId}/${next.type}`);
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
      },
    );
  };

  const adopt = (i: number) => {
    const q = AGENT_QUESTIONS[i];
    setDraft(projectId, draft ? `${draft}\n\n> ${q}\n` : `> ${q}\n`);
    setDismissed((prev) => new Set(prev).add(i));
  };

  const dismiss = (i: number) => {
    setDismissed((prev) => new Set(prev).add(i));
  };

  const advanceHint = !flow
    ? '当前项目还没有工作流'
    : ideaStage == null
      ? '当前工作流不包含意图阶段'
      : ideaStage.status === 'done'
        ? '意图阶段已完成'
        : canAdvance
          ? undefined
          : `意图阶段当前状态：${ideaStage.status}`;

  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-5 p-5">
      <motion.div variants={staggerItem} className="flex flex-[3] flex-col gap-4">
        <h2 className="font-display-13 text-ink">意图澄清</h2>
        <Textarea
          rows={8}
          value={draft}
          onChange={(e) => setDraft(projectId, e.target.value)}
          placeholder="描述你的想法、目标与约束…（草稿会自动保存在本地）"
          className="min-h-48 flex-1 text-[15px] leading-relaxed"
        />
        <div className="flex items-center justify-end gap-3">
          {draft && <span className="text-xs text-ink-mute">草稿已本地保存</span>}
          <Tooltip content={advanceHint ?? '完成意图澄清并推进到下一阶段'} side="top">
            <span>
              <Button
                variant="primary"
                disabled={!canAdvance || !draft.trim()}
                loading={advanceMut.isPending}
                onClick={finish}
              >
                完成意图澄清 <ArrowRight size={15} />
              </Button>
            </span>
          </Tooltip>
        </div>
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-80 flex-col gap-3">
        <h3 className="text-[13px] font-semibold text-ink-dim">Agent 追问</h3>
        {AGENT_QUESTIONS.map((q, i) =>
          dismissed.has(i) ? null : (
            <Card key={i} className="animate-rise p-3.5" style={{ animationDelay: `${120 + i * 80}ms` }}>
              <div className="flex items-start gap-2.5">
                <MessageCircleQuestion size={16} className="mt-0.5 shrink-0 text-ink-mute" />
                <p className="text-[13px] leading-relaxed text-ink-dim">{q}</p>
              </div>
              <div className="mt-2.5 flex gap-1.5">
                <Button variant="ghost" size="sm" onClick={() => adopt(i)}>
                  <ThumbsUp size={13} /> 采纳
                </Button>
                <Button variant="ghost" size="sm" onClick={() => dismiss(i)}>
                  <ThumbsDown size={13} /> 忽略
                </Button>
              </div>
            </Card>
          ),
        )}
        {dismissed.size === AGENT_QUESTIONS.length && (
          <p className="text-[12px] leading-relaxed text-ink-mute">
            追问已处理完毕。采纳的问题已以引用形式插入草稿，可逐条作答后完成澄清。
          </p>
        )}
      </motion.div>
    </motion.div>
  );
}
