import { describe, expect, it } from 'vitest';
import { validateAgentForm, type AgentFormState } from './AgentEditorDialog';

function validForm(): AgentFormState {
  return {
    name: '评审专家',
    description: '检查提交前的变更',
    role_base: 'critic',
    system_prompt: '检查正确性、安全性与回归风险，并给出可执行结论。',
    model: '',
    mcpTools: '',
    skills: 'code-review',
    stageTags: ['review'],
  };
}

describe('validateAgentForm', () => {
  it('accepts a complete custom agent definition', () => {
    expect(validateAgentForm(validForm())).toEqual({});
  });

  it('requires a name and substantive system instructions', () => {
    const form = validForm();
    form.name = ' ';
    form.system_prompt = '太短';
    expect(validateAgentForm(form)).toMatchObject({
      name: '请输入 Agent 名称',
      system_prompt: '系统指令至少需要 12 个字符',
    });
  });
});
