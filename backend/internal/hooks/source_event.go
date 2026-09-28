// Package hooks - Source event reference for audit records (T1.07.c).
//
// 本文件是 T1.07.c 的审计增强：hook 的每一次触发记录都带上来源事件的 ID 与去重键
// （计划 §28 T1.07.c“审计按 event_id 去重”、§15 T1.07“一个事件只有一个触发记录”）。
//
// 为什么需要它：HookEvent.ID 是每次触发新生成的 uuid，Trigger 本身看不到“这次触发
// 是由哪条执行事件引起的”。没有来源事件 ID，审计读取方就无法按 event_id 去重或核对
// “一个事件只有一个触发记录”，只能数行数。来源事件 ID 由调用方从权威资源填进 payload
// （ToolHookPayload.EventID / RunHookPayload.EventID，pre 是 tool.requested、post 是
// 结果事件、RunStart 是 scheduler.claimed、RunFinish 是终态事件），本文件只把它抄进
// 触发记录。
//
// 边界（写在这里，避免被当成承诺）：管理器不新增去重状态，也不因为同一 event id 出现
// 两次而拒绝第二次触发。单飞记忆在适配器（runhooks.Port）里，审计携带 event id 只是让
// 读取方能按 event_id 去重；有界记忆淘汰之后同一事件再次触发会留下第二条同 event_id 的
// 记录（见 runhooks 的 T1.07.c 测试），持久化的触发记录归 T1.04。
package hooks

// SourceEventIDKey 是触发记录的来源事件 ID 键：同时出现在 HookEvent.Metadata 与审计
// Details 里。值为服务端分配的事件 ID（不是后端序号、不是 ProviderRef）。
const SourceEventIDKey = "source_event_id"

// DedupeKeyKey 是触发记录的去重键键：hook 类型 + ":" + 来源事件 ID，与
// ToolHookPayload.DedupeKey / RunHookPayload.DedupeKey 同值（runhooks 的单飞记忆
// 用的就是它）。审计读取方按它可以一行看出“这两条记录说的是同一个事件”。
const DedupeKeyKey = "dedupe_key"

// SourceEventReference 从 payload 里取出这次触发所观察的来源事件 ID，并算出对应的
// 去重键。只有两个保留 payload 契约携带事件 ID（ToolHookPayload / RunHookPayload，
// 值与指针都接受）；其它 payload（字符串、map、历史 hook 的自定义结构）没有来源事件
// 的概念，返回两个空串——空值不会被写进记录（空键绝不当真键）。
//
// 去重键复用 payload.go 的 dedupeKey()（"<hook>:<event id>"），所以管理器的审计与
// runhooks 的单飞记忆永远说的是同一个键。
func SourceEventReference(hookType HookType, payload HookPayload) (eventID, key string) {
	switch typed := payload.(type) {
	case ToolHookPayload:
		eventID = typed.EventID
	case *ToolHookPayload:
		if typed != nil {
			eventID = typed.EventID
		}
	case RunHookPayload:
		eventID = typed.EventID
	case *RunHookPayload:
		if typed != nil {
			eventID = typed.EventID
		}
	}
	return eventID, dedupeKey(hookType, eventID)
}

// annotateHookEvent 把来源事件 ID 与去重键写进触发事件的 Metadata（空值不写键）。
// 传 nil metadata 时是 no-op（调用方没有建元数据就没有可写的地方）。
func annotateHookEvent(metadata HookMetadata, sourceEventID, dedupeKey string) {
	if metadata == nil {
		return
	}
	if sourceEventID != "" {
		metadata[SourceEventIDKey] = sourceEventID
	}
	if dedupeKey != "" {
		metadata[DedupeKeyKey] = dedupeKey
	}
}

// copySourceEventDetails 把触发事件里已经写好的来源事件键抄进审计 Details（空值不写
// 键）。它只抄这两个键：审计 Details 的其余键由 recordAuditEvent 自己构造，不受影响。
func copySourceEventDetails(details, eventMetadata HookMetadata) {
	if details == nil || eventMetadata == nil {
		return
	}
	for _, key := range []string{SourceEventIDKey, DedupeKeyKey} {
		if value, ok := eventMetadata[key]; ok {
			details[key] = value
		}
	}
}
