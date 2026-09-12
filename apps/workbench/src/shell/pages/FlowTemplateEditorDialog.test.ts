import { describe, expect, it } from 'vitest';
import { validateFlowTemplate } from './FlowTemplateEditorDialog';
import type { FlowTemplateInput } from '../../services-bridge/flows';

function template(): FlowTemplateInput {
  return {
    id: 'review_delivery',
    name: '评审交付流程',
    stages: [
      { type: 'coding', name: '编码', canvas: 'coding', optional: false, gates: [] },
      {
        type: 'review',
        name: '评审',
        canvas: 'review',
        optional: false,
        agent_id: 'builtin-red-critic',
        gates: [{ phase: 'exit', kind: 'human_approval', on_fail: 'block' }],
      },
    ],
  };
}

describe('validateFlowTemplate', () => {
  it('accepts ordered stages with agent and approval bindings', () => {
    expect(validateFlowTemplate(template())).toEqual({});
  });

  it('rejects duplicate stage types and an optional first stage', () => {
    const input = template();
    input.stages[0].optional = true;
    input.stages[1].type = 'coding';
    expect(validateFlowTemplate(input).stages).toBeTruthy();
  });

  it('rejects unstable template identifiers', () => {
    const input = template();
    input.id = '中文 模板';
    expect(validateFlowTemplate(input).id).toContain('小写字母');
  });
});
