/**
 * Cowork Factory
 * 工厂函数用于创建和注册执行器到 Orchestrator
 */

import { CoworkOrchestrator } from './CoworkOrchestrator.js';
import { AiderAdapter, AiderConfig } from './adapters/AiderAdapter.js';
import { AiderCodeEditor, AiderEditorConfig } from './editors/AiderCodeEditor.js';
import { ExecutorCapabilities } from './types.js';

/**
 * Aider 执行器配置
 */
export interface AiderExecutorConfig {
  adapter?: AiderConfig;
  editor?: AiderEditorConfig;
  capabilities?: Partial<ExecutorCapabilities>;
}

/**
 * 默认 Aider 能力
 */
const DEFAULT_AIDER_CAPABILITIES: ExecutorCapabilities = {
  name: 'aider',
  supportedTypes: ['code-edit', 'refactor', 'debug'],
  maxConcurrency: 1,
  estimatedSpeed: 'medium',
  features: {
    streaming: true,
    multiFile: true,
    contextAware: true,
    codeReview: false,
  },
};

/**
 * 创建并注册 Aider 执行器
 */
export function registerAiderExecutor(
  orchestrator: CoworkOrchestrator,
  config: AiderExecutorConfig = {}
): AiderCodeEditor {
  const adapter = new AiderAdapter(config.adapter);
  const editor = new AiderCodeEditor(adapter, config.editor);

  const capabilities: ExecutorCapabilities = {
    ...DEFAULT_AIDER_CAPABILITIES,
    ...config.capabilities,
  };

  orchestrator.registerExecutor('aider', editor, capabilities);

  return editor;
}

/**
 * 创建预配置的 Orchestrator（包含 Aider）
 */
export function createOrchestratorWithAider(
  aiderConfig?: AiderExecutorConfig
): {
  orchestrator: CoworkOrchestrator;
  aiderEditor: AiderCodeEditor;
} {
  const orchestrator = new CoworkOrchestrator();
  const aiderEditor = registerAiderExecutor(orchestrator, aiderConfig);

  return { orchestrator, aiderEditor };
}

/**
 * 创建任务辅助函数
 */
export function createTask(
  id: string,
  executor: string,
  files: string[],
  instruction: string,
  options: {
    type?: 'code-edit' | 'test-gen' | 'refactor' | 'review' | 'debug' | 'explain';
    context?: string;
    timeout?: number;
    priority?: number;
  } = {}
) {
  return {
    id,
    type: options.type || 'code-edit',
    executor,
    input: {
      files,
      instruction,
      context: options.context,
    },
    config: {
      timeout: options.timeout,
      priority: options.priority,
    },
    status: 'pending' as const,
    createdAt: Date.now(),
  };
}
