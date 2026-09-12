import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  AlertCircle,
  ArrowRight,
  Bot,
  Copy,
  Layers,
  Pencil,
  Plus,
  ShieldCheck,
  Trash2,
  Workflow,
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
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  StatusPill,
  Tooltip,
} from '../../ui';
import { MiniFlow } from '../../stages/MiniFlow';
import { useAgentRegistry, useFlows, useFlowTemplates, useProjects, qk } from '../../lib/queries';
import {
  createFlow,
  deleteFlowTemplate,
  saveFlowTemplate,
  type Flow,
  type FlowTemplateInfo,
  type FlowTemplateInput,
} from '../../services-bridge/flows';
import { relTime } from '../../lib/format';
import { FlowTemplateEditorDialog } from './FlowTemplateEditorDialog';

const flowTone: Record<string, 'success' | 'accent' | 'danger'> = {
  active: 'success',
  completed: 'accent',
  aborted: 'danger',
};

const flowStatusLabel: Record<string, string> = {
  active: '进行中',
  completed: '已完成',
  aborted: '已终止',
};

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isBuiltin(template: FlowTemplateInfo): boolean {
  return template.source === 'builtin' || template.id === 'new_project' || template.id === 'import_project';
}

function TemplateRow({
  template,
  onUse,
  onCopy,
  onEdit,
  onDelete,
}: {
  template: FlowTemplateInfo;
  onUse: () => void;
  onCopy: () => void;
  onEdit?: () => void;
  onDelete?: () => void;
}) {
  const stages = template.stages ?? [];
  const assigned = stages.filter((stage) => stage.agent_id).length;
  const approvals = stages.filter((stage) => stage.gates?.some((gate) => gate.kind === 'human_approval')).length;
  const builtin = isBuiltin(template);
  return (
    <Card className="p-4">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center">
        <div className="flex min-w-0 flex-1 items-start gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-tint-active text-ink-dim">
            <Workflow size={17} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-sm font-semibold text-ink">{template.name || template.id}</h3>
              <Badge>{builtin ? '内置' : '自定义'}</Badge>
              <span className="font-mono text-[11px] text-ink-mute">{template.id}</span>
            </div>
            <p className="mt-1 line-clamp-2 text-[13px] leading-relaxed text-ink-dim">
              {template.description || '可复用的 CodeFlow 流程模板。'}
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-1.5" aria-label="模板阶段顺序">
              {stages.map((stage, index) => (
                <span key={`${stage.type}-${index}`} className="inline-flex items-center gap-1.5">
                  {index > 0 && <ArrowRight size={11} className="text-ink-mute" />}
                  <Badge tone={stage.optional ? 'neutral' : 'accent'}>
                    {stage.name || stage.type}{stage.optional ? ' · 可选' : ''}
                  </Badge>
                </span>
              ))}
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 lg:justify-end">
          <div className="flex items-center gap-3 text-xs text-ink-mute">
            <span className="inline-flex items-center gap-1"><Bot size={13} /> {assigned} 个指定 Agent</span>
            <span className="inline-flex items-center gap-1"><ShieldCheck size={13} /> {approvals} 个人工确认</span>
          </div>
          <div className="flex items-center gap-1">
            <Tooltip content="复制为自定义模板" side="top">
              <IconButton size="sm" aria-label={`复制 ${template.name || template.id}`} onClick={onCopy}>
                <Copy size={14} />
              </IconButton>
            </Tooltip>
            {!builtin && (
              <>
                <Tooltip content="编辑模板" side="top">
                  <IconButton size="sm" aria-label={`编辑 ${template.name || template.id}`} onClick={onEdit}>
                    <Pencil size={14} />
                  </IconButton>
                </Tooltip>
                <Tooltip content="删除模板" side="top">
                  <IconButton
                    size="sm"
                    aria-label={`删除 ${template.name || template.id}`}
                    className="hover:bg-danger/10 hover:text-danger"
                    onClick={onDelete}
                  >
                    <Trash2 size={14} />
                  </IconButton>
                </Tooltip>
              </>
            )}
            <Button size="sm" variant="primary" onClick={onUse}>
              使用模板 <ArrowRight size={14} />
            </Button>
          </div>
        </div>
      </div>
    </Card>
  );
}

export default function Flows() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const templatesQ = useFlowTemplates();
  const flowsQ = useFlows();
  const projectsQ = useProjects();
  const agentsQ = useAgentRegistry();

  const [launchOpen, setLaunchOpen] = useState(false);
  const [projectId, setProjectId] = useState('');
  const [templateId, setTemplateId] = useState('');
  const [editor, setEditor] = useState<{ initial?: FlowTemplateInfo | null; editing: boolean } | null>(null);
  const [deleting, setDeleting] = useState<FlowTemplateInfo | null>(null);

  useEffect(() => {
    if (params.get('new') === '1') setLaunchOpen(true);
  }, [params]);

  const templates = useMemo<FlowTemplateInfo[]>(() => {
    if (templatesQ.data?.items?.length) return templatesQ.data.items;
    return (templatesQ.data?.ids ?? []).map((id) => ({ id, name: id }));
  }, [templatesQ.data]);
  const flows = flowsQ.data?.items ?? [];
  const projects = projectsQ.data?.projects ?? [];

  useEffect(() => {
    if (!templateId && templates.length) setTemplateId(templates[0].id);
  }, [templateId, templates]);

  const closeLaunch = (next: boolean) => {
    setLaunchOpen(next);
    if (!next && params.get('new')) {
      const nextParams = new URLSearchParams(params);
      nextParams.delete('new');
      setParams(nextParams, { replace: true });
    }
  };

  const createM = useMutation({
    mutationFn: (input: { project_id: string; template_id: string }) => createFlow(input),
    onSuccess: (flow: Flow) => {
      queryClient.invalidateQueries({ queryKey: qk.flows() });
      toast.success('工作流已创建');
      closeLaunch(false);
      const active = flow.stages.find((stage) => stage.status === 'active') ?? flow.stages[0];
      navigate(`/workbench/${flow.project_id}/${active?.type ?? 'idea'}`);
    },
  });
  const saveTemplateM = useMutation({
    mutationFn: (input: FlowTemplateInput) => saveFlowTemplate(input),
    onSuccess: (_, input) => {
      queryClient.invalidateQueries({ queryKey: qk.templates });
      setEditor(null);
      setTemplateId(input.id);
      toast.success('流程模板已保存');
    },
  });
  const deleteTemplateM = useMutation({
    mutationFn: (template: FlowTemplateInfo) => deleteFlowTemplate(template.id),
    onSuccess: (_, template) => {
      queryClient.invalidateQueries({ queryKey: qk.templates });
      if (templateId === template.id) setTemplateId('');
      setDeleting(null);
      toast.success('流程模板已删除');
    },
  });

  const launch = (template: FlowTemplateInfo) => {
    createM.reset();
    setTemplateId(template.id);
    setLaunchOpen(true);
  };

  return (
    <PageShell
      title="流程"
      subtitle="编排可复用模板，并从模板发起真实工作流"
      maxWidth="max-w-6xl"
      actions={
        <>
          <Button variant="secondary" onClick={() => {
            saveTemplateM.reset();
            setEditor({ initial: null, editing: false });
          }}>
            <Layers size={15} /> 新建模板
          </Button>
          <Button variant="primary" onClick={() => setLaunchOpen(true)}>
            <Plus size={16} /> 发起工作流
          </Button>
        </>
      }
    >
      <section aria-labelledby="flow-template-title">
        <SectionTitle className="flex items-center justify-between">
          <span id="flow-template-title">流程模板</span>
          {!templatesQ.isLoading && !templatesQ.isError && (
            <span className="font-normal normal-case tracking-normal">{templates.length}</span>
          )}
        </SectionTitle>
        {templatesQ.isLoading ? (
          <div className="space-y-3" aria-label="正在加载流程模板">
            {Array.from({ length: 3 }).map((_, index) => <Skeleton key={index} className="h-32 w-full rounded-xl" />)}
          </div>
        ) : templatesQ.isError ? (
          <Card className="flex flex-col items-start gap-3 p-5 sm:flex-row sm:items-center">
            <AlertCircle size={20} className="shrink-0 text-danger" />
            <div className="flex-1">
              <h3 className="text-sm font-semibold text-ink">无法读取流程模板</h3>
              <p className="mt-1 text-[13px] text-ink-dim">{errorText(templatesQ.error)}。请确认后端已启用 Flow Engine。</p>
            </div>
            <Button size="sm" onClick={() => templatesQ.refetch()}>重试</Button>
          </Card>
        ) : templates.length === 0 ? (
          <Card>
            <EmptyState
              icon={<Layers size={22} />}
              title="还没有流程模板"
              description="创建第一套阶段编排，保存后即可用于多个项目。"
              action={<Button size="sm" onClick={() => setEditor({ initial: null, editing: false })}><Plus size={14} /> 新建模板</Button>}
            />
          </Card>
        ) : (
          <div className="space-y-3">
            {templates.map((template) => (
              <TemplateRow
                key={template.id}
                template={template}
                onUse={() => launch(template)}
                onCopy={() => {
                  saveTemplateM.reset();
                  setEditor({ initial: template, editing: false });
                }}
                onEdit={() => {
                  saveTemplateM.reset();
                  setEditor({ initial: template, editing: true });
                }}
                onDelete={() => {
                  deleteTemplateM.reset();
                  setDeleting(template);
                }}
              />
            ))}
          </div>
        )}
      </section>

      <section className="mt-9" aria-labelledby="running-flow-title">
        <SectionTitle className="flex items-center justify-between">
          <span id="running-flow-title">工作流实例</span>
          {!flowsQ.isLoading && !flowsQ.isError && (
            <span className="font-normal normal-case tracking-normal">{flows.length}</span>
          )}
        </SectionTitle>
        {flowsQ.isLoading ? (
          <div className="space-y-2" aria-label="正在加载工作流">
            {Array.from({ length: 3 }).map((_, index) => <Skeleton key={index} className="h-16 w-full rounded-xl" />)}
          </div>
        ) : flowsQ.isError ? (
          <Card className="flex items-center gap-3 p-4">
            <AlertCircle size={18} className="text-danger" />
            <p className="flex-1 text-[13px] text-ink-dim">无法读取工作流：{errorText(flowsQ.error)}</p>
            <Button size="sm" onClick={() => flowsQ.refetch()}>重试</Button>
          </Card>
        ) : flows.length === 0 ? (
          <Card className="flex flex-col items-start gap-3 p-4 sm:flex-row sm:items-center">
            <Workflow size={18} className="text-ink-mute" />
            <div className="flex-1">
              <h3 className="text-sm font-medium text-ink">还没有工作流实例</h3>
              <p className="mt-0.5 text-xs text-ink-dim">选择模板和项目后即可开始。</p>
            </div>
            <Button size="sm" onClick={() => setLaunchOpen(true)}>发起第一个</Button>
          </Card>
        ) : (
          <div className="space-y-2">
            {flows.map((flow) => {
              const active = flow.stages.find((stage) => stage.status === 'active' || stage.status === 'waiting_gate');
              const template = templates.find((item) => item.id === flow.template_id);
              return (
                <Card key={flow.id} className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
                  <div className="flex min-w-0 flex-1 items-center gap-3">
                    <StatusPill
                      tone={flowTone[flow.status] ?? 'neutral'}
                      pulse={flow.status === 'active'}
                      label={flowStatusLabel[flow.status] ?? flow.status}
                    />
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium text-ink">{template?.name || flow.template_id}</div>
                      <div className="font-mono text-[11px] text-ink-mute">
                        {flow.project_id.slice(0, 8)} · 更新 {relTime(flow.updated_at)}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center justify-between gap-4 sm:justify-end">
                    <MiniFlow stages={flow.stages} className="hidden md:flex" />
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => navigate(`/workbench/${flow.project_id}/${active?.type ?? 'idea'}`)}
                    >
                      打开 <ArrowRight size={14} />
                    </Button>
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </section>

      <Dialog open={launchOpen} onOpenChange={closeLaunch}>
        <DialogContent>
          <DialogTitle>发起工作流</DialogTitle>
          <DialogDescription>选择项目与流程模板，创建后将进入第一个阶段。</DialogDescription>
          <div className="mt-4 space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="launch-project">项目</Label>
              <Select value={projectId} onValueChange={setProjectId}>
                <SelectTrigger id="launch-project">
                  <SelectValue placeholder={projects.length ? '选择项目' : '暂无项目，请先创建项目'} />
                </SelectTrigger>
                <SelectContent>
                  {projects.map((project) => <SelectItem key={project.id} value={project.id}>{project.title}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="launch-template">流程模板</Label>
              <Select value={templateId} onValueChange={setTemplateId}>
                <SelectTrigger id="launch-template">
                  <SelectValue placeholder={templates.length ? '选择模板' : '暂无可用模板'} />
                </SelectTrigger>
                <SelectContent>
                  {templates.map((template) => (
                    <SelectItem key={template.id} value={template.id}>{template.name || template.id}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          {createM.isError && (
            <p role="alert" className="mt-4 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-[13px] text-danger">
              创建失败：{errorText(createM.error)}
            </p>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => closeLaunch(false)} disabled={createM.isPending}>取消</Button>
            <Button
              variant="primary"
              loading={createM.isPending}
              disabled={!projectId || !templateId}
              onClick={() => createM.mutate({ project_id: projectId, template_id: templateId })}
            >
              创建并进入
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <FlowTemplateEditorDialog
        open={!!editor}
        initial={editor?.initial}
        editing={editor?.editing}
        agents={agentsQ.data ?? []}
        saving={saveTemplateM.isPending}
        error={saveTemplateM.isError ? errorText(saveTemplateM.error) : undefined}
        onOpenChange={(open) => !open && setEditor(null)}
        onSave={(input) => saveTemplateM.mutate(input)}
      />

      <Dialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent className="w-[min(92vw,440px)]">
          <DialogTitle>删除流程模板</DialogTitle>
          <DialogDescription>
            将删除“{deleting?.name || deleting?.id}”。已创建的工作流实例不会受影响。
          </DialogDescription>
          {deleteTemplateM.isError && (
            <p role="alert" className="mt-4 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-[13px] text-danger">
              {errorText(deleteTemplateM.error)}
            </p>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setDeleting(null)} disabled={deleteTemplateM.isPending}>取消</Button>
            <Button variant="danger" loading={deleteTemplateM.isPending} onClick={() => deleting && deleteTemplateM.mutate(deleting)}>
              <Trash2 size={15} /> 确认删除
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}
