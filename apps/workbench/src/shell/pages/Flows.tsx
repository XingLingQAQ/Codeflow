import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import { Workflow, Plus, Layers, ArrowRight } from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import {
  Card,
  Button,
  StatusPill,
  Badge,
  EmptyState,
  Skeleton,
  Label,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '../../ui';
import { MiniFlow } from '../../stages/MiniFlow';
import { useFlows, useFlowTemplates, useProjects, qk } from '../../lib/queries';
import { createFlow, type Flow, type FlowTemplateInfo } from '../../services-bridge/flows';
import { relTime } from '../../lib/format';
import { staggerItem } from '../../lib/motion';

const flowTone: Record<string, 'success' | 'accent' | 'danger'> = {
  active: 'success',
  completed: 'accent',
  aborted: 'danger',
};

export default function Flows() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const templatesQ = useFlowTemplates();
  const flowsQ = useFlows();
  const projectsQ = useProjects();

  const [open, setOpen] = useState(false);
  const [projectId, setProjectId] = useState('');
  const [templateId, setTemplateId] = useState('new_project');

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

  const createM = useMutation({
    mutationFn: (input: { project_id: string; template_id?: string }) => createFlow(input),
    onSuccess: (flow: Flow) => {
      qc.invalidateQueries({ queryKey: qk.flows() });
      toast.success('工作流已创建');
      closeDialog(false);
      const active = flow.stages.find((s) => s.status === 'active') ?? flow.stages[0];
      navigate(`/workbench/${flow.project_id}/${active?.type ?? 'idea'}`);
    },
    onError: (e) => toast.error('创建失败：' + (e instanceof Error ? e.message : String(e))),
  });

  const templates: FlowTemplateInfo[] = templatesQ.data?.items?.length
    ? templatesQ.data.items
    : (templatesQ.data?.ids ?? []).map((id) => ({ id, name: id, description: undefined }));
  const flows = flowsQ.data?.items ?? [];
  const projects = projectsQ.data?.projects ?? [];

  return (
    <PageShell
      title="工作流"
      subtitle="从模板发起流程，或监控进行中的工作流"
      actions={
        <Button variant="primary" onClick={() => setOpen(true)}>
          <Plus size={16} /> 发起工作流
        </Button>
      }
    >
      <motion.div variants={staggerItem}>
        <SectionTitle>流程模板</SectionTitle>
        {templatesQ.isLoading ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Card key={i} className="p-5">
                <Skeleton className="h-4 w-1/3" />
                <Skeleton className="mt-3 h-3 w-full" />
              </Card>
            ))}
          </div>
        ) : templates.length === 0 ? (
          <Card className="py-6">
            <EmptyState
              icon={<Layers size={22} />}
              title={templatesQ.isError ? '流程模板不可用' : '暂无模板'}
              description="流程引擎为实验特性，需后端启用。启用后这里会展示新建/导入等模板。"
            />
          </Card>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {templates.map((t) => (
              <Card key={t.id} className="group flex flex-col p-5">
                <div className="flex items-center gap-2.5">
                  <span className="grid size-9 place-items-center rounded-lg bg-tint-active text-ink-dim">
                    <Workflow size={17} />
                  </span>
                  <h3 className="font-display font-semibold text-ink">{t.name ?? t.id}</h3>
                </div>
                <p className="mt-2.5 flex-1 text-[13px] leading-relaxed text-ink-dim">
                  {t.description ?? '标准 CodeFlow 流程模板。'}
                </p>
                <div className="mt-4 flex items-center justify-between">
                  {t.stages?.length ? <Badge>{t.stages.length} 阶段</Badge> : <span />}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setTemplateId(String(t.id));
                      setOpen(true);
                    }}
                  >
                    使用 <ArrowRight size={14} />
                  </Button>
                </div>
              </Card>
            ))}
          </div>
        )}
      </motion.div>

      <motion.div variants={staggerItem} className="mt-9">
        <SectionTitle>进行中的工作流</SectionTitle>
        {flowsQ.isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-xl" />
            ))}
          </div>
        ) : flows.length === 0 ? (
          <Card className="flex items-center gap-3 px-4 py-3.5 text-[13px] text-ink-dim">
            <Workflow size={16} className="text-ink-mute" />
            还没有工作流。
            <button onClick={() => setOpen(true)} className="text-ink hover:underline">
              发起第一个
            </button>
          </Card>
        ) : (
          <div className="space-y-2">
            {flows.map((f) => {
              const active = f.stages.find((s) => s.status === 'active' || s.status === 'waiting_gate');
              return (
                <Card
                  key={f.id}
                  interactive
                  className="flex items-center justify-between gap-4 px-4 py-3"
                  onClick={() => navigate(`/workbench/${f.project_id}/${active?.type ?? 'idea'}`)}
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <StatusPill tone={flowTone[f.status] ?? 'neutral'} pulse={f.status === 'active'} label={f.status} />
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium text-ink">{f.template_id}</div>
                      <div className="font-mono text-[11px] text-ink-mute">{f.project_id.slice(0, 8)} · 更新 {relTime(f.updated_at)}</div>
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <MiniFlow stages={f.stages} className="hidden sm:flex" />
                    <ArrowRight size={15} className="text-ink-mute" />
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </motion.div>

      <Dialog open={open} onOpenChange={closeDialog}>
        <DialogContent>
          <DialogTitle>发起工作流</DialogTitle>
          <DialogDescription>为一个项目按模板创建流程实例。</DialogDescription>
          <div className="mt-4 space-y-3.5">
            <div className="space-y-1.5">
              <Label>项目</Label>
              <Select value={projectId} onValueChange={setProjectId}>
                <SelectTrigger>
                  <SelectValue placeholder={projects.length ? '选择项目' : '暂无项目（请先创建）'} />
                </SelectTrigger>
                <SelectContent>
                  {projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>模板</Label>
              <Select value={templateId} onValueChange={setTemplateId}>
                <SelectTrigger>
                  <SelectValue placeholder="选择模板" />
                </SelectTrigger>
                <SelectContent>
                  {(templates.length ? templates : [{ id: 'new_project', name: 'new_project' }, { id: 'import_project', name: 'import_project' }]).map((t) => (
                    <SelectItem key={t.id} value={String(t.id)}>
                      {t.name ?? t.id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => closeDialog(false)}>
              取消
            </Button>
            <Button
              variant="primary"
              loading={createM.isPending}
              disabled={!projectId}
              onClick={() => createM.mutate({ project_id: projectId, template_id: templateId })}
            >
              创建工作流
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </PageShell>
  );
}
