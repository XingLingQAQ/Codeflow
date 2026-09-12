/**
 * Hook Bus 事件系统类型定义
 */
export function cloneMessagePart(part) {
    if (part.type === 'text') {
        return { ...part };
    }
    if (part.type === 'tool_call') {
        return {
            ...part,
            args: part.args === undefined ? undefined : structuredClone(part.args),
        };
    }
    if (part.type === 'tool_result') {
        return {
            ...part,
            result: part.result === undefined ? undefined : structuredClone(part.result),
        };
    }
    return {
        ...part,
        data: structuredClone(part.data),
    };
}
export function normalizeMessageContent(content) {
    if (typeof content === 'string') {
        return [{ type: 'text', text: content }];
    }
    return content.map((part) => cloneMessagePart(part));
}
export function getMessageText(content) {
    if (typeof content === 'string') {
        return content;
    }
    return content
        .map((part) => {
        switch (part.type) {
            case 'text':
                return part.text;
            case 'tool_call':
                return `[tool_call:${part.toolName}] ${safeJsonStringify(part.args)}`.trim();
            case 'tool_result':
                return `[tool_result:${part.toolName}] ${safeJsonStringify(part.result)}`.trim();
            case 'json':
                return safeJsonStringify(part.data);
            default:
                return '';
        }
    })
        .filter((segment) => segment.length > 0)
        .join('\n');
}
export function hasMessagePartType(content, type) {
    return normalizeMessageContent(content).some((part) => part.type === type);
}
export function isToolTurnMessage(message) {
    return hasMessagePartType(message.content, 'tool_call') || hasMessagePartType(message.content, 'tool_result');
}
export function cloneMessage(message) {
    return {
        ...message,
        content: typeof message.content === 'string'
            ? message.content
            : message.content.map((part) => cloneMessagePart(part)),
        metadata: message.metadata ? structuredClone(message.metadata) : undefined,
    };
}
export function serializeMessageContent(content) {
    if (typeof content === 'string') {
        return content;
    }
    return JSON.stringify({ __codeflowMessageContent: true, parts: content });
}
export function deserializeMessageContent(content) {
    if (!content.startsWith('{')) {
        return content;
    }
    try {
        const parsed = JSON.parse(content);
        if (parsed.__codeflowMessageContent && Array.isArray(parsed.parts)) {
            return parsed.parts.map((part) => cloneMessagePart(part));
        }
    }
    catch {
        return content;
    }
    return content;
}
function safeJsonStringify(value) {
    if (value === undefined) {
        return '';
    }
    if (typeof value === 'string') {
        return value;
    }
    try {
        return JSON.stringify(value);
    }
    catch {
        return String(value);
    }
}
/**
 * Hook 事件类型枚举
 */
export var HookEvent;
(function (HookEvent) {
    HookEvent["BEFORE_SEND"] = "before_send";
    HookEvent["POST_RESPONSE"] = "post_response";
    HookEvent["ON_STREAM"] = "on_stream";
    HookEvent["BEFORE_COMPRESS"] = "before_compress";
    HookEvent["MESSAGE_COMPLETE"] = "message_complete";
    HookEvent["AFTER_EXEC"] = "after_exec";
    HookEvent["RESTORE_STATE"] = "restore_state";
    HookEvent["USER_INPUT_SUBMITTED"] = "user_input_submitted";
    HookEvent["BEFORE_TASK_EXECUTE"] = "before_task_execute";
    HookEvent["AFTER_TASK_EXECUTE"] = "after_task_execute";
    HookEvent["ON_TASK_FAILURE"] = "on_task_failure";
    HookEvent["ON_TASK_COMPLETE"] = "on_task_complete";
})(HookEvent || (HookEvent = {}));
//# sourceMappingURL=types.js.map