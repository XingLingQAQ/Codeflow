import { useEffect, useState } from 'react';
import { ArrowDown, ArrowUp, Bot, Plus, ShieldCheck, Trash2 } from 'lucide-react';
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  IconButton,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Switch,
  Textarea,
  Tooltip,
} from '../../ui';
import type { AgentInfo } from '../../services-bridge/agents';
import type {
  FlowTemplateInfo,
  FlowTemplateInput,
  FlowTemplateStage,
  StageType,
} from '../../services-bridge/flows';

const STAGE_OPTIONS: { type: StageType; name: string; canvas: string }[] = [
  { type: 'idea', name: '意图', canvas: 'intent' },
  { type: 'import', name: '导入', canvas: 'import_pipeline' },
  { type: 'comprehension', name: '理解', canvas: 'comprehension' },
  { type: 'design', name: '设计', canvas: 'design_doc' },
  { type: 'planning', name: '规划', canvas: 'planning_board' },
  { type: 'research', name: '调研', canvas: 'deep_search' },
  { type: 'coding', name: '编码', canvas: 'coding' },
  { type: 'review', name: '评审', canvas: 'review' },
  { type: 'submit', name: '提交', canvas: 'submit' },
];

const DEFAULT_STAGES: FlowTemplateStage[] = ['planning', 'coding', 'review'].map((type) => {
  const option = STAGE_OPTIONS.find((item) => item.type === type)!;
  return { ...option, optional: false, gates: [] };
});

interface FlowTemplateEditorDialogProps {
  open: boolean;
  initial?: FlowTemplateInfo | null;
  editing?: boolean;
  agents: AgentInfo[];
  saving: boolean;
  error?: string;
  onOpenChange: (open: boolean) => void;
  onSave: (input: FlowTemplateInput) => void;
}

function cloneStages(stages?: FlowTemplateInfo['stages']): FlowTemplateStage[] {
  if (!stages?.length) return structuredClone(DEFAULT_STAGES);
  return stages.map((stage) => ({
    type: stage.type,
    name: stage.name || STAGE_OPTIONS.find((item) => item.type === stage.type)?.name || stage.type,
    canvas: stage.canvas || STAGE_OPTIONS.find((item) => item.type === stage.type)?.canvas || stage.type,
    agent_id: stage.agent_id,
    optional: !!stage.optional,
    gates: structuredClone(stage.gates ?? []),
  }));
}

function initialForm(initial?: FlowTemplateInfo | null, editing?: boolean): FlowTemplateInput {
  return {
    id: editing ? initial?.id ?? '' : '',
    name: initial ? `${initial.name || initial.id}${editing ? '' : ' 副本'}` : '',
    description: initial?.description ?? '',
    stages: cloneStages(initial?.stages),
    loops: [],
  };
}

export function validateFlowTemplate(input: FlowTemplateInput): Record<string, string> {
  const errors: Record<string, string> = {};
  if (!input.id.trim()) errors.id = '请输入模板标识';
  else if (!/^[a-z][a-z0-9_-]{2,63}$/.test(input.id.trim())) {
    errors.id = '使用 3-64 位小写字母、数字、短横线或下划线，并以字母开头';
  }
  if (!input.name.trim()) errors.name = '请输入模板名称';
  if (!input.stages.length) errors.stages = '至少需要一个阶段';
  if (input.stages[0]?.optional) errors.stages = '第一个阶段不能设为可选';
  const types = input.stages.map((stage) => stage.type);
  if (new Set(types).size !== types.length) errors.stages = '同一种阶段在一个模板中只能出现一次';
  return errors;
}

export function FlowTemplateEditorDialog({
  open,
  initial,
  editing,
  agents,
  saving,
  error,
  onOpenChange,
  onSave,
}: FlowTemplateEditorDialogProps) {
  const [form, setForm] = useState<FlowTemplateInput>(() => initialForm(initial, editing));
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!open) return;
    setForm(initialForm(initial, editing));
    setErrors({});
  }, [editing, initial, open]);

  const updateStage = (index: number, update: Partial<FlowTemplateStage>) => {
    setForm((current) => ({
      ...current,
      stages: current.stages.map((stage, stageIndex) => stageIndex === index ? { ...stage, ...update } : stage),
    }));
  };

  const chooseStageType = (index: number, type: StageType) => {
    const option = STAGE_OPTIONS.find((item) => item.type === type)!;
    updateStage(index, { type, name: option.name, canvas: option.canvas });
  };

  const moveStage = (index: number, offset: -1 | 1) => {
    setForm((current) => {
      const stages = [...current.stages];
      const target = index + offset;
      if (target < 0 || target >= stages.length) return current;
      [stages[index], stages[target]] = [stages[target], stages[index]];
      if (stages[0]) stages[0] = { ...stages[0], optional: false };
      return { ...current, stages };
    });
  };

  const setHumanGate = (index: number, checked: boolean) => {
    const stage = form.stages[index];
    const others = (stage.gates ?? []).filter((gate) => gate.kind !== 'human_approval');
    updateStage(index, {
      gates: checked
        ? [...others, { phase: 'exit', kind: 'human_approval', on_fail: 'block' }]
        : others,
    });
  };

  const addStage = () => {
    const used = new Set(form.stages.map((stage) => stage.type));
    const option = STAGE_OPTIONS.find((item) => !used.has(item.type));
    if (!option) return;
    setForm((current) => ({
      ...current,
      stages: [...current.stages, { ...option, optional: false, gates: [] }],
    }));
  };

  const submit = () => {
    const normalized = { ...form, id: form.id.trim(), name: form.name.trim(), description: form.description?.trim() };
    const nextErrors = validateFlowTemplate(normalized);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length) return;
    onSave(normalized);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] w-[min(96vw,860px)] overflow-y-auto">
        <DialogTitle>{editing ? '编辑流程模板' : initial ? '复制流程模板' : '新建流程模板'}</DialogTitle>
        <DialogDescription>
          按执行顺序配置阶段、负责 Agent、可选步骤与人工确认点。
        </DialogDescription>

        <div className="mt-5 grid gap-4 sm:grid-cols-[minmax(0,0.7fr)_minmax(0,1.3fr)]">
          <div className="space-y-1.5">
            <Label htmlFor="flow-template-id">模板标识</Label>
            <Input
              id="flow-template-id"
              value={form.id}
              disabled={editing}
              aria-invalid={!!errors.id}
              onChange={(event) => setForm({ ...form, id: event.target.value.toLowerCase() })}
              placeholder="frontend_delivery"
            />
            {errors.id && <p className="text-xs text-danger">{errors.id}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="flow-template-name">名称</Label>
            <Input
              id="flow-template-name"
              value={form.name}
              aria-invalid={!!errors.name}
              onChange={(event) => setForm({ ...form, name: event.target.value })}
              placeholder="前端交付流程"
            />
            {errors.name && <p className="text-xs text-danger">{errors.name}</p>}
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="flow-template-description">说明</Label>
            <Textarea
              id="flow-template-description"
              rows={2}
              value={form.description}
              onChange={(event) => setForm({ ...form, description: event.target.value })}
              placeholder="说明这套流程适合处理的项目与交付目标"
            />
          </div>
        </div>

        <div className="mt-6 flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-ink">阶段编排</h3>
            <p className="mt-0.5 text-xs text-ink-mute">从上到下执行；人工确认将在阶段出口阻止自动推进。</p>
          </div>
          <Button size="sm" onClick={addStage} disabled={form.stages.length >= STAGE_OPTIONS.length}>
            <Plus size={14} /> 添加阶段
          </Button>
        </div>

        <div className="mt-3 overflow-hidden rounded-lg border border-line bg-surface">
          {form.stages.map((stage, index) => {
            const humanGate = stage.gates?.some((gate) => gate.kind === 'human_approval') ?? false;
            return (
              <div
                key={`${stage.type}-${index}`}
                className="grid gap-3 border-b border-line p-3 last:border-b-0 lg:grid-cols-[32px_minmax(140px,0.8fr)_minmax(170px,1fr)_auto] lg:items-center"
              >
                <span className="grid size-7 place-items-center rounded-md bg-tint text-xs font-semibold tabular-nums text-ink-dim">
                  {index + 1}
                </span>

                <div className="min-w-0 space-y-1">
                  <Label htmlFor={`stage-type-${index}`}>阶段</Label>
                  <Select value={stage.type} onValueChange={(value) => chooseStageType(index, value as StageType)}>
                    <SelectTrigger id={`stage-type-${index}`}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {STAGE_OPTIONS.map((option) => (
                        <SelectItem
                          key={option.type}
                          value={option.type}
                          disabled={form.stages.some((item, itemIndex) => itemIndex !== index && item.type === option.type)}
                        >
                          {option.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="min-w-0 space-y-1">
                  <Label htmlFor={`stage-agent-${index}`}>负责 Agent</Label>
                  <Select
                    value={stage.agent_id || '__none__'}
                    onValueChange={(value) => updateStage(index, { agent_id: value === '__none__' ? undefined : value })}
                  >
                    <SelectTrigger id={`stage-agent-${index}`}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__none__">按阶段自动推荐</SelectItem>
                      {agents.filter((agent) => agent.enabled !== false).map((agent) => (
                        <SelectItem key={agent.id} value={agent.id}>{agent.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="flex flex-wrap items-center justify-between gap-2 lg:justify-end">
                  <label className="flex items-center gap-2 text-xs text-ink-dim">
                    <Switch
                      checked={stage.optional}
                      disabled={index === 0}
                      onCheckedChange={(optional) => updateStage(index, { optional })}
                      aria-label={`将第 ${index + 1} 阶段设为可选`}
                    />
                    可选
                  </label>
                  <label className="flex items-center gap-2 text-xs text-ink-dim">
                    <Switch
                      checked={humanGate}
                      onCheckedChange={(checked) => setHumanGate(index, checked)}
                      aria-label={`为第 ${index + 1} 阶段设置人工确认`}
                    />
                    <ShieldCheck size={13} /> 人工确认
                  </label>
                  <div className="flex items-center">
                    <Tooltip content="上移阶段" side="top">
                      <IconButton size="sm" disabled={index === 0} onClick={() => moveStage(index, -1)} aria-label={`上移第 ${index + 1} 阶段`}>
                        <ArrowUp size={14} />
                      </IconButton>
                    </Tooltip>
                    <Tooltip content="下移阶段" side="top">
                      <IconButton size="sm" disabled={index === form.stages.length - 1} onClick={() => moveStage(index, 1)} aria-label={`下移第 ${index + 1} 阶段`}>
                        <ArrowDown size={14} />
                      </IconButton>
                    </Tooltip>
                    <Tooltip content="删除阶段" side="top">
                      <IconButton
                        size="sm"
                        disabled={form.stages.length === 1}
                        onClick={() => setForm({ ...form, stages: form.stages.filter((_, itemIndex) => itemIndex !== index) })}
                        aria-label={`删除第 ${index + 1} 阶段`}
                        className="hover:bg-danger/10 hover:text-danger"
                      >
                        <Trash2 size={14} />
                      </IconButton>
                    </Tooltip>
                  </div>
                </div>
              </div>
            );
          })}
        </div>

        {errors.stages && <p className="mt-2 text-xs text-danger">{errors.stages}</p>}
        <div className="mt-3 flex flex-wrap gap-2 text-xs text-ink-mute">
          <Badge><Bot size={11} /> {form.stages.filter((stage) => stage.agent_id).length} 个阶段已指定 Agent</Badge>
          <Badge><ShieldCheck size={11} /> {form.stages.filter((stage) => stage.gates?.some((gate) => gate.kind === 'human_approval')).length} 个人工确认点</Badge>
        </div>

        {error && (
          <p role="alert" className="mt-4 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-[13px] text-danger">
            {error}
          </p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>取消</Button>
          <Button variant="primary" onClick={submit} loading={saving}>
            {editing ? '保存修改' : '保存模板'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
