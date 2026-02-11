/**
 * Cowork Factory
 * 工厂函数用于创建和注册执行器到 Orchestrator
 */
import { CoworkOrchestrator } from './CoworkOrchestrator.js';
import { AiderAdapter } from './adapters/AiderAdapter.js';
import { AiderCodeEditor } from './editors/AiderCodeEditor.js';
import { ClaudeCodeEditor } from './editors/ClaudeCodeEditor.js';
import { GeminiCodeEditor } from './editors/GeminiCodeEditor.js';
import { CodexCodeEditor } from './editors/CodexCodeEditor.js';
/**
 * 默认 Aider 能力
 */
const DEFAULT_AIDER_CAPABILITIES = {
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
export function registerAiderExecutor(orchestrator, config = {}) {
    const adapter = new AiderAdapter(config.adapter);
    const editor = new AiderCodeEditor(adapter, config.editor);
    const capabilities = {
        ...DEFAULT_AIDER_CAPABILITIES,
        ...config.capabilities,
    };
    orchestrator.registerExecutor('aider', editor, capabilities);
    return editor;
}
/**
 * 创建预配置的 Orchestrator（包含 Aider）
 */
export function createOrchestratorWithAider(aiderConfig) {
    const orchestrator = new CoworkOrchestrator();
    const aiderEditor = registerAiderExecutor(orchestrator, aiderConfig);
    return { orchestrator, aiderEditor };
}
/**
 * 创建任务辅助函数
 */
export function createTask(id, executor, files, instruction, options = {}) {
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
        status: 'pending',
        createdAt: Date.now(),
    };
}
/**
 * 默认 Claude 能力
 */
const DEFAULT_CLAUDE_CAPABILITIES = {
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
const DEFAULT_GEMINI_CAPABILITIES = {
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
const DEFAULT_CODEX_CAPABILITIES = {
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
export function registerClaudeExecutor(orchestrator, adapter, config, capabilities) {
    const editor = new ClaudeCodeEditor(adapter, config);
    const caps = {
        ...DEFAULT_CLAUDE_CAPABILITIES,
        ...capabilities,
    };
    orchestrator.registerExecutor('claude', editor, caps);
    return editor;
}
/**
 * 创建并注册 Gemini 执行器
 */
export function registerGeminiExecutor(orchestrator, adapter, config, capabilities) {
    const editor = new GeminiCodeEditor(adapter, config);
    const caps = {
        ...DEFAULT_GEMINI_CAPABILITIES,
        ...capabilities,
    };
    orchestrator.registerExecutor('gemini', editor, caps);
    return editor;
}
/**
 * 创建并注册 Codex 执行器
 */
export function registerCodexExecutor(orchestrator, adapter, config, capabilities) {
    const editor = new CodexCodeEditor(adapter, config);
    const caps = {
        ...DEFAULT_CODEX_CAPABILITIES,
        ...capabilities,
    };
    orchestrator.registerExecutor('codex', editor, caps);
    return editor;
}
export function createOrchestratorWithAllEditors(config = {}) {
    const orchestrator = new CoworkOrchestrator();
    const aiderEditor = registerAiderExecutor(orchestrator, config.aiderConfig);
    let claudeEditor;
    let geminiEditor;
    let codexEditor;
    if (config.claudeAdapter) {
        claudeEditor = registerClaudeExecutor(orchestrator, config.claudeAdapter, config.claudeConfig);
    }
    if (config.geminiAdapter) {
        geminiEditor = registerGeminiExecutor(orchestrator, config.geminiAdapter, config.geminiConfig);
    }
    if (config.codexAdapter) {
        codexEditor = registerCodexExecutor(orchestrator, config.codexAdapter, config.codexConfig);
    }
    return {
        orchestrator,
        aiderEditor,
        claudeEditor,
        geminiEditor,
        codexEditor,
    };
}
//# sourceMappingURL=factory.js.map