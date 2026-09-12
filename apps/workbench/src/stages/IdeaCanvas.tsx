import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import {
  AlertCircle,
  ArrowRight,
  FileText,
  MessageCircleQuestion,
  RefreshCw,
  ThumbsUp,
  ThumbsDown,
} from 'lucide-react';
import { Button, Textarea, Card, Skeleton, Tooltip } from '../ui';
import { getApiBase, post } from '../../api';
import { qk, useProjectFlow, useAdvanceStage } from '../lib/queries';
import { useDraftsStore } from '../stores/drafts';
import { staggerContainer, staggerItem } from '../lib/motion';
import type { Artifact, Flow } from '../services-bridge/flows';

const IDEA_ARTIFACT_TYPE = 'idea.md';
const MAX_IDEA_LENGTH = 20_000;

const AGENT_QUESTIONS = [
  '目标用户群体是谁？有无规模约束？',
  '技术栈有特定要求吗（语言/框架/部署）？',
  '是否需要与现有系统集成？哪些端点？',
];

function ideaContentRef(content: string): string {
  return `data:text/markdown;charset=utf-8,${encodeURIComponent(`${content.trim()}\n`)}`;
}

async function saveIdeaArtifact(
  flow: Flow,
  stageId: string,
  contentRef: string
): Promise<Artifact> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const flows = store.MOCK_FLOWS as Flow[];
      const index = flows.findIndex((item) => item.id === flow.id);
      if (index < 0) throw new Error('当前工作流不存在，请刷新后重试。');

      const next = structuredClone(flows[index]);
      const existing = next.artifacts?.find(
        (artifact) =>
          artifact.stage_id === stageId &&
          artifact.type === IDEA_ARTIFACT_TYPE &&
          artifact.content_ref === contentRef &&
          artifact.status !== 'stale'
      );
      if (existing) return existing;

      const version =
        (next.artifacts ?? []).filter(
          (artifact) => artifact.stage_id === stageId && artifact.type === IDEA_ARTIFACT_TYPE
        ).length + 1;
      const createdAt = new Date().toISOString();
      const artifact: Artifact = {
        id: `artifact-mock-${Date.now().toString(36)}`,
        stage_id: stageId,
        type: IDEA_ARTIFACT_TYPE,
        version,
        status: 'draft',
        content_ref: contentRef,
        created_at: createdAt,
      };
      next.artifacts = [...(next.artifacts ?? []), artifact];
      next.events = [
        ...(next.events ?? []),
        {
          id: `event-mock-${Date.now().toString(36)}`,
          type: 'artifact.created',
          stage_id: stageId,
          message: `artifact type=${IDEA_ARTIFACT_TYPE} v=${version}`,
          timestamp: createdAt,
        },
      ];
      next.updated_at = createdAt;
      flows[index] = next;
      return artifact;
    }
  }

  return post<Artifact>(`${getApiBase()}/api/v1/flows/${flow.id}/stages/${stageId}/artifacts`, {
    type: IDEA_ARTIFACT_TYPE,
    content_ref: contentRef,
  });
}

export default function IdeaCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const draft = useDraftsStore((s) => s.byProject[projectId]) ?? '';
  const setDraft = useDraftsStore((s) => s.setDraft);
  const [dismissed, setDismissed] = useState<Set<number>>(() => new Set());
  const [isSaving, setIsSaving] = useState(false);
  const [savedContentRef, setSavedContentRef] = useState('');
  const [saveError, setSaveError] = useState('');

  const flowQuery = useProjectFlow(projectId);
  const { flow } = flowQuery;
  const ideaStage = flow?.stages.find((s) => s.type === 'idea');
  const canAdvance = flow?.status === 'active' && ideaStage?.status === 'active';
  const advanceMut = useAdvanceStage(projectId);

  const finish = async () => {
    if (!flow || !ideaStage || !canAdvance || isSaving || advanceMut.isPending) return;

    const normalizedDraft = draft.trim();
    if (!normalizedDraft) return;
    const contentRef = ideaContentRef(normalizedDraft);
    let artifactSaved =
      savedContentRef === contentRef ||
      !!flow.artifacts?.some(
        (artifact) =>
          artifact.stage_id === ideaStage.id &&
          artifact.type === IDEA_ARTIFACT_TYPE &&
          artifact.content_ref === contentRef &&
          artifact.status !== 'stale'
      );

    setIsSaving(true);
    setSaveError('');
    try {
      if (!artifactSaved) {
        await saveIdeaArtifact(flow, ideaStage.id, contentRef);
        artifactSaved = true;
        setSavedContentRef(contentRef);
        await queryClient.invalidateQueries({ queryKey: qk.flows(projectId) });
      }

      const updated = await advanceMut.mutateAsync({ flowId: flow.id, stageId: ideaStage.id });
      const next = updated.stages.find(
        (stage) => stage.status === 'active' || stage.status === 'waiting_gate'
      );
      setDraft(projectId, '');
      toast.success(`idea.md 已保存，意图澄清完成${next ? `，进入「${next.name}」` : ''}`);
      if (next) navigate(`/workbench/${projectId}/${next.type}`);
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      const message = artifactSaved
        ? `idea.md 已保存，但阶段未能推进：${detail} 草稿仍在本机，恢复连接后再次点击即可继续。`
        : `idea.md 保存失败：${detail} 草稿仍在本机，恢复连接后再次点击即可重试。`;
      setSaveError(message);
      toast.error(message);
    } finally {
      setIsSaving(false);
    }
  };

  const adopt = (i: number) => {
    if (isSaving || advanceMut.isPending) return;
    const q = AGENT_QUESTIONS[i];
    const nextDraft = draft ? `${draft}\n\n> ${q}\n` : `> ${q}\n`;
    if (nextDraft.length > MAX_IDEA_LENGTH) {
      setSaveError(
        `Idea 草稿最多 ${MAX_IDEA_LENGTH.toLocaleString('zh-CN')} 字，请精简后再采纳追问。`
      );
      return;
    }
    setDraft(projectId, nextDraft);
    setSavedContentRef('');
    setSaveError('');
    setDismissed((prev) => new Set(prev).add(i));
  };

  const dismiss = (i: number) => {
    if (isSaving || advanceMut.isPending) return;
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

  if (flowQuery.isLoading) {
    return (
      <div className="flex h-full flex-col gap-4 p-5" aria-label="正在加载 Idea 阶段" aria-busy>
        <Skeleton className="h-5 w-28" />
        <Skeleton className="min-h-48 flex-1" />
        <div className="flex justify-end">
          <Skeleton className="h-9 w-40" />
        </div>
      </div>
    );
  }

  if (flowQuery.isError) {
    return (
      <div className="grid h-full place-items-center p-6">
        <div role="alert" className="max-w-md text-center">
          <AlertCircle size={24} className="mx-auto text-danger" />
          <h2 className="mt-3 text-[15px] font-semibold text-ink">无法加载当前工作流</h2>
          <p className="mt-1.5 text-[13px] leading-relaxed text-ink-dim">
            本地 Idea 草稿不受影响。恢复后端连接后重试，即可继续编辑并保存 idea.md。
          </p>
          <Button
            className="mt-4"
            variant="secondary"
            onClick={() => flowQuery.refetch()}
            loading={flowQuery.isFetching}
          >
            <RefreshCw size={14} /> 重试加载
          </Button>
        </div>
      </div>
    );
  }

  if (!flow || !ideaStage) {
    return (
      <div className="grid h-full place-items-center p-6">
        <div role="status" className="max-w-md text-center">
          <FileText size={24} className="mx-auto text-ink-mute" />
          <h2 className="mt-3 text-[15px] font-semibold text-ink">Idea 阶段尚未就绪</h2>
          <p className="mt-1.5 text-[13px] leading-relaxed text-ink-dim">
            当前项目没有可用的 Idea
            工作流。本地草稿已经保留，请先重试加载；若仍无结果，返回项目列表重新进入。
          </p>
          <div className="mt-4 flex flex-wrap justify-center gap-2">
            <Button
              variant="secondary"
              onClick={() => flowQuery.refetch()}
              loading={flowQuery.isFetching}
            >
              <RefreshCw size={14} /> 重试加载
            </Button>
            <Button variant="ghost" onClick={() => navigate('/projects')}>
              返回项目列表
            </Button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <motion.div
      variants={staggerContainer}
      initial="initial"
      animate="animate"
      className="flex h-full flex-col gap-5 overflow-auto p-5 xl:flex-row xl:overflow-hidden"
    >
      <motion.div variants={staggerItem} className="flex flex-[3] flex-col gap-4">
        <h2 className="font-display-13 text-ink">意图澄清</h2>
        <Textarea
          rows={8}
          value={draft}
          maxLength={MAX_IDEA_LENGTH}
          disabled={isSaving || advanceMut.isPending || ideaStage.status !== 'active'}
          onChange={(e) => {
            setDraft(projectId, e.target.value);
            setSavedContentRef('');
            if (saveError) setSaveError('');
          }}
          placeholder="描述你的想法、目标与约束…（草稿会自动保存在本地）"
          className="min-h-48 flex-1 text-[15px] leading-relaxed"
        />
        {saveError && (
          <div
            id="idea-save-error"
            role="alert"
            className="flex items-start gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2.5 text-[13px] leading-relaxed text-danger"
          >
            <AlertCircle size={15} className="mt-0.5 shrink-0" />
            <span className="min-w-0 break-words">{saveError}</span>
          </div>
        )}
        <div className="flex items-center justify-end gap-3">
          {draft && (
            <span className="text-xs text-ink-mute" aria-live="polite">
              {isSaving ? '正在写入 idea.md…' : '草稿已本地保存'}
            </span>
          )}
          <Tooltip content={advanceHint ?? '先保存 idea.md，再推进到下一阶段'} side="top">
            <span>
              <Button
                variant="primary"
                disabled={!canAdvance || !draft.trim() || isSaving || advanceMut.isPending}
                loading={isSaving || advanceMut.isPending}
                onClick={finish}
                aria-describedby={saveError ? 'idea-save-error' : undefined}
              >
                保存并进入设计 <ArrowRight size={15} />
              </Button>
            </span>
          </Tooltip>
        </div>
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-full shrink-0 flex-col gap-3 xl:w-80">
        <h3 className="text-[13px] font-semibold text-ink-dim">Agent 追问</h3>
        {AGENT_QUESTIONS.map((q, i) =>
          dismissed.has(i) ? null : (
            <Card
              key={i}
              className="animate-rise p-3.5"
              style={{ animationDelay: `${120 + i * 80}ms` }}
            >
              <div className="flex items-start gap-2.5">
                <MessageCircleQuestion size={16} className="mt-0.5 shrink-0 text-ink-mute" />
                <p className="text-[13px] leading-relaxed text-ink-dim">{q}</p>
              </div>
              <div className="mt-2.5 flex gap-1.5">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={isSaving || advanceMut.isPending}
                  onClick={() => adopt(i)}
                >
                  <ThumbsUp size={13} /> 采纳
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={isSaving || advanceMut.isPending}
                  onClick={() => dismiss(i)}
                >
                  <ThumbsDown size={13} /> 忽略
                </Button>
              </div>
            </Card>
          )
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
