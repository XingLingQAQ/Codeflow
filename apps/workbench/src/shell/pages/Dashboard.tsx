import { useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import { FolderKanban, Workflow, ShieldAlert, Activity, Plus, ArrowRight, GitBranch } from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import { Card, Button, StatusPill, EmptyState, Skeleton, Badge } from '../../ui';
import { MiniFlow } from '../../stages/MiniFlow';
import { useProjects, useFlows } from '../../lib/queries';
import { useLayoutStore } from '../../stores/layout';
import { greeting, relTime } from '../../lib/format';
import { staggerItem } from '../../lib/motion';
import { cn } from '../../lib/cn';
import type { Project } from '../../../types';

const projectTone: Record<string, 'success' | 'info' | 'warn' | 'accent' | 'neutral'> = {
  active: 'success',
  planning: 'info',
  paused: 'warn',
  completed: 'accent',
  archived: 'neutral',
};

type StatTone = 'accent' | 'info' | 'warn' | 'neutral';
const statIconWrap: Record<StatTone, string> = {
  accent: 'bg-tint text-ink-dim',
  info: 'bg-tint text-ink-dim',
  warn: 'bg-warn/10 text-warn',
  neutral: 'bg-tint text-ink-mute',
};

export default function Dashboard() {
  const navigate = useNavigate();
  const setLoc = useLayoutStore((s) => s.setWorkbenchLocation);
  const projectsQ = useProjects();
  const flowsQ = useFlows();

  const projects = projectsQ.data?.projects ?? [];
  const flows = flowsQ.data?.items ?? [];
  const activeFlows = flows.filter((f) => f.status === 'active');
  const pendingGates = flows.reduce(
    (n, f) => n + f.stages.filter((s) => s.status === 'waiting_gate').length,
    0,
  );

  const openProject = (p: Project) => {
    setLoc(p.id, 'idea');
    navigate(`/workbench/${p.id}/idea`);
  };

  const stats: { label: string; value: number; icon: typeof FolderKanban; tone: StatTone }[] = [
    { label: '项目', value: projects.length, icon: FolderKanban, tone: 'accent' },
    { label: '进行中工作流', value: activeFlows.length, icon: Activity, tone: 'info' },
    { label: '待审批 Gate', value: pendingGates, icon: ShieldAlert, tone: pendingGates ? 'warn' : 'neutral' },
    { label: '工作流总数', value: flows.length, icon: Workflow, tone: 'neutral' },
  ];

  const loading = projectsQ.isLoading;

  return (
    <PageShell
      title={greeting()}
      subtitle={`欢迎回到 CodeFlow · ${new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })}`}
      actions={
        <Button variant="primary" onClick={() => navigate('/projects?new=1')}>
          <Plus size={16} /> 新建项目
        </Button>
      }
    >
      <motion.div variants={staggerItem} className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {stats.map((s) => {
          const Icon = s.icon;
          return (
            <Card key={s.label} className="p-4">
              <div className="flex items-center justify-between">
                <span className="text-[13px] text-ink-dim">{s.label}</span>
                <span className={cn('grid size-8 place-items-center rounded-lg', statIconWrap[s.tone])}>
                  <Icon size={16} />
                </span>
              </div>
              <div className="mt-2 font-display text-3xl font-bold text-ink">
                {loading ? <Skeleton className="h-8 w-12" /> : s.value}
              </div>
            </Card>
          );
        })}
      </motion.div>

      <motion.div variants={staggerItem} className="mt-9">
        <div className="mb-3 flex items-center justify-between">
          <SectionTitle className="mb-0">最近项目</SectionTitle>
          <button onClick={() => navigate('/projects')} className="flex items-center gap-1 text-[12px] text-ink-dim hover:text-ink">
            查看全部 <ArrowRight size={13} />
          </button>
        </div>

        {loading ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Card key={i} className="p-4">
                <Skeleton className="h-4 w-1/2" />
                <Skeleton className="mt-3 h-3 w-full" />
                <Skeleton className="mt-2 h-3 w-2/3" />
              </Card>
            ))}
          </div>
        ) : projects.length === 0 ? (
          <Card className="py-4">
            <EmptyState
              icon={<FolderKanban size={22} />}
              title={projectsQ.isError ? '暂时无法连接后端' : '还没有项目'}
              description={projectsQ.isError ? '后端未就绪，稍后自动重试，或先在离线模式浏览界面。' : '创建你的第一个项目，从想法开始一条完整的开发流程。'}
              action={
                projectsQ.isError ? (
                  <Button variant="secondary" onClick={() => projectsQ.refetch()}>
                    重试连接
                  </Button>
                ) : (
                  <Button variant="primary" onClick={() => navigate('/projects?new=1')}>
                    <Plus size={16} /> 新建项目
                  </Button>
                )
              }
            />
          </Card>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {projects.slice(0, 6).map((p) => (
              <Card key={p.id} interactive className="group p-4" onClick={() => openProject(p)}>
                <div className="flex items-start justify-between gap-2">
                  <h3 className="line-clamp-1 font-medium text-ink">{p.title}</h3>
                  <StatusPill tone={projectTone[p.status] ?? 'neutral'} pulse={p.status === 'active'} label={p.status} />
                </div>
                <p className="mt-1.5 line-clamp-2 min-h-8 text-[13px] leading-relaxed text-ink-dim">
                  {p.description || '暂无描述'}
                </p>
                <div className="mt-3 flex items-center justify-between text-[11px] text-ink-mute">
                  <span className="flex items-center gap-1.5">
                    {p.git_branch && (
                      <>
                        <GitBranch size={12} /> {p.git_branch}
                      </>
                    )}
                  </span>
                  <span>{relTime(p.updated_at)}</span>
                </div>
              </Card>
            ))}
          </div>
        )}
      </motion.div>

      <motion.div variants={staggerItem} className="mt-9">
        <SectionTitle>进行中的工作流</SectionTitle>
        {activeFlows.length === 0 ? (
          <Card className="flex items-center gap-3 px-4 py-3.5 text-[13px] text-ink-dim">
            <Workflow size={16} className="text-ink-mute" />
            暂无进行中的工作流。
            <button onClick={() => navigate('/flows?new=1')} className="text-ink hover:underline">
              发起一个
            </button>
          </Card>
        ) : (
          <div className="space-y-2">
            {activeFlows.slice(0, 5).map((f) => {
              const activeStage = f.stages.find((s) => s.status === 'active' || s.status === 'waiting_gate');
              return (
                <Card
                  key={f.id}
                  interactive
                  className="flex items-center justify-between gap-4 px-4 py-3"
                  onClick={() => navigate(`/workbench/${f.project_id}/${activeStage?.type ?? 'idea'}`)}
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium text-ink">{f.template_id}</span>
                      <Badge tone="info">{activeStage?.name ?? '—'}</Badge>
                    </div>
                    <span className="font-mono text-[11px] text-ink-mute">{f.project_id.slice(0, 8)}</span>
                  </div>
                  <MiniFlow stages={f.stages} />
                </Card>
              );
            })}
          </div>
        )}
      </motion.div>
    </PageShell>
  );
}
