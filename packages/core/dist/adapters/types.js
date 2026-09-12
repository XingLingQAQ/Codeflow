/**
 * CLI Adapter 接口类型定义
 */
import { cloneMessage, getMessageText, serializeMessageContent, deserializeMessageContent, isToolTurnMessage, } from '../hooks/types.js';
export function cloneMessages(messages) {
    return messages.map((message) => cloneMessage(message));
}
export function messageToText(message) {
    return getMessageText(message.content);
}
export function messagesToPrompt(messages, separator = '\n\n') {
    return messages.map((message) => messageToText(message)).join(separator);
}
export function serializeMessage(message) {
    return {
        ...cloneMessage(message),
        content: serializeMessageContent(message.content),
    };
}
export function deserializeMessage(message) {
    return {
        ...cloneMessage(message),
        content: typeof message.content === 'string'
            ? deserializeMessageContent(message.content)
            : message.content,
    };
}
export function toHookPayload(context) {
    return {
        messages: cloneMessages(context.messages),
        model: context.model,
        temperature: context.temperature,
        maxTokens: context.maxTokens,
    };
}
export function applyHookPayload(context, payload) {
    const hasMessageOverride = Array.isArray(payload.messages) && payload.messages.length > 0;
    const shouldApplyScalarOverrides = hasMessageOverride || !Array.isArray(payload.messages);
    return {
        messages: hasMessageOverride
            ? cloneMessages(payload.messages)
            : cloneMessages(context.messages),
        model: shouldApplyScalarOverrides && typeof payload.model === 'string' && payload.model.length > 0
            ? payload.model
            : context.model,
        temperature: shouldApplyScalarOverrides ? payload.temperature ?? context.temperature : context.temperature,
        maxTokens: shouldApplyScalarOverrides ? payload.maxTokens ?? context.maxTokens : context.maxTokens,
    };
}
export function splitHistoryForGovernance(messages) {
    const systemMessages = [];
    const dialogueMessages = [];
    for (const message of messages) {
        if (message.role === 'system') {
            systemMessages.push(cloneMessage(message));
            continue;
        }
        dialogueMessages.push(cloneMessage(message));
    }
    return { systemMessages, dialogueMessages };
}
export function countCompletedTurns(messages) {
    return messages.filter((message) => message.role === 'assistant').length;
}
export function rewindHistoryByTurns(messages, steps) {
    if (steps <= 0) {
        throw new Error('Steps must be positive');
    }
    const { systemMessages, dialogueMessages } = splitHistoryForGovernance(messages);
    const assistantBoundaries = dialogueMessages.flatMap((message, index) => message.role === 'assistant' ? [index + 1] : []);
    const availableTurns = assistantBoundaries.length;
    if (steps > availableTurns) {
        throw new Error(`Cannot rewind ${steps} steps, only ${availableTurns} rounds available`);
    }
    const turnsToKeep = availableTurns - steps;
    const cutoff = turnsToKeep === 0 ? 0 : assistantBoundaries[turnsToKeep - 1];
    return [...systemMessages, ...dialogueMessages.slice(0, cutoff)].map((message) => cloneMessage(message));
}
export async function compactHistoryWithSummary(messages, options = {}) {
    if (messages.length === 0) {
        return [];
    }
    const { systemMessages, dialogueMessages } = splitHistoryForGovernance(messages);
    if (dialogueMessages.length === 0 || !options.buildSkeleton) {
        return [...systemMessages, ...dialogueMessages].map((message) => cloneMessage(message));
    }
    const estimateTokens = options.estimateTokens ??
        ((history) => history.reduce((sum, message) => sum + Math.ceil(messageToText(message).length / 4), 0));
    const keepRatio = options.keepRatio ?? 0.2;
    const minimumRecentMessages = options.minimumRecentMessages ?? 2;
    const keepCount = Math.min(dialogueMessages.length, Math.max(minimumRecentMessages, Math.ceil(dialogueMessages.length * keepRatio)));
    const recentMessages = dialogueMessages.slice(-keepCount);
    const olderMessages = dialogueMessages.slice(0, -keepCount);
    if (olderMessages.length === 0) {
        return [...systemMessages, ...recentMessages].map((message) => cloneMessage(message));
    }
    const skeletonSource = [...systemMessages, ...olderMessages];
    const skeleton = await options.buildSkeleton(skeletonSource, estimateTokens(skeletonSource));
    const relations = skeleton.relations.map((relation) => `${relation.from} ${relation.type} ${relation.to}`).join(', ');
    const summaryMessage = {
        role: 'system',
        content: `[Compressed Context]\nEntities: ${skeleton.entities.join(', ')}\nDecisions: ${skeleton.decisions.join('; ')}\nRelations: ${relations}`,
        timestamp: options.summaryTimestamp ?? Date.now(),
    };
    const preservedSystemMessages = systemMessages.filter((message) => !messageToText(message).startsWith('[Compressed Context]'));
    return [...preservedSystemMessages, summaryMessage, ...recentMessages].map((message) => cloneMessage(message));
}
export function hasToolTurns(messages) {
    return messages.some((message) => isToolTurnMessage(message));
}
/**
 * API 错误类型
 */
export class APIError extends Error {
    constructor(message, statusCode, code, retryable = false) {
        super(message);
        this.statusCode = statusCode;
        this.code = code;
        this.retryable = retryable;
        this.name = 'APIError';
    }
}
/**
 * 超时错误
 */
export class TimeoutError extends Error {
    constructor(message = 'Request timeout') {
        super(message);
        this.name = 'TimeoutError';
    }
}
//# sourceMappingURL=types.js.map