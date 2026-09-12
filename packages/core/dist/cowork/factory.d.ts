import { HookManager } from '../hooks/HookManager.js';
import type { HookRuntimeControls } from '../hooks/types.js';
/**
 * Cowork Factory
 * 工厂函数用于创建和注册执行器到 Orchestrator
 */
import { CoworkOrchestrator } from './CoworkOrchestrator.js';
import { AiderConfig } from './adapters/AiderAdapter.js';
import { CodexCLIAdapter } from './adapters/CodexCLIAdapter.js';
import { GeminiCLIAdapter } from './adapters/GeminiCLIAdapter.js';
import { AiderCodeEditor, AiderEditorConfig } from './editors/AiderCodeEditor.js';
import { ClaudeCodeEditor, ClaudeEditorConfig } from './editors/ClaudeCodeEditor.js';
import { GeminiCodeEditor, GeminiEditorAdapter, GeminiEditorConfig } from './editors/GeminiCodeEditor.js';
import { CodexCodeEditor, CodexEditorAdapter, CodexEditorConfig } from './editors/CodexCodeEditor.js';
import { ClaudeAdapter } from '../adapters/ClaudeAdapter.js';
import { GeminiAdapter } from '../adapters/GeminiAdapter.js';
import { CodexAdapter } from '../adapters/CodexAdapter.js';
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
 * 创建并注册 Aider 执行器
 */
export declare function registerAiderExecutor(orchestrator: CoworkOrchestrator, config?: AiderExecutorConfig): AiderCodeEditor;
/**
 * 创建预配置的 Orchestrator（包含 Aider）
 */
export declare function createOrchestratorWithAider(aiderConfig?: AiderExecutorConfig): {
    orchestrator: CoworkOrchestrator;
    aiderEditor: AiderCodeEditor;
};
/**
 * 创建任务辅助函数
 */
export declare function createTask(id: string, executor: string, files: string[], instruction: string, options?: {
    type?: 'code-edit' | 'test-gen' | 'refactor' | 'review' | 'debug' | 'explain';
    context?: string;
    timeout?: number;
    priority?: number;
}): {
    id: string;
    type: "code-edit" | "test-gen" | "refactor" | "review" | "debug" | "explain";
    executor: string;
    input: {
        files: string[];
        instruction: string;
        context: string | undefined;
    };
    config: {
        timeout: number | undefined;
        priority: number | undefined;
    };
    status: "pending";
    createdAt: number;
};
/**
 * 创建并注册 Claude 执行器
 */
export declare function registerClaudeExecutor(orchestrator: CoworkOrchestrator, adapter: ClaudeAdapter, config?: ClaudeEditorConfig, capabilities?: Partial<ExecutorCapabilities>, hookManager?: HookManager): ClaudeCodeEditor;
/**
 * 创建并注册 Gemini 执行器
 */
export declare function registerGeminiExecutor(orchestrator: CoworkOrchestrator, adapter: GeminiEditorAdapter, config?: GeminiEditorConfig, capabilities?: Partial<ExecutorCapabilities>, hookManager?: HookManager): GeminiCodeEditor;
/**
 * 创建并注册 Codex 执行器
 */
export declare function registerCodexExecutor(orchestrator: CoworkOrchestrator, adapter: CodexEditorAdapter, config?: CodexEditorConfig, capabilities?: Partial<ExecutorCapabilities>, hookManager?: HookManager): CodexCodeEditor;
/**
 * 创建包含所有编辑器的 Orchestrator
 */
export interface AllEditorsConfig {
    aiderConfig?: AiderExecutorConfig;
    claudeAdapter?: ClaudeAdapter;
    claudeConfig?: ClaudeEditorConfig;
    geminiAdapter?: GeminiAdapter;
    geminiCliAdapter?: GeminiCLIAdapter;
    geminiConfig?: GeminiEditorConfig;
    codexAdapter?: CodexAdapter;
    codexCliAdapter?: CodexCLIAdapter;
    codexConfig?: CodexEditorConfig;
    hookManager?: HookManager;
    hookControls?: HookRuntimeControls;
}
export declare function createOrchestratorWithAllEditors(config?: AllEditorsConfig): {
    orchestrator: CoworkOrchestrator;
    aiderEditor: AiderCodeEditor;
    claudeEditor?: ClaudeCodeEditor;
    geminiEditor?: GeminiCodeEditor;
    codexEditor?: CodexCodeEditor;
};
//# sourceMappingURL=factory.d.ts.map