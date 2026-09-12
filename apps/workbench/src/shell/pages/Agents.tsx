import { useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  AlertCircle,
  Bot,
  Cpu,
  Pencil,
  Plus,
  Search,
  ShieldCheck,
  Trash2,
  Wrench,
} from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import {
  Badge,
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  EmptyState,
  IconButton,
  Input,
  Skeleton,
  StatusPill,
  Switch,
  Tooltip,
} from '../../ui';
import { useAgentRegistry, qk } from '../../lib/queries';
import {
  createAgent,
  deleteAgent,
  roleLabel,
  updateAgent,
  type AgentInfo,
  type AgentInput,
} from '../../services-bridge/agents';
import { AgentEditorDialog } from './AgentEditorDialog';

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function AgentRow({
  agent,
  onEdit,
  onToggle,
  onDelete,
  toggling,
}: {
  agent: AgentInfo;
  onEdit?: () => void;
  onToggle?: (enabled: boolean) => void;
  onDelete?: () => void;
  toggling?: boolean;
}) {
  const builtin = agent.source === 'builtin';
  const capabilityCount = (agent.mounts?.mcp_tools?.length ?? 0) + (agent.mounts?.skills?.length ?? 0);
  return (
    <Card className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-tint-active text-ink-dim">
          {agent.role_base === 'critic' ? <ShieldCheck size={17} /> : <Bot size={17} />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate text-sm font-semibold text-ink">{agent.name}</h3>
            <Badge>{roleLabel(agent.role_base)}</Badge>
            <StatusPill
              tone={agent.enabled === false ? 'neutral' : 'success'}
              label={agent.enabled === false ? '已停用' : '可用'}
            />
          </div>
          <p className="mt-1 line-clamp-2 text-[13px] leading-relaxed text-ink-dim">
            {agent.description || '尚未填写说明。'}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-ink-mute">
            <span className="inline-flex items-center gap-1">
              <Cpu size={12} /> {agent.binding?.model || '继承默认模型'}
            </span>
            <span className="inline-flex items-center gap-1">
              <Wrench size={12} /> {capabilityCount ? `${capabilityCount} 项能力` : '未挂载能力'}
            </span>
            {!!agent.stage_tags?.length && <span>{agent.stage_tags.length} 个适用阶段</span>}
          </div>
        </div>
      </div>

      {builtin ? (
        <Badge className="self-start sm:self-center">内置</Badge>
      ) : (
        <div className="flex items-center gap-1 self-end sm:self-center">
          <span className="mr-2 text-xs text-ink-mute">启用</span>
          <Switch
            checked={agent.enabled !== false}
            disabled={toggling}
            aria-label={`${agent.enabled === false ? '启用' : '停用'} ${agent.name}`}
            onCheckedChange={onToggle}
          />
          <Tooltip content="编辑 Agent" side="top">
            <IconButton size="sm" aria-label={`编辑 ${agent.name}`} onClick={onEdit}>
              <Pencil size={14} />
            </IconButton>
          </Tooltip>
          <Tooltip content="删除 Agent" side="top">
            <IconButton
              size="sm"
              aria-label={`删除 ${agent.name}`}
              className="hover:bg-danger/10 hover:text-danger"
              onClick={onDelete}
            >
              <Trash2 size={14} />
            </IconButton>
          </Tooltip>
        </div>
      )}
    </Card>
  );
}

export default function Agents() {
  const agentsQ = useAgentRegistry();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<AgentInfo | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<AgentInfo | null>(null);

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: qk.agentRegistry });
    queryClient.invalidateQueries({ queryKey: qk.agents });
  };
  const saveM = useMutation({
    mutationFn: ({ agent, input }: { agent?: AgentInfo | null; input: AgentInput }) =>
      agent ? updateAgent(agent.id, input) : createAgent(input),
    onSuccess: (_, variables) => {
      refresh();
      setEditing(undefined);
      toast.success(variables.agent ? 'Agent 已更新' : 'Agent 已创建');
    },
  });
  const toggleM = useMutation({
    mutationFn: ({ agent, enabled }: { agent: AgentInfo; enabled: boolean }) =>
      updateAgent(agent.id, { enabled }),
    onSuccess: (_, variables) => {
      refresh();
      toast.success(variables.enabled ? 'Agent 已启用' : 'Agent 已停用');
    },
    onError: (error) => toast.error(`状态更新失败：${errorText(error)}`),
  });
  const deleteM = useMutation({
    mutationFn: (agent: AgentInfo) => deleteAgent(agent.id),
    onSuccess: () => {
      refresh();
      setDeleting(null);
      toast.success('Agent 已删除');
    },
  });

  const filtered = useMemo(() => {
    const keyword = search.trim().toLocaleLowerCase();
    const items = agentsQ.data ?? [];
    if (!keyword) return items;
    return items.filter((agent) =>
      [agent.name, agent.description, roleLabel(agent.role_base), ...(agent.stage_tags ?? [])]
        .filter(Boolean)
        .some((value) => String(value).toLocaleLowerCase().includes(keyword)),
    );
  }, [agentsQ.data, search]);
  const builtins = filtered.filter((agent) => agent.source === 'builtin');
  const custom = filtered.filter((agent) => agent.source !== 'builtin');

  return (
    <PageShell
      title="Agent"
      subtitle="管理可复用角色，并把自定义 Agent 分配给流程阶段"
      maxWidth="max-w-5xl"
      actions={
        <Button
          variant="primary"
          onClick={() => {
            saveM.reset();
            setEditing(null);
          }}
        >
          <Plus size={16} /> 新建 Agent
        </Button>
      }
    >
      <div className="mb-6 flex items-center justify-between gap-3">
        <div className="relative w-full max-w-sm">
          <Search size={15} className="pointer-events-none absolute left-3 top-2.5 text-ink-mute" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索名称、角色或适用阶段"
            aria-label="搜索 Agent"
            className="pl-9"
          />
        </div>
        {!agentsQ.isLoading && !agentsQ.isError && (
          <span className="shrink-0 text-xs tabular-nums text-ink-mute">{filtered.length} 个 Agent</span>
        )}
      </div>

      {agentsQ.isLoading ? (
        <div className="space-y-3" aria-label="正在加载 Agent">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-24 w-full rounded-xl" />
          ))}
        </div>
      ) : agentsQ.isError ? (
        <Card className="flex flex-col items-start gap-3 p-5 sm:flex-row sm:items-center">
          <AlertCircle size={20} className="shrink-0 text-danger" />
          <div className="flex-1">
            <h2 className="text-sm font-semibold text-ink">无法读取 Agent Registry</h2>
            <p className="mt-1 text-[13px] text-ink-dim">{errorText(agentsQ.error)}。请确认后端已启用 Agent Registry。</p>
          </div>
          <Button size="sm" onClick={() => agentsQ.refetch()}>重试</Button>
        </Card>
      ) : filtered.length === 0 && search ? (
        <Card>
          <EmptyState
            icon={<Search size={22} />}
            title="没有匹配的 Agent"
            description="调整搜索词，或创建一个新的自定义 Agent。"
            action={<Button size="sm" onClick={() => setSearch('')}>清除搜索</Button>}
          />
        </Card>
      ) : (
        <>
          <section aria-labelledby="builtin-agent-title">
            <SectionTitle className="flex items-center justify-between">
              <span id="builtin-agent-title">内置 Agent</span>
              <span className="font-normal normal-case tracking-normal">{builtins.length}</span>
            </SectionTitle>
            <div className="space-y-2">
              {builtins.map((agent) => <AgentRow key={agent.id} agent={agent} />)}
            </div>
          </section>

          <section className="mt-8" aria-labelledby="custom-agent-title">
            <SectionTitle className="flex items-center justify-between">
              <span id="custom-agent-title">自定义 Agent</span>
              <span className="font-normal normal-case tracking-normal">{custom.length}</span>
            </SectionTitle>
            {custom.length ? (
              <div className="space-y-2">
                {custom.map((agent) => (
                  <AgentRow
                    key={agent.id}
                    agent={agent}
                    toggling={toggleM.isPending && toggleM.variables?.agent.id === agent.id}
                    onEdit={() => {
                      saveM.reset();
                      setEditing(agent);
                    }}
                    onToggle={(enabled) => toggleM.mutate({ agent, enabled })}
                    onDelete={() => {
                      deleteM.reset();
                      setDeleting(agent);
                    }}
                  />
                ))}
              </div>
            ) : (
              <Card>
                <EmptyState
                  icon={<Bot size={22} />}
                  title={search ? '没有匹配的自定义 Agent' : '还没有自定义 Agent'}
                  description={search ? '可调整搜索词查看其他 Agent。' : '创建角色后，即可在流程模板的阶段中分配负责人。'}
                  action={!search ? (
                    <Button size="sm" onClick={() => setEditing(null)}>
                      <Plus size={14} /> 新建 Agent
                    </Button>
                  ) : undefined}
                />
              </Card>
            )}
          </section>
        </>
      )}

      <AgentEditorDialog
        open={editing !== undefined}
        agent={editing}
        saving={saveM.isPending}
        error={saveM.isError ? errorText(saveM.error) : undefined}
        onOpenChange={(open) => !open && setEditing(undefined)}
        onSave={(input) => saveM.mutate({ agent: editing, input })}
      />

      <Dialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent className="w-[min(92vw,440px)]">
          <DialogTitle>删除 Agent</DialogTitle>
          <DialogDescription>
            将永久删除“{deleting?.name}”。已经保存到流程模板中的 Agent ID 不会自动改写。
          </DialogDescription>
          {deleteM.isError && (
            <p role="alert" className="mt-4 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-[13px] text-danger">
              {errorText(deleteM.error)}
            </p>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setDeleting(null)} disabled={deleteM.isPending}>取消</Button>
            <Button variant="danger" loading={deleteM.isPending} onClick={() => deleting && deleteM.mutate(deleting)}>
              <Trash2 size={15} /> 确认删除
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}
