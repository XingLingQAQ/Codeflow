import { HookManager } from '../hooks/HookManager.js';
import type { HookRuntimeControls } from '../hooks/types.js';
import { AgentRuntime } from './runtime.js';
/**
 * Cowork Factory
 * 工厂函数用于创建和注册执行器到 Orchestrator
 */

import { CoworkOrchestrator } from './CoworkOrchestrator.js';
import { AiderAdapter, AiderConfig } from './adapters/AiderAdapter.js';
import { CodexCLIAdapter } from './adapters/CodexCLIAdapter.js';
import { AiderCodeEditor, AiderEditorConfig } from './editors/AiderCodeEditor.js';
import { ClaudeCodeEditor, ClaudeEditorConfig } from './editors/ClaudeCodeEditor.js';
import { GeminiCodeEditor, GeminiEditorConfig } from './editors/GeminiCodeEditor.js';
import { CodexCodeEditor, CodexEditorAdapter, CodexEditorConfig } from './editors/CodexCodeEditor.js';
import { ClaudeAdapter } from '../adapters/ClaudeAdapter.js';
import { GeminiAdapter } from '../adapters/GeminiAdapter.js';
import { CodexAdapter } from '../adapters/CodexAdapter.js';
import { ExecutorCapabilities } from './types.js';

interface HookAwareAdapter {
  setHookManager(hookManager?: HookManager): void;
  getHookManager?(): HookManager | undefined;
}

function attachHookManager<TAdapter extends HookAwareAdapter>(
  adapter: TAdapter,
  hookManager?: HookManager,
): TAdapter {
  if (hookManager) {
    adapter.setHookManager(hookManager);
  }
  return adapter;
}

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
  config: AiderExecutorConfig = {},
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
  aiderConfig?: AiderExecutorConfig,
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
  } = {},
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

/**
 * 默认 Claude 能力
 */
const DEFAULT_CLAUDE_CAPABILITIES: ExecutorCapabilities = {
  name: 'claude',
  supportedTypes: ['code-edit', 'refactor', 'review', 'explain'],
  maxConcurrency: 3,
  estimatedSpeed: 'fast',
  features: {
    streaming: true,
    multiFile: true,
    contextAware: true,
    codeReview: true,
  },
};

/**
 * 默认 Gemini 能力
 */
const DEFAULT_GEMINI_CAPABILITIES: ExecutorCapabilities = {
  name: 'gemini',
  supportedTypes: ['code-edit', 'refactor', 'review', 'explain'],
  maxConcurrency: 3,
  estimatedSpeed: 'fast',
  features: {
    streaming: true,
    multiFile: true,
    contextAware: true,
    codeReview: true,
  },
};

/**
 * 默认 Codex 能力
 */
const DEFAULT_CODEX_CAPABILITIES: ExecutorCapabilities = {
  name: 'codex',
  supportedTypes: ['code-edit', 'refactor', 'debug'],
  maxConcurrency: 3,
  estimatedSpeed: 'fast',
  features: {
    streaming: true,
    multiFile: true,
    contextAware: true,
    codeReview: false,
  },
};

/**
 * 创建并注册 Claude 执行器
 */
export function registerClaudeExecutor(
  orchestrator: CoworkOrchestrator,
  adapter: ClaudeAdapter,
  config?: ClaudeEditorConfig,
  capabilities?: Partial<ExecutorCapabilities>,
  hookManager?: HookManager,
): ClaudeCodeEditor {
  const editor = new ClaudeCodeEditor(attachHookManager(adapter, hookManager), config);

  const caps: ExecutorCapabilities = {
    ...DEFAULT_CLAUDE_CAPABILITIES,
    ...capabilities,
  };

  orchestrator.registerExecutor('claude', editor, caps);

  return editor;
}

/**
 * 创建并注册 Gemini 执行器
 */
export function registerGeminiExecutor(
  orchestrator: CoworkOrchestrator,
  adapter: GeminiAdapter,
  config?: GeminiEditorConfig,
  capabilities?: Partial<ExecutorCapabilities>,
  hookManager?: HookManager,
): GeminiCodeEditor {
  const editor = new GeminiCodeEditor(attachHookManager(adapter, hookManager), config);

  const caps: ExecutorCapabilities = {
    ...DEFAULT_GEMINI_CAPABILITIES,
    ...capabilities,
  };

  orchestrator.registerExecutor('gemini', editor, caps);

  return editor;
}

/**
 * 创建并注册 Codex 执行器
 */
export function registerCodexExecutor(
  orchestrator: CoworkOrchestrator,
  adapter: CodexEditorAdapter,
  config?: CodexEditorConfig,
  capabilities?: Partial<ExecutorCapabilities>,
  hookManager?: HookManager,
): CodexCodeEditor {
  const editor = new CodexCodeEditor(attachHookManager(adapter as HookAwareAdapter, hookManager) as CodexEditorAdapter, config);

  const caps: ExecutorCapabilities = {
    ...DEFAULT_CODEX_CAPABILITIES,
    ...capabilities,
  };

  orchestrator.registerExecutor('codex', editor, caps);

  return editor;
}

/**
 * 创建包含所有编辑器的 Orchestrator
 */
export interface AllEditorsConfig {
  aiderConfig?: AiderExecutorConfig;
  claudeAdapter?: ClaudeAdapter;
  claudeConfig?: ClaudeEditorConfig;
  geminiAdapter?: GeminiAdapter;
  geminiConfig?: GeminiEditorConfig;
  codexAdapter?: CodexAdapter;
  codexCliAdapter?: CodexCLIAdapter;
  codexConfig?: CodexEditorConfig;
  hookManager?: HookManager;
  hookControls?: HookRuntimeControls;
}

export function createOrchestratorWithAllEditors(config: AllEditorsConfig = {}): {
  orchestrator: CoworkOrchestrator;
  aiderEditor: AiderCodeEditor;
  claudeEditor?: ClaudeCodeEditor;
  geminiEditor?: GeminiCodeEditor;
  codexEditor?: CodexCodeEditor;
} {
  const sharedHookManager = config.hookManager ?? new HookManager(undefined, config.hookControls);
  if (config.hookControls) {
    sharedHookManager.setControls(config.hookControls);
  }
  const runtime = new AgentRuntime({
    hookManager: sharedHookManager,
    hookControls: config.hookControls,
  });
  const orchestrator = new CoworkOrchestrator(undefined, undefined, runtime);
  const aiderEditor = registerAiderExecutor(orchestrator, config.aiderConfig);

  let claudeEditor: ClaudeCodeEditor | undefined;
  let geminiEditor: GeminiCodeEditor | undefined;
  let codexEditor: CodexCodeEditor | undefined;

  if (config.claudeAdapter) {
    claudeEditor = registerClaudeExecutor(
      orchestrator,
      config.claudeAdapter,
      config.claudeConfig,
      undefined,
      sharedHookManager,
    );
  }

  if (config.geminiAdapter) {
    geminiEditor = registerGeminiExecutor(
      orchestrator,
      config.geminiAdapter,
      config.geminiConfig,
      undefined,
      sharedHookManager,
    );
  }

  const codexExecutorAdapter = config.codexCliAdapter ?? config.codexAdapter;
  if (codexExecutorAdapter) {
    codexEditor = registerCodexExecutor(
      orchestrator,
      codexExecutorAdapter,
      config.codexConfig,
      undefined,
      sharedHookManager,
    );
  }

  return {
    orchestrator,
    aiderEditor,
    claudeEditor,
    geminiEditor,
    codexEditor,
  };
}
