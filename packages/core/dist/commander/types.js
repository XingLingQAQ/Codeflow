/**
 * Commander Mode 类型定义
 * 实现 Main AI 调用 Coder/Sub Agent 的工具定义
 */
/**
 * Commander 事件
 */
export var CommanderEvent;
(function (CommanderEvent) {
    CommanderEvent["AGENT_REGISTERED"] = "agent_registered";
    CommanderEvent["TOOL_CALL_START"] = "tool_call_start";
    CommanderEvent["TOOL_CALL_END"] = "tool_call_end";
    CommanderEvent["CONTEXT_GRAFTED"] = "context_grafted";
    CommanderEvent["NESTED_CALL_START"] = "nested_call_start";
    CommanderEvent["NESTED_CALL_END"] = "nested_call_end";
})(CommanderEvent || (CommanderEvent = {}));
//# sourceMappingURL=types.js.map