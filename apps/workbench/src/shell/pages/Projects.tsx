import { type FormEvent, type KeyboardEvent, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import { AlertCircle, Plus, FolderKanban, GitBranch, RefreshCw } from 'lucide-react';
import { PageShell } from './PageShell';
import {
  Card,
  Button,
  StatusPill,
  EmptyState,
  Skeleton,
  Badge,
  Input,
  Textarea,
  Label,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '../../ui';
import { useProjects, useCreateProject } from '../../lib/queries';
import { useLayoutStore } from '../../stores/layout';
import { relTime } from '../../lib/format';
import { staggerItem } from '../../lib/motion';
import type { Project } from '../../../types';

const projectTone: Record<string, 'success' | 'info' | 'warn' | 'accent' | 'neutral'> = {
  active: 'success',
  planning: 'info',
  paused: 'warn',
  completed: 'accent',
  archived: 'neutral',
};

const projectStatusLabel: Record<string, string> = {
  active: '进行中',
  planning: '规划中',
  paused: '已暂停',
  completed: '已完成',
  archived: '已归档',
};

export default function Projects() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const projectsQ = useProjects();
  const createM = useCreateProject();
  const setLoc = useLayoutStore((s) => s.setWorkbenchLocation);

  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [desc, setDesc] = useState('');
  const [tags, setTags] = useState('');
  const [createError, setCreateError] = useState('');
  const submittingRef = useRef(false);

  useEffect(() => {
    if (params.get('new') === '1') setOpen(true);
  }, [params]);

  const closeDialog = (next: boolean) => {
    setOpen(next);
    if (next) setCreateError('');
    if (!next && params.get('new')) {
      const nextParams = new URLSearchParams(params);
      nextParams.delete('new');
      setParams(nextParams, { replace: true });
    }
  };

  const openProject = (p: Project) => {
    setLoc(p.id, 'idea');
    navigate(`/workbench/${p.id}/idea`);
  };

  const submit = async (event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    if (submittingRef.current || createM.isPending) return;

    const normalizedTitle = title.trim();
    const normalizedTags = tags
      .split(',')
      .map((tag) => tag.trim())
      .filter(Boolean);
    if (!normalizedTitle) {
      setCreateError('请输入项目标题。');
      return;
    }
    if (normalizedTags.length > 20) {
      setCreateError('标签最多填写 20 个，请精简后重试。');
      return;
    }

    submittingRef.current = true;
    setCreateError('');
    try {
      const created = await createM.mutateAsync({
        title: normalizedTitle,
        description: desc.trim() || undefined,
        tags: normalizedTags.length > 0 ? Array.from(new Set(normalizedTags)) : undefined,
      });
      const activeStage = created.flow.stages.find(
        (stage) => stage.status === 'active' || stage.status === 'waiting_gate'
      );
      if (created.flow.status !== 'active' || !activeStage) {
        toast.error('项目已创建，但默认工作流状态异常。请在项目列表刷新确认，避免重复创建。');
        setTitle('');
        setDesc('');
        setTags('');
        closeDialog(false);
        await projectsQ.refetch();
        return;
      }

      toast.success('项目和七阶段工作流已创建');
      setTitle('');
      setDesc('');
      setTags('');
      closeDialog(false);
      setLoc(created.id, activeStage.type);
      navigate(`/workbench/${created.id}/${activeStage.type}`);
    } catch (e) {
      const detail = e instanceof Error ? e.message : String(e);
      const message = `未能创建完整项目：${detail} 表单内容已保留，可修正后重试。`;
      setCreateError(message);
      toast.error(message);
    } finally {
      submittingRef.current = false;
    }
  };

  const onProjectKeyDown = (event: KeyboardEvent<HTMLDivElement>, project: Project) => {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    openProject(project);
  };

  const projects = projectsQ.data?.projects ?? [];

  return (
    <PageShell
      title="项目"
      subtitle="管理你的活跃工作区，从想法进入完整开发流程"
      actions={
        <Button variant="primary" onClick={() => setOpen(true)}>
          <Plus size={16} /> 新建项目
        </Button>
      }
    >
      {projectsQ.isError && projects.length > 0 && (
        <div
          role="status"
          className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-warn/30 bg-warn/10 px-3.5 py-3 text-[13px] text-ink-dim"
        >
          <span className="flex min-w-0 items-center gap-2">
            <AlertCircle size={15} className="shrink-0 text-warn" />
            当前显示的是上次加载结果，最新项目状态暂时无法获取。
          </span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => projectsQ.refetch()}
            loading={projectsQ.isFetching}
          >
            <RefreshCw size={14} /> 重试
          </Button>
        </div>
      )}
      {projectsQ.isLoading ? (
        <div
          className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3"
          aria-label="正在加载项目"
          aria-busy
        >
          {Array.from({ length: 6 }).map((_, i) => (
            <Card key={i} className="p-4">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="mt-3 h-3 w-full" />
              <Skeleton className="mt-2 h-3 w-2/3" />
              <Skeleton className="mt-4 h-1.5 w-full" />
            </Card>
          ))}
        </div>
      ) : projects.length === 0 ? (
        <Card className="py-6">
          <EmptyState
            icon={<FolderKanban size={24} />}
            title={projectsQ.isError ? '暂时无法连接后端' : '还没有项目'}
            description={
              projectsQ.isError
                ? '后端未就绪。可稍后重试，或先浏览界面。'
                : '创建你的第一个项目，CodeFlow 会以流程为中心陪你从想法走到提交。'
            }
            action={
              projectsQ.isError ? (
                <Button variant="secondary" onClick={() => projectsQ.refetch()}>
                  重试连接
                </Button>
              ) : (
                <Button variant="primary" onClick={() => setOpen(true)}>
                  <Plus size={16} /> 新建项目
                </Button>
              )
            }
          />
        </Card>
      ) : (
        <motion.div
          variants={staggerItem}
          className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3"
        >
          {projects.map((p) => (
            <Card
              key={p.id}
              interactive
              role="button"
              tabIndex={0}
              aria-label={`打开项目：${p.title}`}
              className="flex min-w-0 flex-col p-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
              onClick={() => openProject(p)}
              onKeyDown={(event) => onProjectKeyDown(event, p)}
            >
              <div className="flex items-start justify-between gap-2">
                <h3 className="line-clamp-1 font-medium text-ink">{p.title}</h3>
                <StatusPill
                  tone={projectTone[p.status] ?? 'neutral'}
                  pulse={p.status === 'active'}
                  label={projectStatusLabel[p.status] ?? p.status}
                />
              </div>
              <p className="mt-1.5 line-clamp-2 min-h-8 flex-1 text-[13px] leading-relaxed text-ink-dim">
                {p.description || '暂无描述'}
              </p>
              {p.tags && p.tags.length > 0 && (
                <div className="mt-2.5 flex flex-wrap gap-1.5">
                  {p.tags.slice(0, 4).map((t) => (
                    <Badge key={t}>{t}</Badge>
                  ))}
                </div>
              )}
              <div className="mt-3">
                <div className="h-1.5 overflow-hidden rounded-full bg-tint-hover">
                  <div
                    className="h-full rounded-full bg-ink"
                    style={{
                      width: `${Math.round((p.progress ?? 0) * (p.progress <= 1 ? 100 : 1))}%`,
                    }}
                  />
                </div>
                <div className="mt-2 flex items-center justify-between text-[11px] text-ink-mute">
                  <span className="flex items-center gap-1.5">
                    {p.git_branch && (
                      <>
                        <GitBranch size={12} /> {p.git_branch}
                      </>
                    )}
                  </span>
                  <span>{relTime(p.updated_at)}</span>
                </div>
              </div>
            </Card>
          ))}
        </motion.div>
      )}

      <Dialog open={open} onOpenChange={closeDialog}>
        <DialogContent>
          <DialogTitle>新建项目</DialogTitle>
          <DialogDescription>为一段新的想法开启完整的开发流程。</DialogDescription>
          <form className="mt-4" onSubmit={submit} aria-busy={createM.isPending}>
            <div className="space-y-3.5">
              <div className="space-y-1.5">
                <Label htmlFor="p-title">项目标题 *</Label>
                <Input
                  id="p-title"
                  value={title}
                  autoFocus
                  required
                  maxLength={200}
                  onChange={(e) => {
                    setTitle(e.target.value);
                    if (createError) setCreateError('');
                  }}
                  placeholder="例如：结账流程重构"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="p-desc">描述</Label>
                <Textarea
                  id="p-desc"
                  rows={3}
                  value={desc}
                  maxLength={10_000}
                  onChange={(e) => setDesc(e.target.value)}
                  placeholder="一句话说明这个项目要解决什么问题"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="p-tags">标签（逗号分隔）</Label>
                <Input
                  id="p-tags"
                  value={tags}
                  maxLength={1_300}
                  onChange={(e) => setTags(e.target.value)}
                  placeholder="frontend, refactor"
                />
              </div>
            </div>
            {createError && (
              <div
                id="project-create-error"
                role="alert"
                className="mt-3 flex items-start gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2.5 text-[13px] leading-relaxed text-danger"
              >
                <AlertCircle size={15} className="mt-0.5 shrink-0" />
                <span className="min-w-0 break-words">{createError}</span>
              </div>
            )}
            <div className="mt-5 flex justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                onClick={() => closeDialog(false)}
                disabled={createM.isPending}
              >
                取消
              </Button>
              <Button
                type="submit"
                variant="primary"
                loading={createM.isPending}
                disabled={!title.trim()}
                aria-describedby={createError ? 'project-create-error' : undefined}
              >
                创建并进入 Idea
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}
