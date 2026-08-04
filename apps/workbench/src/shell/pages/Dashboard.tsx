import { useMemo } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import {
  FolderKanban, Workflow, ShieldAlert, Plus, ArrowRight, ArrowUpRight, GitBranch,
  Terminal, Bell, Camera, MessagesSquare, Compass, Bot, Search, ShieldCheck,
  Telescope, type LucideIcon,
} from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import { Card, Button, StatusPill, EmptyState, Skeleton, Badge, Kbd } from '../../ui';
import { MiniFlow } from '../../stages/MiniFlow';
import { STAGE_BY_TYPE } from '../../stages/stageMeta';
import { LogoMark } from '../Logo';
import { useProjects, useFlows, useAgents } from '../../lib/queries';
import { useLayoutStore } from '../../stores/layout';
import { useActivityStore } from '../../stores/activity';
import { useShellStore } from '../../stores/shell';
import { greeting, relTime } from '../../lib/format';
import { modLabel } from '../../lib/platform';
import { staggerItem } from '../../lib/motion';
import { roleLabel } from '../../services-bridge/agents';
import type { AgentInfo } from '../../services-bridge/agents';
import type { Project } from '../../../types';
import type { StageType } from '../../services-bridge/flows';

const projectTone: Record<string, 'success' | 'info' | 'warn' | 'accent' | 'neutral'> = {
  active: 'success',
  planning: 'info',
  paused: 'warn',
  completed: 'accent',
  archived: 'neutral',
};

/** Which stage each builtin agent opens when you start a chat from the arena. */
const AGENT_HOME_STAGE: Record<string, StageType> = {
  'builtin-flow-conductor': 'planning',
  'builtin-code-artisan': 'coding',
  'builtin-scout': 'research',
  'builtin-red-critic': 'review',
  'builtin-deep-researcher': 'research',
};

/** Lucide stand-ins, keyed by the builtin emoji avatar (backend field). */
const AGENT_ICON: Record<string, LucideIcon> = {
  '🎯': Compass,
  '🛠️': Bot,
  '🔍': Search,
  '🛡️': ShieldCheck,
  '📚': Telescope,
};

function agentIcon(a: AgentInfo): LucideIcon {
  return (a.avatar && AGENT_ICON[a.avatar]) || MessagesSquare;
}

/** The four hero command tiles, in brief order. */
const HERO_ACTIONS = [
  { key: 'new-project', label: '新建项目', desc: '从想法开始一条完整流程', icon: Plus },
  { key: 'new-flow', label: '新建工作流', desc: '在项目中发起七阶段流程', icon: Workflow },
  { key: 'return', label: '回到工作台', desc: '继续上次离开的位置', icon: ArrowUpRight },
  { key: 'palette', label: '命令面板', desc: '全部入口与跳转，一搜即达', icon: Terminal },
] as const;

export default function Dashboard() {
  const navigate = useNavigate();
  const setLoc = useLayoutStore((s) => s.setWorkbenchLocation);
  const lastProjectId = useLayoutStore((s) => s.lastProjectId);
  const lastStage = useLayoutStore((s) => s.lastStage);
  const setCommandOpen = useShellStore((s) => s.setCommandOpen);
  const projectsQ = useProjects();
  const flowsQ = useFlows();
  const agentsQ = useAgents();
  const activities = useActivityStore((s) => s.entries);

  const projects = projectsQ.data?.projects ?? [];
  const flows = flowsQ.data?.items ?? [];
  const loading = projectsQ.isLoading || flowsQ.isLoading;
  const activeFlows = flows.filter((f) => f.status === 'active');
  const waitingGates = flows.flatMap((f) =>
    f.stages.filter((s) => s.status === 'waiting_gate').map((s) => ({ flow: f, stage: s })),
  );
  const builtinAgents = (agentsQ.data ?? []).filter((a) => a.source === 'builtin').slice(0, 5);

  const todayEvents = activities.filter((e) => {
    const t = Date.parse(e.at);
    const now = new Date();
    return Number.isFinite(t) && new Date(t).toDateString() === now.toDateString();
  }).length;

  const snapshotCount = useMemo(
    () =>
      flows.reduce(
        (n, f) => n + new Set(f.stages.map((s) => s.snapshot_id).filter(Boolean)).size,
        0,
      ),
    [flows],
  );

  const projectTitle = (id: string) => projects.find((p) => p.id === id)?.title ?? id.slice(0, 8);
  const lastProject = lastProjectId ? projects.find((p) => p.id === lastProjectId) : undefined;
  const lastStageLabel = STAGE_BY_TYPE[lastStage]?.label ?? (lastStage as string);

  const openProject = (p: Project) => {
    setLoc(p.id, 'idea');
    navigate(`/workbench/${p.id}/idea`);
  };

  const openGate = (projectId: string, stage: StageType) => {
    setLoc(projectId, stage);
    navigate(`/workbench/${projectId}/${stage}?gate=1`);
  };

  /** Arena CTA: land on the agent's home stage (mock mode picks it), or the projects picker when there is nothing to work in. */
  const startChat = (agent: AgentInfo) => {
    const stage = STAGE_BY_TYPE[AGENT_HOME_STAGE[agent.id] ?? 'coding'] ? AGENT_HOME_STAGE[agent.id] : 'coding';
    const target = (lastProjectId ? projects.find((p) => p.id === lastProjectId) : undefined) ?? projects[0];
    if (target) {
      setLoc(target.id, stage);
      navigate(`/workbench/${target.id}/${stage}?agent=${agent.id}`);
    } else {
      navigate('/projects');
    }
  };

  const heroHandlers: Record<(typeof HERO_ACTIONS)[number]['key'], () => void> = {
    'new-project': () => navigate('/projects?new=1'),
    'new-flow': () => navigate('/flows?new=1'),
    return: () => {
      if (lastProjectId) navigate(`/workbench/${lastProjectId}/${lastStage}`);
    },
    palette: () => setCommandOpen(true),
  };

  const opsCounts = [
    { label: '项目', value: projects.length, icon: FolderKanban, to: '/projects' },
    { label: '进行中', value: activeFlows.length, icon: Workflow, to: '/flows' },
    { label: '快照', value: snapshotCount, icon: Camera, to: undefined },
    { label: '今日事件', value: todayEvents, icon: Bell, to: undefined },
  ];

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
      {/* ---- Hero: brand card + the four commands ---- */}
      <motion.section variants={staggerItem} className="grid gap-4 lg:grid-cols-5" aria-label="从这里开始">
        <Card className="flex flex-col justify-between gap-8 p-6 lg:col-span-3 lg:p-7">
          <LogoMark size={34} />
          <div>
            <h2 className="font-display text-[28px] font-bold leading-tight tracking-tight text-ink lg:text-[32px]">
              从想法到提交
            </h2>
            <p className="mt-2 max-w-md text-[13px] leading-relaxed text-ink-dim">
              一条七阶段工作流驱动整个开发过程
              <span className="mx-1.5 inline-block h-[2px] w-8 translate-y-[-3px] rounded-full bg-gradient-to-r from-[var(--brand-from)] to-[var(--brand-to)]" />
              阶段画布随进程变形，Agent 伴侣全程协同，守卫与快照守住每一步。
            </p>
            <div className="mt-5 flex flex-wrap items-center gap-x-4 gap-y-2">
              {opsCounts.map((c) => {
                const Icon = c.icon;
                const inner = (
                  <>
                    <Icon size={13} className="text-ink-mute" />
                    <span className="text-ink-dim">{c.label}</span>
                    <span className="nums inline-flex items-center font-semibold text-ink">
                      {loading && projects.length === 0 ? <Skeleton className="h-3.5 w-5" /> : c.value}
                    </span>
                  </>
                );
                return c.to ? (
                  <Link
                    key={c.label}
                    to={c.to}
                    className="flex items-center gap-1.5 text-[12px] hover:text-ink"
                  >
                    {inner}
                  </Link>
                ) : (
                  <span key={c.label} className="flex items-center gap-1.5 text-[12px]">
                    {inner}
                  </span>
                );
              })}
            </div>
          </div>
        </Card>

        <div className="grid grid-cols-2 gap-3 lg:col-span-2">
          {HERO_ACTIONS.map((a) => {
            const Icon = a.icon;
            const disabled = a.key === 'return' && !lastProjectId;
            return (
              <button
                key={a.key}
                type="button"
                disabled={disabled}
                onClick={heroHandlers[a.key]}
                className="card card-interactive flex flex-col items-start justify-between gap-4 rounded-[var(--cf-radius-card)] border border-[var(--elev-rest-border)] bg-surface p-4 text-left shadow-cf-rest transition duration-150 ease-[var(--ease-flow)] hover:border-[var(--elev-lift-border)] hover:bg-hover disabled:pointer-events-none disabled:opacity-45"
              >
                <span className="flex w-full items-start justify-between">
                  <span className="grid size-8 place-items-center rounded-lg bg-tint text-ink-dim">
                    <Icon size={16} />
                  </span>
                  {a.key === 'new-project' ? (
                    <span className="mt-1.5 size-1 rounded-full bg-[var(--brand-from)]" aria-hidden />
                  ) : a.key === 'new-flow' ? (
                    <span className="mt-1.5 size-1 rounded-full bg-[var(--brand-to)]" aria-hidden />
                  ) : a.key === 'palette' ? (
                    <span className="flex items-center gap-1">
                      <Kbd>{modLabel()}</Kbd>
                      <Kbd>K</Kbd>
                    </span>
                  ) : null}
                </span>
                <span>
                  <span className="block text-sm font-semibold text-ink">{a.label}</span>
                  <span className="mt-1 block text-[11px] leading-snug text-ink-mute">
                    {a.key === 'return'
                      ? disabled
                        ? '没有工作台记录，先打开一个项目'
                        : projectsQ.isLoading
                          ? a.desc
                          : `${lastProject?.title ?? '上次项目'} · ${lastStageLabel}`
                      : a.desc}
                  </span>
                </span>
              </button>
            );
          })}
        </div>
      </motion.section>

      {/* ---- Waiting-gate inbox: persistent strip, only when something waits ---- */}
      {waitingGates.length > 0 && (
        <motion.div variants={staggerItem} className="mt-5">
          <Card className="card-accent-waiting p-4" aria-label="等待你审批的 Gate">
            <div className="flex items-center justify-between gap-3">
              <span className="flex items-center gap-2 text-sm font-semibold text-ink">
                <ShieldAlert size={15} className="text-warn" />
                {waitingGates.length} 个 Gate 等待你的决定
              </span>
              <span className="text-[11px] text-ink-mute">进入工作台完成审批</span>
            </div>
            <ul className="mt-3 space-y-1.5">
              {waitingGates.map(({ flow, stage }) => (
                <li key={`${flow.id}-${stage.id}`}>
                  <button
                    type="button"
                    onClick={() => openGate(flow.project_id, stage.type)}
                    className="group flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-tint"
                  >
                    <span className="min-w-0">
                      <span className="block truncate text-[13px] font-medium text-ink">
                        {projectTitle(flow.project_id)}
                      </span>
                      <span className="mt-0.5 block text-[11px] text-ink-mute">
                        阶段「{stage.name}」 · {flow.template_id}
                      </span>
                    </span>
                    <span className="flex shrink-0 items-center gap-2">
                      <Badge tone="warn">待审批</Badge>
                      <ArrowRight size={14} className="text-ink-mute transition-transform group-hover:translate-x-0.5" />
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </Card>
        </motion.div>
      )}

      {/* ---- Recent projects ---- */}
      <motion.div variants={staggerItem} className="mt-9">
        <div className="mb-3 flex items-center justify-between">
          <SectionTitle className="mb-0">最近项目</SectionTitle>
          <button
            onClick={() => navigate('/projects')}
            className="flex items-center gap-1 text-[12px] text-ink-dim hover:text-ink"
          >
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
              description={
                projectsQ.isError
                  ? '后端未就绪，稍后自动重试，或先在离线模式浏览界面。'
                  : '创建你的第一个项目，从想法开始一条完整的开发流程。'
              }
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

      {/* ---- Active flows, one row per flow with the seven-node timeline ---- */}
      <motion.div variants={staggerItem} className="mt-9">
        <div className="mb-3 flex items-center justify-between">
          <SectionTitle className="mb-0">进行中的工作流</SectionTitle>
          <button
            onClick={() => navigate('/flows')}
            className="flex items-center gap-1 text-[12px] text-ink-dim hover:text-ink"
          >
            查看全部 <ArrowRight size={13} />
          </button>
        </div>
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
              const waiting = activeStage?.status === 'waiting_gate';
              return (
                <Card
                  key={f.id}
                  className={waiting ? 'card-accent-waiting' : undefined}
                >
                  <button
                    type="button"
                    onClick={() => navigate(`/workbench/${f.project_id}/${activeStage?.type ?? 'idea'}`)}
                    className="group flex w-full items-center justify-between gap-4 rounded-[var(--cf-radius-card)] px-4 py-3 text-left transition-colors hover:bg-hover"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-ink">
                          {projectTitle(f.project_id)}
                        </span>
                        <Badge tone={waiting ? 'warn' : 'info'}>
                          {activeStage?.name ?? '—'}
                        </Badge>
                      </div>
                      <span className="font-mono text-[11px] text-ink-mute">{f.template_id}</span>
                    </div>
                    <span className="flex items-center gap-3">
                      <MiniFlow stages={f.stages} className="hidden sm:flex" />
                      <ArrowRight size={14} className="shrink-0 text-ink-mute transition-transform group-hover:translate-x-0.5" />
                    </span>
                  </button>
                </Card>
              );
            })}
          </div>
        )}
      </motion.div>

      {/* ---- Agent arena: the five builtin specialists ---- */}
      <motion.div variants={staggerItem} className="mb-9 mt-9">
        <div className="mb-3 flex items-center justify-between">
          <SectionTitle className="mb-0">开始一次对话</SectionTitle>
          <button
            onClick={() => navigate('/agents')}
            className="flex items-center gap-1 text-[12px] text-ink-dim hover:text-ink"
          >
            Agent 广场 <ArrowRight size={13} />
          </button>
        </div>
        {agentsQ.isLoading ? (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
            {Array.from({ length: 5 }).map((_, i) => (
              <Card key={i} className="p-4">
                <Skeleton className="size-8 rounded-lg" />
                <Skeleton className="mt-3 h-4 w-2/3" />
                <Skeleton className="mt-2 h-3 w-full" />
              </Card>
            ))}
          </div>
        ) : builtinAgents.length === 0 ? (
          <Card className="flex items-center gap-3 px-4 py-3.5 text-[13px] text-ink-dim">
            <MessagesSquare size={16} className="text-ink-mute" />
            Agent 注册表暂不可用，内置角色将在连接恢复后出现。
          </Card>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
            {builtinAgents.map((a) => {
              const Icon = agentIcon(a);
              return (
                <Card key={a.id} className="flex h-full flex-col p-4">
                  <div className="flex items-center gap-2">
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-tint text-ink-dim">
                      <Icon size={15} />
                    </span>
                    <span className="truncate text-sm font-semibold text-ink">{a.name}</span>
                  </div>
                  <p className="mt-2 line-clamp-2 min-h-8 text-[12px] leading-relaxed text-ink-dim">
                    {a.description ?? roleLabel(a.role_base)}
                  </p>
                  <div className="mt-auto flex items-center justify-between gap-2 border-t border-line pt-3">
                    <Badge tone="accent">{roleLabel(a.role_base)}</Badge>
                    <button
                      type="button"
                      onClick={() => startChat(a)}
                      className="flex items-center gap-1 text-[12px] font-medium text-ink-dim transition-colors hover:text-ink"
                    >
                      开始对话 <ArrowUpRight size={13} />
                    </button>
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </motion.div>
    </PageShell>
  );
}
