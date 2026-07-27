import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import { Plus, FolderKanban, GitBranch } from 'lucide-react';
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

  useEffect(() => {
    if (params.get('new') === '1') setOpen(true);
  }, [params]);

  const closeDialog = (next: boolean) => {
    setOpen(next);
    if (!next && params.get('new')) {
      params.delete('new');
      setParams(params, { replace: true });
    }
  };

  const openProject = (p: Project) => {
    setLoc(p.id, 'idea');
    navigate(`/workbench/${p.id}/idea`);
  };

  const submit = async () => {
    if (!title.trim()) return;
    try {
      const created = await createM.mutateAsync({
        title: title.trim(),
        description: desc.trim() || undefined,
        tags: tags.trim() ? tags.split(',').map((t) => t.trim()).filter(Boolean) : undefined,
      });
      toast.success('项目已创建');
      setTitle('');
      setDesc('');
      setTags('');
      closeDialog(false);
      if (created?.id) openProject(created);
    } catch (e) {
      toast.error('创建失败：' + (e instanceof Error ? e.message : String(e)));
    }
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
      {projectsQ.isLoading ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
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
        <motion.div variants={staggerItem} className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {projects.map((p) => (
            <Card key={p.id} interactive className="flex flex-col p-4" onClick={() => openProject(p)}>
              <div className="flex items-start justify-between gap-2">
                <h3 className="line-clamp-1 font-medium text-ink">{p.title}</h3>
                <StatusPill tone={projectTone[p.status] ?? 'neutral'} pulse={p.status === 'active'} label={p.status} />
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
                    style={{ width: `${Math.round((p.progress ?? 0) * (p.progress <= 1 ? 100 : 1))}%` }}
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
          <div className="mt-4 space-y-3.5">
            <div className="space-y-1.5">
              <Label htmlFor="p-title">项目标题 *</Label>
              <Input
                id="p-title"
                value={title}
                autoFocus
                onChange={(e) => setTitle(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && submit()}
                placeholder="例如：结账流程重构"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="p-desc">描述</Label>
              <Textarea
                id="p-desc"
                rows={3}
                value={desc}
                onChange={(e) => setDesc(e.target.value)}
                placeholder="一句话说明这个项目要解决什么问题"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="p-tags">标签（逗号分隔）</Label>
              <Input id="p-tags" value={tags} onChange={(e) => setTags(e.target.value)} placeholder="frontend, refactor" />
            </div>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => closeDialog(false)}>
              取消
            </Button>
            <Button variant="primary" onClick={submit} loading={createM.isPending} disabled={!title.trim()}>
              创建项目
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}
