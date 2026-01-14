/**
 * Commander Mode 类型定义
 * 实现 Main AI 调用 Coder/Sub Agent 的工具定义
 */

import { Message } from '../hooks/types.js';
import { ICliAdapter } from '../adapters/types.js';

/**
 * Agent 角色类型
 */
export type AgentRole = 'main' | 'coder' | 'sub_expert';

/**
 * Agent 配置
 */
export interface AgentConfig {
  role: AgentRole;
  adapter: ICliAdapter;
  systemPrompt?: string;
  maxDepth?: number;
  timeout?: number;
}

/**
 * 工具调用参数 - call_coder_agent
 */
export interface CallCoderAgentParams {
  task: string;
  context?: string;
  files?: string[];
  language?: string;
  constraints?: string[];
  [key: string]: unknown;
}

/**
 * 工具调用参数 - consult_sub_expert
 */
export interface ConsultSubExpertParams {
  domain: string;
  question: string;
  context?: string;
  depth?: number;
  [key: string]: unknown;
}

/**
 * 工具调用结果
 */
export interface ToolCallResult {
  success: boolean;
  output: string;
  agentRole: AgentRole;
  tokenUsage?: {
    promptTokens: number;
    completionTokens: number;
    totalTokens: number;
  };
  duration?: number;
  error?: string;
}

/**
 * 上下文嫁接配置
 */
export interface ContextGraftConfig {
  inheritMessages?: boolean;
  inheritSystemPrompt?: boolean;
  maxContextTokens?: number;
  filterRoles?: Array<'user' | 'assistant' | 'system'>;
}

/**
 * 嫁接后的上下文
 */
export interface GraftedContext {
  messages: Message[];
  systemPrompt?: string;
  metadata: {
    sourceAgent: AgentRole;
    graftedAt: number;
    tokenCount: number;
  };
}

/**
 * 工具定义接口
 */
export interface ToolDefinition {
  name: string;
  description: string;
  parameters: {
    type: 'object';
    properties: Record<
      string,
      {
        type: string;
        description: string;
        enum?: string[];
        items?: { type: string };
      }
    >;
    required: string[];
  };
}

/**
 * Commander 接口
 */
export interface ICommander {
  // Agent 管理
  registerAgent(config: AgentConfig): void;
  getAgent(role: AgentRole): AgentConfig | undefined;

  // 工具调用
  callCoderAgent(params: CallCoderAgentParams): Promise<ToolCallResult>;
  consultSubExpert(params: ConsultSubExpertParams): Promise<ToolCallResult>;

  // 上下文嫁接
  graftContext(
    sourceRole: AgentRole,
    targetRole: AgentRole,
    config?: ContextGraftConfig
  ): Promise<GraftedContext>;

  // 工具定义
  getToolDefinitions(): ToolDefinition[];
}

/**
 * 嵌套调用追踪
 */
export interface CallTrace {
  id: string;
  parentId?: string;
  agentRole: AgentRole;
  toolName: string;
  params: Record<string, unknown>;
  startTime: number;
  endTime?: number;
  result?: ToolCallResult;
  children: CallTrace[];
}

/**
 * Commander 事件
 */
export enum CommanderEvent {
  AGENT_REGISTERED = 'agent_registered',
  TOOL_CALL_START = 'tool_call_start',
  TOOL_CALL_END = 'tool_call_end',
  CONTEXT_GRAFTED = 'context_grafted',
  NESTED_CALL_START = 'nested_call_start',
  NESTED_CALL_END = 'nested_call_end',
}

/**
 * Commander 事件处理器
 */
export type CommanderEventHandler<T = unknown> = (data: T) => void | Promise<void>;
