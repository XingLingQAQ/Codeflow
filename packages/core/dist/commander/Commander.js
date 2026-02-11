/**
 * Commander Mode 实现
 * 指挥官模式编排 - Main AI 调用 Coder/Sub Agent
 */
import { CommanderEvent, } from './types.js';
export class Commander {
    constructor(hookManager, maxNestingDepth = 5) {
        this.agents = new Map();
        this.callStack = [];
        this.currentCallId = 0;
        this.eventHandlers = new Map();
        this.hookManager = hookManager;
        this.maxNestingDepth = maxNestingDepth;
    }
    registerAgent(config) {
        this.agents.set(config.role, config);
        this.emit(CommanderEvent.AGENT_REGISTERED, { role: config.role });
    }
    getAgent(role) {
        return this.agents.get(role);
    }
    async callCoderAgent(params) {
        const startTime = Date.now();
        const callId = this.generateCallId();
        const trace = {
            id: callId,
            parentId: this.getCurrentParentId(),
            agentRole: 'coder',
            toolName: 'call_coder_agent',
            params,
            startTime,
            children: [],
        };
        this.pushCall(trace);
        this.emit(CommanderEvent.TOOL_CALL_START, { trace });
        try {
            // 检查嵌套深度
            if (this.getCurrentDepth() > this.maxNestingDepth) {
                throw new Error(`Max nesting depth (${this.maxNestingDepth}) exceeded`);
            }
            const coderAgent = this.agents.get('coder');
            if (!coderAgent) {
                throw new Error('Coder agent not registered');
            }
            // 构建 Coder Agent 的 prompt
            const prompt = this.buildCoderPrompt(params);
            // 嫁接上下文
            const mainAgent = this.agents.get('main');
            if (mainAgent && params.context) {
                const graftedContext = await this.graftContext('main', 'coder', {
                    inheritMessages: true,
                    maxContextTokens: 4000,
                });
                coderAgent.adapter.setHistory(graftedContext.messages);
            }
            // 调用 Coder Agent
            const response = await coderAgent.adapter.send(prompt);
            const result = {
                success: true,
                output: response.content,
                agentRole: 'coder',
                tokenUsage: response.usage,
                duration: Date.now() - startTime,
            };
            trace.endTime = Date.now();
            trace.result = result;
            this.emit(CommanderEvent.TOOL_CALL_END, { trace, result });
            this.popCall();
            return result;
        }
        catch (error) {
            const errorMessage = error instanceof Error ? error.message : 'Unknown error';
            const result = {
                success: false,
                output: '',
                agentRole: 'coder',
                duration: Date.now() - startTime,
                error: errorMessage,
            };
            trace.endTime = Date.now();
            trace.result = result;
            this.emit(CommanderEvent.TOOL_CALL_END, { trace, result });
            this.popCall();
            return result;
        }
    }
    async consultSubExpert(params) {
        const startTime = Date.now();
        const callId = this.generateCallId();
        const trace = {
            id: callId,
            parentId: this.getCurrentParentId(),
            agentRole: 'sub_expert',
            toolName: 'consult_sub_expert',
            params,
            startTime,
            children: [],
        };
        this.pushCall(trace);
        this.emit(CommanderEvent.TOOL_CALL_START, { trace });
        try {
            // 检查嵌套深度
            const maxDepth = params.depth ?? this.maxNestingDepth;
            if (this.getCurrentDepth() > maxDepth) {
                throw new Error(`Max nesting depth (${maxDepth}) exceeded`);
            }
            const subExpert = this.agents.get('sub_expert');
            if (!subExpert) {
                throw new Error('Sub expert agent not registered');
            }
            // 构建 Sub Expert 的 prompt
            const prompt = this.buildSubExpertPrompt(params);
            // 嫁接上下文
            if (params.context) {
                const mainAgent = this.agents.get('main');
                if (mainAgent) {
                    const graftedContext = await this.graftContext('main', 'sub_expert', {
                        inheritMessages: true,
                        maxContextTokens: 2000,
                    });
                    subExpert.adapter.setHistory(graftedContext.messages);
                }
            }
            // 调用 Sub Expert
            const response = await subExpert.adapter.send(prompt);
            const result = {
                success: true,
                output: response.content,
                agentRole: 'sub_expert',
                tokenUsage: response.usage,
                duration: Date.now() - startTime,
            };
            trace.endTime = Date.now();
            trace.result = result;
            this.emit(CommanderEvent.TOOL_CALL_END, { trace, result });
            this.popCall();
            return result;
        }
        catch (error) {
            const errorMessage = error instanceof Error ? error.message : 'Unknown error';
            const result = {
                success: false,
                output: '',
                agentRole: 'sub_expert',
                duration: Date.now() - startTime,
                error: errorMessage,
            };
            trace.endTime = Date.now();
            trace.result = result;
            this.emit(CommanderEvent.TOOL_CALL_END, { trace, result });
            this.popCall();
            return result;
        }
    }
    async graftContext(sourceRole, targetRole, config) {
        const sourceAgent = this.agents.get(sourceRole);
        if (!sourceAgent) {
            throw new Error(`Source agent '${sourceRole}' not registered`);
        }
        const targetAgent = this.agents.get(targetRole);
        if (!targetAgent) {
            throw new Error(`Target agent '${targetRole}' not registered`);
        }
        const sourceHistory = sourceAgent.adapter.getHistory();
        let messages = [];
        if (config?.inheritMessages !== false) {
            messages = [...sourceHistory];
            // 过滤角色
            if (config?.filterRoles) {
                messages = messages.filter((m) => config.filterRoles.includes(m.role));
            }
            // 限制 token 数量
            if (config?.maxContextTokens) {
                const maxTokens = config.maxContextTokens;
                let totalTokens = 0;
                const filteredMessages = [];
                // 从最新消息开始保留
                for (let i = messages.length - 1; i >= 0; i--) {
                    const msgTokens = this.estimateMessageTokens(messages[i]);
                    if (totalTokens + msgTokens <= maxTokens) {
                        filteredMessages.unshift(messages[i]);
                        totalTokens += msgTokens;
                    }
                    else {
                        break;
                    }
                }
                messages = filteredMessages;
            }
        }
        const graftedContext = {
            messages,
            systemPrompt: config?.inheritSystemPrompt ? sourceAgent.systemPrompt : undefined,
            metadata: {
                sourceAgent: sourceRole,
                graftedAt: Date.now(),
                tokenCount: Math.ceil(messages.reduce((acc, m) => acc + m.content.length, 0) / 4),
            },
        };
        this.emit(CommanderEvent.CONTEXT_GRAFTED, {
            sourceRole,
            targetRole,
            context: graftedContext,
        });
        return graftedContext;
    }
    getToolDefinitions() {
        return [
            {
                name: 'call_coder_agent',
                description: 'Delegate a coding task to the Coder Agent. Use this for code generation, refactoring, debugging, or any programming-related tasks.',
                parameters: {
                    type: 'object',
                    properties: {
                        task: {
                            type: 'string',
                            description: 'The coding task to perform',
                        },
                        context: {
                            type: 'string',
                            description: 'Additional context or requirements for the task',
                        },
                        files: {
                            type: 'array',
                            description: 'List of file paths relevant to the task',
                            items: { type: 'string' },
                        },
                        language: {
                            type: 'string',
                            description: 'Programming language for the task',
                        },
                        constraints: {
                            type: 'array',
                            description: 'Constraints or requirements to follow',
                            items: { type: 'string' },
                        },
                    },
                    required: ['task'],
                },
            },
            {
                name: 'consult_sub_expert',
                description: 'Consult a domain-specific sub-expert for specialized knowledge. Use this for questions requiring deep expertise in a specific area.',
                parameters: {
                    type: 'object',
                    properties: {
                        domain: {
                            type: 'string',
                            description: 'The domain of expertise (e.g., "security", "performance", "architecture")',
                        },
                        question: {
                            type: 'string',
                            description: 'The question to ask the sub-expert',
                        },
                        context: {
                            type: 'string',
                            description: 'Additional context for the question',
                        },
                        depth: {
                            type: 'number',
                            description: 'Maximum nesting depth for recursive consultations',
                        },
                    },
                    required: ['domain', 'question'],
                },
            },
        ];
    }
    // 事件系统
    on(event, handler) {
        const handlers = this.eventHandlers.get(event) || [];
        handlers.push(handler);
        this.eventHandlers.set(event, handlers);
    }
    off(event, handler) {
        const handlers = this.eventHandlers.get(event) || [];
        const index = handlers.indexOf(handler);
        if (index !== -1) {
            handlers.splice(index, 1);
        }
    }
    // 获取调用追踪
    getCallTrace() {
        return [...this.callStack];
    }
    // ==================== 私有方法 ====================
    generateCallId() {
        return `call_${++this.currentCallId}_${Date.now()}`;
    }
    getCurrentParentId() {
        if (this.callStack.length === 0)
            return undefined;
        return this.callStack[this.callStack.length - 1].id;
    }
    getCurrentDepth() {
        return this.callStack.length;
    }
    pushCall(trace) {
        if (this.callStack.length > 0) {
            const parent = this.callStack[this.callStack.length - 1];
            parent.children.push(trace);
            this.emit(CommanderEvent.NESTED_CALL_START, { trace, parent });
        }
        this.callStack.push(trace);
    }
    popCall() {
        const trace = this.callStack.pop();
        if (trace && this.callStack.length > 0) {
            this.emit(CommanderEvent.NESTED_CALL_END, { trace });
        }
    }
    emit(event, data) {
        const handlers = this.eventHandlers.get(event) || [];
        for (const handler of handlers) {
            try {
                handler(data);
            }
            catch {
                // 忽略事件处理器错误
            }
        }
    }
    buildCoderPrompt(params) {
        let prompt = `## Coding Task\n\n${params.task}`;
        if (params.context) {
            prompt += `\n\n## Context\n\n${params.context}`;
        }
        if (params.files && params.files.length > 0) {
            prompt += `\n\n## Relevant Files\n\n${params.files.map((f) => `- ${f}`).join('\n')}`;
        }
        if (params.language) {
            prompt += `\n\n## Language\n\n${params.language}`;
        }
        if (params.constraints && params.constraints.length > 0) {
            prompt += `\n\n## Constraints\n\n${params.constraints.map((c) => `- ${c}`).join('\n')}`;
        }
        return prompt;
    }
    buildSubExpertPrompt(params) {
        let prompt = `## Domain: ${params.domain}\n\n## Question\n\n${params.question}`;
        if (params.context) {
            prompt += `\n\n## Context\n\n${params.context}`;
        }
        return prompt;
    }
    /**
     * 估算消息的 Token 数量
     * 使用改进的启发式算法
     */
    estimateMessageTokens(message) {
        const content = message.content;
        if (!content)
            return 0;
        let tokens = 0;
        // 1. 检测代码块
        const codeBlockRegex = /```[\s\S]*?```/g;
        const codeBlocks = content.match(codeBlockRegex) || [];
        let nonCodeContent = content;
        for (const block of codeBlocks) {
            tokens += Math.ceil(block.length / 3.5);
            nonCodeContent = nonCodeContent.replace(block, '');
        }
        // 2. 检测 CJK 字符
        const cjkRegex = /[\u4e00-\u9fff\u3040-\u309f\u30a0-\u30ff\uac00-\ud7af]/g;
        const cjkChars = nonCodeContent.match(cjkRegex) || [];
        tokens += cjkChars.length * 1.5;
        const nonCjkContent = nonCodeContent.replace(cjkRegex, '');
        // 3. 计算英文和其他内容
        const words = nonCjkContent.split(/\s+/).filter(w => w.length > 0);
        for (const word of words) {
            if (/^\d+$/.test(word)) {
                tokens += Math.ceil(word.length / 3);
            }
            else if (/^[a-zA-Z]+$/.test(word)) {
                if (word.length <= 4) {
                    tokens += 1;
                }
                else if (word.length <= 8) {
                    tokens += 1.5;
                }
                else {
                    tokens += Math.ceil(word.length / 4);
                }
            }
            else {
                tokens += Math.ceil(word.length / 3);
            }
        }
        // 4. 标点和换行
        const punctuation = nonCjkContent.match(/[.,!?;:'"()\[\]{}]/g) || [];
        tokens += punctuation.length * 0.3;
        const newlines = content.match(/\n/g) || [];
        tokens += newlines.length * 0.5;
        // 5. 角色标记开销
        tokens += 4; // 每条消息的角色标记
        return Math.ceil(tokens);
    }
}
//# sourceMappingURL=Commander.js.map