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
    HookEvent["BEFORE_TASK_EXECUTE"] = "before_task_execute";
    HookEvent["AFTER_TASK_EXECUTE"] = "after_task_execute";
    HookEvent["ON_TASK_FAILURE"] = "on_task_failure";
    HookEvent["ON_TASK_COMPLETE"] = "on_task_complete";
})(HookEvent || (HookEvent = {}));
//# sourceMappingURL=types.js.map