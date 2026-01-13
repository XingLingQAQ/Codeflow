/**
 * Hook Bus 事件系统类型定义
 */
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
})(HookEvent || (HookEvent = {}));
//# sourceMappingURL=types.js.map