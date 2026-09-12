import { useEffect, useState } from 'react';
import { Bot, Cpu, Wrench } from 'lucide-react';
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
} from '../../ui';
import type { AgentInfo, AgentInput } from '../../services-bridge/agents';

const ROLES = [
  { value: 'main', label: '主控', description: '拆解目标并协调其他 Agent' },
  { value: 'coder', label: '编码', description: '实现和修改代码' },
  { value: 'sub', label: '子代理', description: '承接可隔离的专门任务' },
  { value: 'critic', label: '评审', description: '检查正确性、安全与质量' },
  { value: 'researcher', label: '调研', description: '检索、核验和综合证据' },
] as const;

const STAGES = [
  ['idea', '意图'],
  ['design', '设计'],
  ['planning', '规划'],
  ['research', '调研'],
  ['coding', '编码'],
  ['review', '评审'],
  ['submit', '提交'],
] as const;

interface AgentEditorDialogProps {
  open: boolean;
  agent?: AgentInfo | null;
  saving: boolean;
  error?: string;
  onOpenChange: (open: boolean) => void;
  onSave: (input: AgentInput) => void;
}

export interface AgentFormState {
  name: string;
  description: string;
  role_base: string;
  system_prompt: string;
  model: string;
  mcpTools: string;
  skills: string;
  stageTags: string[];
}

function fromAgent(agent?: AgentInfo | null): AgentFormState {
  return {
    name: agent?.name ?? '',
    description: agent?.description ?? '',
    role_base: agent?.role_base ?? 'sub',
    system_prompt: agent?.system_prompt ?? '',
    model: agent?.binding?.model ?? '',
    mcpTools: agent?.mounts?.mcp_tools?.join(', ') ?? '',
    skills: agent?.mounts?.skills?.join(', ') ?? '',
    stageTags: agent?.stage_tags ?? [],
  };
}

function splitList(value: string): string[] {
  return Array.from(new Set(value.split(',').map((item) => item.trim()).filter(Boolean)));
}

export function validateAgentForm(form: AgentFormState): Record<string, string> {
  const errors: Record<string, string> = {};
  const name = form.name.trim();
  if (!name) errors.name = '请输入 Agent 名称';
  else if (name.length > 80) errors.name = '名称不能超过 80 个字符';
  if (!form.system_prompt.trim()) errors.system_prompt = '请填写角色或系统指令';
  else if (form.system_prompt.trim().length < 12) errors.system_prompt = '系统指令至少需要 12 个字符';
  if (form.description.length > 500) errors.description = '说明不能超过 500 个字符';
  return errors;
}

export function AgentEditorDialog({
  open,
  agent,
  saving,
  error,
  onOpenChange,
  onSave,
}: AgentEditorDialogProps) {
  const [form, setForm] = useState<AgentFormState>(() => fromAgent(agent));
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!open) return;
    setForm(fromAgent(agent));
    setErrors({});
  }, [agent, open]);

  const submit = () => {
    const nextErrors = validateAgentForm(form);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length) return;
    onSave({
      name: form.name.trim(),
      description: form.description.trim(),
      role_base: form.role_base,
      system_prompt: form.system_prompt.trim(),
      binding: { model: form.model.trim() },
      mounts: { mcp_tools: splitList(form.mcpTools), skills: splitList(form.skills) },
      stage_tags: form.stageTags,
    });
  };

  const toggleStage = (stage: string) => {
    setForm((current) => ({
      ...current,
      stageTags: current.stageTags.includes(stage)
        ? current.stageTags.filter((item) => item !== stage)
        : [...current.stageTags, stage],
    }));
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[88vh] w-[min(94vw,680px)] overflow-y-auto">
        <DialogTitle>{agent ? '编辑 Agent' : '新建 Agent'}</DialogTitle>
        <DialogDescription>
          定义角色边界、系统指令与运行能力。保存后可在流程阶段中直接选择。
        </DialogDescription>

        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="agent-name">名称</Label>
            <Input
              id="agent-name"
              autoFocus
              value={form.name}
              aria-invalid={!!errors.name}
              onChange={(event) => setForm({ ...form, name: event.target.value })}
              placeholder="例如：前端实现专家"
            />
            {errors.name && <p className="text-xs text-danger">{errors.name}</p>}
          </div>

          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="agent-description">说明</Label>
            <Textarea
              id="agent-description"
              rows={2}
              value={form.description}
              aria-invalid={!!errors.description}
              onChange={(event) => setForm({ ...form, description: event.target.value })}
              placeholder="说明这个 Agent 适合处理什么任务"
            />
            {errors.description && <p className="text-xs text-danger">{errors.description}</p>}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="agent-role">基础角色</Label>
            <Select value={form.role_base} onValueChange={(role_base) => setForm({ ...form, role_base })}>
              <SelectTrigger id="agent-role">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((role) => (
                  <SelectItem key={role.value} value={role.value}>
                    {role.label} · {role.description}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="agent-model">模型</Label>
            <div className="relative">
              <Cpu size={14} className="pointer-events-none absolute left-3 top-2.5 text-ink-mute" />
              <Input
                id="agent-model"
                value={form.model}
                onChange={(event) => setForm({ ...form, model: event.target.value })}
                placeholder="留空则继承全局默认模型"
                className="pl-9"
              />
            </div>
          </div>

          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="agent-prompt">角色 / 系统指令</Label>
            <Textarea
              id="agent-prompt"
              rows={6}
              value={form.system_prompt}
              aria-invalid={!!errors.system_prompt}
              onChange={(event) => setForm({ ...form, system_prompt: event.target.value })}
              placeholder="描述职责、工作方式、输出要求和必须遵守的边界"
            />
            {errors.system_prompt && <p className="text-xs text-danger">{errors.system_prompt}</p>}
          </div>

          <fieldset className="sm:col-span-2">
            <legend className="text-xs font-medium text-ink-dim">适用阶段</legend>
            <div className="mt-2 flex flex-wrap gap-2">
              {STAGES.map(([value, label]) => (
                <label
                  key={value}
                  className="flex cursor-pointer items-center gap-2 rounded-lg border border-line bg-raised px-2.5 py-1.5 text-xs text-ink-dim hover:border-line-strong"
                >
                  <input
                    type="checkbox"
                    checked={form.stageTags.includes(value)}
                    onChange={() => toggleStage(value)}
                    className="accent-[var(--color-primary)]"
                  />
                  {label}
                </label>
              ))}
            </div>
          </fieldset>

          <div className="space-y-1.5">
            <Label htmlFor="agent-mcp">MCP 工具</Label>
            <div className="relative">
              <Wrench size={14} className="pointer-events-none absolute left-3 top-2.5 text-ink-mute" />
              <Input
                id="agent-mcp"
                value={form.mcpTools}
                onChange={(event) => setForm({ ...form, mcpTools: event.target.value })}
                placeholder="filesystem, browser"
                className="pl-9"
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="agent-skills">Skills</Label>
            <div className="relative">
              <Bot size={14} className="pointer-events-none absolute left-3 top-2.5 text-ink-mute" />
              <Input
                id="agent-skills"
                value={form.skills}
                onChange={(event) => setForm({ ...form, skills: event.target.value })}
                placeholder="code-review, research"
                className="pl-9"
              />
            </div>
          </div>
        </div>

        {error && (
          <p role="alert" className="mt-4 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-[13px] text-danger">
            {error}
          </p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
            取消
          </Button>
          <Button variant="primary" onClick={submit} loading={saving}>
            {agent ? '保存修改' : '创建 Agent'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
