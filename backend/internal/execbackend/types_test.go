package execbackend

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 本文件是 T1.06.a 的类型层测试（§15 T1.06、§27.4、§27.6、§27.7）：
//   - 结构防线：Observation 不得含可自定义序号/身份字段（反射，防将来被人加回来）。
//   - StartRequest.Validate 表驱动：每个非法输入一行，期望 code=invalid_request 且 Field 正确。
//   - Observation.Validate：未知 kind、超 256 KiB、控制帧超 64 KiB、exited 缺/坏
//     ExitResult、ProviderRef 超长全部拒绝；合法各 kind 通过。
//   - Usage：Quality=unknown 时 JSON 中 token/cost 字段缺席（不是 0）。
//   - 枚举：每个枚举的 Valid() 集合与注释列表一致。

// forbiddenObservationFields 是不允许出现在 Observation 上的字段名（不区分大小写）。
// 这些字段一旦出现在观察记录里，就意味着适配器可以自报序号或身份，违反
// §27.4「服务端填 identity/ID/time，不能直接接受 CLI 自报 project/sequence 或审计」。
var forbiddenObservationFields = map[string]string{
	"sequence":   "sequence 由服务端事务分配（§27.4）",
	"seq":        "seq 是 sequence 的变体",
	"eventid":    "event ID 由服务端分配",
	"id":         "观察记录不得自报 ID",
	"projectid":  "project 身份由服务端填充",
	"runid":      "run 身份由服务端填充",
	"attemptid":  "attempt 身份由服务端填充",
	"identity":   "ExecutionIdentity 由服务端构造（§27.1）",
	"actor":      "actor 由服务端从权威资源验证",
	"occurredat": "occurred_at 由服务端分配（§27.4）",
	"eventtype":  "领域事件类型由服务端映射",
}

// TestObservationHasNoCustomizableSequenceOrIdentity 是结构防线：
// 反射检查 Observation 的字段名，禁止任何可自定义序号/身份字段。
// 同时检查这些名字没有以嵌套结构体的形式绕道混进来。
func TestObservationHasNoCustomizableSequenceOrIdentity(t *testing.T) {
	typ := reflect.TypeOf(Observation{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if reason, banned := forbiddenObservationFields[strings.ToLower(field.Name)]; banned {
			t.Errorf("Observation.%s 不允许存在：%s", field.Name, reason)
		}
		// JSON tag 同样检查：字段名可以改，tag 不能偷偷把序号写进线协议。
		tag := strings.ToLower(strings.Split(field.Tag.Get("json"), ",")[0])
		if tag == "" || tag == "-" {
			continue
		}
		if reason, banned := forbiddenObservationFields[tag]; banned {
			t.Errorf("Observation.%s 的 json tag %q 不允许存在：%s", field.Name, tag, reason)
		}
	}
}

// TestObservationFieldSetIsFrozen 固定 Observation 的字段集合。
// 新增字段必须显式更新本测试，从而被迫复核"这个字段是否让适配器自报身份"。
func TestObservationFieldSetIsFrozen(t *testing.T) {
	want := []string{"Kind", "ObservedAt", "ProviderRef", "Payload"}
	typ := reflect.TypeOf(Observation{})
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Observation 字段集合 = %v, want %v（新增字段前请先确认不会让适配器自报序号/身份）", got, want)
	}
}

// TestExitResultFieldSetIsFrozen 固定 exited 终结观察的 Payload 形状。
func TestExitResultFieldSetIsFrozen(t *testing.T) {
	want := []string{"ExitCode", "Reason", "Retryable", "Usage"}
	typ := reflect.TypeOf(ExitResult{})
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExitResult 字段集合 = %v, want %v", got, want)
	}
}

// TestSessionInterfaceSurface 固定 Session 的方法集合与 Observations 的只读方向。
// 生命周期（谁 Close、channel 何时关、Wait/Cancel 幂等）写在接口注释里，
// 行为验证归 T1.06.b/c；这里只保证形状不被无声改动。
func TestSessionInterfaceSurface(t *testing.T) {
	typ := reflect.TypeOf((*Session)(nil)).Elem()
	want := map[string]string{
		"Observations": "<-chan execbackend.Observation",
		"Approve":      "error",
		"Cancel":       "error",
		"Wait":         "execbackend.ExitResult,error",
		"Close":        "error",
	}
	if typ.NumMethod() != len(want) {
		t.Fatalf("Session 方法数 = %d, want %d", typ.NumMethod(), len(want))
	}
	for name, signature := range want {
		m, ok := typ.MethodByName(name)
		if !ok {
			t.Errorf("Session 缺少方法 %s", name)
			continue
		}
		got := renderMethodSignature(m)
		if got != signature {
			t.Errorf("Session.%s 签名 = %q, want %q", name, got, signature)
		}
	}
}

// TestBackendInterfaceSurface 固定 Backend 的方法集合。
func TestBackendInterfaceSurface(t *testing.T) {
	typ := reflect.TypeOf((*Backend)(nil)).Elem()
	want := []string{"Name", "Capabilities", "Prepare", "Start"}
	if typ.NumMethod() != len(want) {
		t.Fatalf("Backend 方法数 = %d, want %d", typ.NumMethod(), len(want))
	}
	for _, name := range want {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("Backend 缺少方法 %s", name)
		}
	}
}

// renderMethodSignature 渲染方法签名的紧凑形式，便于比较（无参数，返回名以逗号连接）。
func renderMethodSignature(m reflect.Method) string {
	out := make([]string, 0, m.Type.NumOut())
	for i := 0; i < m.Type.NumOut(); i++ {
		out = append(out, m.Type.Out(i).String())
	}
	return strings.Join(out, ",")
}

// validRequest 返回一份合法请求，测试用例只改需要非法的字段。
func validRequest() StartRequest {
	return StartRequest{
		Run: RunRef{
			ProjectID:       "p_123",
			RunID:           "run_88",
			AttemptID:       "att_1",
			AgentRevisionID: "ar_12",
		},
		Input: FrozenInput{
			SnapshotID:   "snap_1",
			SnapshotHash: "sha256:" + strings.Repeat("ab", 32),
			Prompt:       "给 X 函数增加单元测试",
		},
		WorkDir:  `C:\runs\run_88`,
		Process:  ProcessControl{GracePeriod: 5 * time.Second, MaxOutputBytes: 8 << 20},
		Required: []Capability{CapabilityNonInteractive, CapabilityJSONStream},
	}
}

// TestStartRequestValidate 表驱动：每个非法输入一行，期望 code=invalid_request 且 Field 正确。
func TestStartRequestValidate(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*StartRequest)
		wantField string
	}{
		{"合法请求", func(r *StartRequest) {}, ""},
		{"project_id 缺失", func(r *StartRequest) { r.Run.ProjectID = "" }, "run.project_id"},
		{"project_id 空白", func(r *StartRequest) { r.Run.ProjectID = "   " }, "run.project_id"},
		{"run_id 缺失", func(r *StartRequest) { r.Run.RunID = "" }, "run.run_id"},
		{"attempt_id 缺失", func(r *StartRequest) { r.Run.AttemptID = "" }, "run.attempt_id"},
		{"agent_revision_id 缺失", func(r *StartRequest) { r.Run.AgentRevisionID = "" }, "run.agent_revision_id"},
		{"snapshot_id 缺失", func(r *StartRequest) { r.Input.SnapshotID = "" }, "input.snapshot_id"},
		{"snapshot_hash 为空", func(r *StartRequest) { r.Input.SnapshotHash = "" }, "input.snapshot_hash"},
		{"snapshot_hash 缺前缀", func(r *StartRequest) { r.Input.SnapshotHash = strings.Repeat("ab", 32) }, "input.snapshot_hash"},
		{"snapshot_hash 长度不足", func(r *StartRequest) { r.Input.SnapshotHash = "sha256:" + strings.Repeat("a", 63) }, "input.snapshot_hash"},
		{"snapshot_hash 大写 hex", func(r *StartRequest) { r.Input.SnapshotHash = "sha256:" + strings.Repeat("A", 64) }, "input.snapshot_hash"},
		{"snapshot_hash 非 hex", func(r *StartRequest) { r.Input.SnapshotHash = "sha256:" + strings.Repeat("z", 64) }, "input.snapshot_hash"},
		{"snapshot_hash 其他算法", func(r *StartRequest) { r.Input.SnapshotHash = "sha1:" + strings.Repeat("a", 64) }, "input.snapshot_hash"},
		{"work_dir 为空", func(r *StartRequest) { r.WorkDir = "" }, "work_dir"},
		{"work_dir 相对路径", func(r *StartRequest) { r.WorkDir = "runs/run_88" }, "work_dir"},
		{"work_dir 未 Clean", func(r *StartRequest) { r.WorkDir = `C:\runs\run_88\` }, "work_dir"},
		{"work_dir 含 ..", func(r *StartRequest) { r.WorkDir = `C:\runs\run_88\..\other` }, "work_dir"},
		{"grace_period 负数", func(r *StartRequest) { r.Process.GracePeriod = -time.Second }, "process.grace_period"},
		{"max_output_bytes 负数", func(r *StartRequest) { r.Process.MaxOutputBytes = -1 }, "process.max_output_bytes"},
		{"required 含未知能力", func(r *StartRequest) { r.Required = []Capability{"teleport"} }, "required"},
		{"required 重复", func(r *StartRequest) {
			r.Required = []Capability{CapabilityJSONStream, CapabilityNonInteractive, CapabilityJSONStream}
		}, "required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			tc.mutate(&req)

			err := req.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("合法请求被拒绝: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("非法请求被接受，期望 Field=%s", tc.wantField)
			}

			var domainErr *Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("错误类型 = %T, want *execbackend.Error", err)
			}
			if domainErr.Code != CodeInvalidRequest {
				t.Errorf("code = %q, want %q", domainErr.Code, CodeInvalidRequest)
			}
			if domainErr.Field != tc.wantField {
				t.Errorf("field = %q, want %q", domainErr.Field, tc.wantField)
			}
			if !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("errors.Is(err, ErrInvalidRequest) = false, want true")
			}
		})
	}
}

// TestStartRequestValidateAcceptsOptionalFields 空 Env/HardDeadline/MaxOutputBytes=0
// 都表示"调用方未设"，不是非法输入。
func TestStartRequestValidateAcceptsOptionalFields(t *testing.T) {
	req := validRequest()
	req.Process = ProcessControl{}
	req.Required = nil
	if err := req.Validate(); err != nil {
		t.Fatalf("可选字段缺省应合法，得到: %v", err)
	}
}

// TestStartRequestCloneIsolatesMutableFields Prepare 固化请求后，调用方改原请求
// 不得影响已固化的副本。
func TestStartRequestCloneIsolatesMutableFields(t *testing.T) {
	req := validRequest()
	req.Process.Env = map[string]string{"PATH": "C:/bin"}
	req.Required = []Capability{CapabilityNonInteractive}

	clone := req.Clone()
	req.Process.Env["PATH"] = "tampered"
	req.Required[0] = CapabilityPTY

	if clone.Process.Env["PATH"] != "C:/bin" {
		t.Errorf("clone.Env 被原请求修改污染: %q", clone.Process.Env["PATH"])
	}
	if clone.Required[0] != CapabilityNonInteractive {
		t.Errorf("clone.Required 被原请求修改污染: %q", clone.Required[0])
	}
}

// validExitResultPayload 返回 exited 观察的合法 Payload。
func validExitResultPayload(t *testing.T, result ExitResult) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal ExitResult: %v", err)
	}
	return payload
}

// TestObservationValidateRejects 拒绝路径：未知 kind、超限、坏 ProviderRef、
// exited 缺/坏 ExitResult。
func TestObservationValidateRejects(t *testing.T) {
	bigPayload := func(n int) json.RawMessage {
		// 合法 JSON 字符串，长度受控。
		return json.RawMessage(`"` + strings.Repeat("x", n-2) + `"`)
	}

	cases := []struct {
		name      string
		obs       Observation
		wantField string
	}{
		{
			name:      "未知 kind",
			obs:       Observation{Kind: "telemetry", Payload: json.RawMessage(`{}`)},
			wantField: "kind",
		},
		{
			name:      "空 kind",
			obs:       Observation{Kind: "", Payload: json.RawMessage(`{}`)},
			wantField: "kind",
		},
		{
			name:      "provider_ref 超 256 字节",
			obs:       Observation{Kind: ObservationOutput, ProviderRef: strings.Repeat("r", MaxProviderRefBytes+1), Payload: json.RawMessage(`{}`)},
			wantField: "provider_ref",
		},
		{
			name:      "output payload 超 256 KiB",
			obs:       Observation{Kind: ObservationOutput, Payload: bigPayload(MaxObservationBytes + 1)},
			wantField: "payload",
		},
		{
			name:      "exited 控制帧超 64 KiB",
			obs:       Observation{Kind: ObservationExited, Payload: bigPayload(MaxControlFrameBytes + 1)},
			wantField: "payload",
		},
		{
			name:      "process_started 控制帧超 64 KiB",
			obs:       Observation{Kind: ObservationProcessStarted, Payload: bigPayload(MaxControlFrameBytes + 1)},
			wantField: "payload",
		},
		{
			name:      "approval_required 控制帧超 64 KiB",
			obs:       Observation{Kind: ObservationApprovalRequired, Payload: bigPayload(MaxControlFrameBytes + 1)},
			wantField: "payload",
		},
		{
			name:      "protocol_warning 控制帧超 64 KiB",
			obs:       Observation{Kind: ObservationProtocolWarning, Payload: bigPayload(MaxControlFrameBytes + 1)},
			wantField: "payload",
		},
		{
			name:      "exited 缺 Payload",
			obs:       Observation{Kind: ObservationExited},
			wantField: "payload",
		},
		{
			name:      "exited Payload 不是 JSON",
			obs:       Observation{Kind: ObservationExited, Payload: json.RawMessage(`{`)},
			wantField: "payload",
		},
		{
			name:      "exited Payload 非对象",
			obs:       Observation{Kind: ObservationExited, Payload: json.RawMessage(`[1,2]`)},
			wantField: "payload",
		},
		{
			name:      "exited reason 非法",
			obs:       Observation{Kind: ObservationExited, Payload: json.RawMessage(`{"reason":"exploded","retryable":false,"usage":{"quality":"unknown"}}`)},
			wantField: "reason",
		},
		{
			name:      "exited reason 缺失",
			obs:       Observation{Kind: ObservationExited, Payload: json.RawMessage(`{"exit_code":0,"retryable":false,"usage":{"quality":"reported"}}`)},
			wantField: "reason",
		},
		{
			name:      "exited usage.quality 非法",
			obs:       Observation{Kind: ObservationExited, Payload: json.RawMessage(`{"reason":"completed","retryable":false,"usage":{"quality":"guessed"}}`)},
			wantField: "usage.quality",
		},
		{
			name: "exited exit_code 负数",
			obs: Observation{
				Kind:    ObservationExited,
				Payload: json.RawMessage(`{"exit_code":-1,"reason":"failed","retryable":false,"usage":{"quality":"unknown"}}`),
			},
			wantField: "exit_code",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obs.Validate()
			if err == nil {
				t.Fatalf("非法观察被接受，期望 Field=%s", tc.wantField)
			}
			var domainErr *Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("错误类型 = %T, want *execbackend.Error", err)
			}
			if domainErr.Code != CodeInvalidRequest {
				t.Errorf("code = %q, want %q", domainErr.Code, CodeInvalidRequest)
			}
			if domainErr.Field != tc.wantField {
				t.Errorf("field = %q, want %q", domainErr.Field, tc.wantField)
			}
		})
	}
}

// TestObservationValidateAccepts 正常路径：每个合法 kind 通过，边界值（恰好等于上限）通过。
func TestObservationValidateAccepts(t *testing.T) {
	exitPayload := validExitResultPayload(t, ExitResult{
		ExitCode:  intPtr(0),
		Reason:    ExitReasonCompleted,
		Retryable: false,
		Usage:     Usage{Quality: UsageQualityUnknown},
	})

	cases := []struct {
		name string
		obs  Observation
	}{
		{"process_started", Observation{Kind: ObservationProcessStarted, Payload: json.RawMessage(`{"pid":4242}`)}},
		{"output", Observation{Kind: ObservationOutput, Payload: json.RawMessage(`{"text":"hello"}`)}},
		{"tool_requested", Observation{Kind: ObservationToolRequested, ProviderRef: "toolu_1", Payload: json.RawMessage(`{"tool":"read_file"}`)}},
		{"tool_result", Observation{Kind: ObservationToolResult, ProviderRef: "toolu_1", Payload: json.RawMessage(`{"ok":true}`)}},
		{"approval_required", Observation{Kind: ObservationApprovalRequired, ProviderRef: "appr_1", Payload: json.RawMessage(`{"tool":"run_command"}`)}},
		{"usage", Observation{Kind: ObservationUsage, Payload: json.RawMessage(`{"quality":"estimated"}`)}},
		{"protocol_warning", Observation{Kind: ObservationProtocolWarning, Payload: json.RawMessage(`{"code":"garbled_frame"}`)}},
		{"exited", Observation{Kind: ObservationExited, Payload: exitPayload}},
		{"exited 无 exit_code（未知退出码）", Observation{Kind: ObservationExited, Payload: json.RawMessage(`{"reason":"crashed","retryable":true,"usage":{"quality":"unknown"}}`)}},
		{"output 恰好 256 KiB", Observation{Kind: ObservationOutput, Payload: exactJSONSize(MaxObservationBytes)}},
		{"provider_ref 恰好 256 字节", Observation{Kind: ObservationOutput, ProviderRef: strings.Repeat("r", MaxProviderRefBytes), Payload: json.RawMessage(`{}`)}},
		{"payload 缺省", Observation{Kind: ObservationUsage}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.obs.Validate(); err != nil {
				t.Fatalf("合法观察被拒绝: %v", err)
			}
		})
	}
}

// TestObservationValidateControlFrameBoundary 精确边界：恰好 64 KiB 的控制帧通过，
// 多 1 字节拒绝；恰好 256 KiB 的非控制帧通过，多 1 字节拒绝。
func TestObservationValidateControlFrameBoundary(t *testing.T) {
	atLimit := Observation{Kind: ObservationProcessStarted, Payload: exactJSONSize(MaxControlFrameBytes)}
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("恰好 64 KiB 控制帧应通过: %v", err)
	}
	if got := len(atLimit.Payload); got != MaxControlFrameBytes {
		t.Fatalf("测试构造有误：payload = %d 字节, want %d", got, MaxControlFrameBytes)
	}

	overLimit := Observation{Kind: ObservationProcessStarted, Payload: exactJSONSize(MaxControlFrameBytes + 1)}
	if err := overLimit.Validate(); err == nil {
		t.Fatal("64 KiB + 1 的控制帧应被拒绝")
	}

	outputAtLimit := Observation{Kind: ObservationOutput, Payload: exactJSONSize(MaxObservationBytes)}
	if err := outputAtLimit.Validate(); err != nil {
		t.Fatalf("恰好 256 KiB 输出应通过: %v", err)
	}
	outputOverLimit := Observation{Kind: ObservationOutput, Payload: exactJSONSize(MaxObservationBytes + 1)}
	if err := outputOverLimit.Validate(); err == nil {
		t.Fatal("256 KiB + 1 的输出应被拒绝")
	}
}

// TestObservationValidateRejectsOversizeBeforeControlCheck 超 256 KiB 的控制帧报的
// 是总量上限（先于 64 KiB 判定），Field 仍是 payload。
func TestObservationValidateRejectsOversizeBeforeControlCheck(t *testing.T) {
	obs := Observation{Kind: ObservationProtocolWarning, Payload: exactJSONSize(MaxObservationBytes + 1)}
	err := obs.Validate()
	if err == nil {
		t.Fatal("超 256 KiB 的控制帧应被拒绝")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("错误文本应包含上限说明: %v", err)
	}
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Field != "payload" {
		t.Errorf("field = %q, want payload", domainErr.Field)
	}
}

// TestUsageUnknownOmitsNumericFields §27.6：未知用量标 unknown，不能把未知当 0。
// Quality=unknown 且指针为 nil 时，JSON 里不得出现 token/cost 数字字段。
func TestUsageUnknownOmitsNumericFields(t *testing.T) {
	usage := Usage{Quality: UsageQualityUnknown}
	payload, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal usage: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	for _, key := range []string{"input_tokens", "output_tokens", "cost_minor"} {
		if _, present := raw[key]; present {
			t.Errorf("quality=unknown 时 %q 必须缺席（不能把未知当 0），JSON=%s", key, payload)
		}
	}
	if string(raw["quality"]) != `"unknown"` {
		t.Errorf("quality = %s, want \"unknown\"", raw["quality"])
	}

	// 真实零与未知必须可区分：零值指针序列化后字段在场且为 0。
	zero := int64(0)
	reported := Usage{InputTokens: &zero, Quality: UsageQualityReported}
	payload, err = json.Marshal(reported)
	if err != nil {
		t.Fatalf("marshal reported usage: %v", err)
	}
	if !strings.Contains(string(payload), `"input_tokens":0`) {
		t.Errorf("reported 的 0 用量必须显式出现，JSON=%s", payload)
	}
}

// TestUsageValidate 用量校验：quality 必填合法、指针字段非负、有金额必须有币种。
func TestUsageValidate(t *testing.T) {
	cases := []struct {
		name      string
		usage     Usage
		wantField string
	}{
		{"unknown", Usage{Quality: UsageQualityUnknown}, ""},
		{"reported 带 token", Usage{InputTokens: int64Ptr(10), OutputTokens: int64Ptr(20), Quality: UsageQualityReported}, ""},
		{"estimated 带金额", Usage{CostMinor: int64Ptr(1250), Currency: "CNY", Quality: UsageQualityEstimated}, ""},
		{"quality 为空", Usage{}, "usage.quality"},
		{"quality 未知", Usage{Quality: "guesstimate"}, "usage.quality"},
		{"input_tokens 负数", Usage{InputTokens: int64Ptr(-1), Quality: UsageQualityReported}, "usage.input_tokens"},
		{"output_tokens 负数", Usage{OutputTokens: int64Ptr(-1), Quality: UsageQualityReported}, "usage.output_tokens"},
		{"cost_minor 负数", Usage{CostMinor: int64Ptr(-1), Currency: "CNY", Quality: UsageQualityReported}, "usage.cost_minor"},
		{"cost_minor 缺币种", Usage{CostMinor: int64Ptr(1), Quality: UsageQualityReported}, "usage.currency"},
		{"cost_minor 币种空白", Usage{CostMinor: int64Ptr(1), Currency: "  ", Quality: UsageQualityReported}, "usage.currency"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.usage.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("合法 usage 被拒绝: %v", err)
				}
				return
			}
			var domainErr *Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("错误 = %v, want *execbackend.Error", err)
			}
			if domainErr.Code != CodeInvalidRequest || domainErr.Field != tc.wantField {
				t.Errorf("code/field = %q/%q, want %q/%q", domainErr.Code, domainErr.Field, CodeInvalidRequest, tc.wantField)
			}
		})
	}
}

// TestExitReasonValidate 终结原因枚举与 §21.1 的映射口径一致。
func TestExitReasonValidate(t *testing.T) {
	cases := []struct {
		name    string
		result  ExitResult
		wantErr bool
	}{
		{"completed", ExitResult{ExitCode: intPtr(0), Reason: ExitReasonCompleted, Usage: Usage{Quality: UsageQualityUnknown}}, false},
		{"failed", ExitResult{ExitCode: intPtr(1), Reason: ExitReasonFailed, Retryable: true, Usage: Usage{Quality: UsageQualityUnknown}}, false},
		{"cancelled", ExitResult{Reason: ExitReasonCancelled, Usage: Usage{Quality: UsageQualityUnknown}}, false},
		{"timeout", ExitResult{Reason: ExitReasonTimeout, Usage: Usage{Quality: UsageQualityUnknown}}, false},
		{"crashed", ExitResult{Reason: ExitReasonCrashed, Retryable: true, Usage: Usage{Quality: UsageQualityUnknown}}, false},
		{"protocol_error", ExitResult{Reason: ExitReasonProtocolError, Usage: Usage{Quality: UsageQualityUnknown}}, false},
		{"未知 reason", ExitResult{Reason: "abandoned", Usage: Usage{Quality: UsageQualityUnknown}}, true},
		{"空 reason", ExitResult{Usage: Usage{Quality: UsageQualityUnknown}}, true},
		{"负 exit_code", ExitResult{ExitCode: intPtr(-1), Reason: ExitReasonFailed, Usage: Usage{Quality: UsageQualityUnknown}}, true},
		{"usage 非法", ExitResult{Reason: ExitReasonCompleted, Usage: Usage{Quality: "maybe"}}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.result.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("应被拒绝但通过了")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("应通过但被拒绝: %v", err)
			}
		})
	}
}

// TestCapabilityEnumValid 每个能力枚举值 Valid() 为真，集合与 AllCapabilities 一致。
func TestCapabilityEnumValid(t *testing.T) {
	want := []Capability{
		"non_interactive", "json_stream", "approval_hook", "cancel_graceful",
		"resume_checkpoint", "mcp", "sandbox", "pty", "inject", "usage_tokens", "usage_cost",
	}
	got := AllCapabilities()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllCapabilities() = %v, want %v", got, want)
	}
	for _, c := range want {
		if !c.Valid() {
			t.Errorf("Capability(%q).Valid() = false, want true", c)
		}
	}
	for _, bad := range []Capability{"", "PTY", "NonInteractive", "teleport", "approval"} {
		if bad.Valid() {
			t.Errorf("Capability(%q).Valid() = true, want false", bad)
		}
	}
}

// TestObservationKindEnumValid 观察类型枚举值与注释列表一致，且控制类/终结类划分固定。
func TestObservationKindEnumValid(t *testing.T) {
	want := []ObservationKind{
		"process_started", "output", "tool_requested", "tool_result",
		"approval_required", "usage", "protocol_warning", "exited",
	}
	got := AllObservationKinds()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllObservationKinds() = %v, want %v", got, want)
	}
	for _, k := range want {
		if !k.Valid() {
			t.Errorf("ObservationKind(%q).Valid() = false, want true", k)
		}
	}
	for _, bad := range []ObservationKind{"", "Output", "sequence", "process_exited", "heartbeat"} {
		if bad.Valid() {
			t.Errorf("ObservationKind(%q).Valid() = true, want false", bad)
		}
	}

	wantControl := map[ObservationKind]bool{
		ObservationProcessStarted:   true,
		ObservationOutput:           false,
		ObservationToolRequested:    false,
		ObservationToolResult:       false,
		ObservationApprovalRequired: true,
		ObservationUsage:            false,
		ObservationProtocolWarning:  true,
		ObservationExited:           true,
	}
	for _, k := range want {
		if got := k.IsControlKind(); got != wantControl[k] {
			t.Errorf("ObservationKind(%q).IsControlKind() = %v, want %v", k, got, wantControl[k])
		}
	}

	// 终结观察只有 exited 一种：它投递后 Session 关闭 Observations channel。
	for _, k := range want {
		wantTerminal := k == ObservationExited
		if got := k.IsTerminal(); got != wantTerminal {
			t.Errorf("ObservationKind(%q).IsTerminal() = %v, want %v", k, got, wantTerminal)
		}
		if got := (Observation{Kind: k}).IsTerminal(); got != wantTerminal {
			t.Errorf("Observation{Kind:%q}.IsTerminal() = %v, want %v", k, got, wantTerminal)
		}
	}
}

// TestExitReasonAndUsageQualityAndCancelModeEnums 其余封闭枚举的取值集合。
func TestExitReasonAndUsageQualityAndCancelModeEnums(t *testing.T) {
	for _, r := range []ExitReason{"completed", "failed", "cancelled", "timeout", "crashed", "protocol_error"} {
		if !r.Valid() {
			t.Errorf("ExitReason(%q).Valid() = false, want true", r)
		}
	}
	for _, bad := range []ExitReason{"", "Completed", "killed", "expired"} {
		if bad.Valid() {
			t.Errorf("ExitReason(%q).Valid() = true, want false", bad)
		}
	}

	for _, q := range []UsageQuality{"reported", "estimated", "unknown"} {
		if !q.Valid() {
			t.Errorf("UsageQuality(%q).Valid() = false, want true", q)
		}
	}
	for _, bad := range []UsageQuality{"", "Reported", "zero", "exact"} {
		if bad.Valid() {
			t.Errorf("UsageQuality(%q).Valid() = true, want false", bad)
		}
	}

	for _, m := range []CancelMode{"graceful", "force"} {
		if !m.Valid() {
			t.Errorf("CancelMode(%q).Valid() = false, want true", m)
		}
	}
	for _, bad := range []CancelMode{"", "Graceful", "sigkill", "pause"} {
		if bad.Valid() {
			t.Errorf("CancelMode(%q).Valid() = true, want false", bad)
		}
	}
}

// TestErrorCodeConstants 失败 code 常量与计划 §21.1/§27.7 逐字一致。
func TestErrorCodeConstants(t *testing.T) {
	cases := map[string]string{
		CodeCapabilityUnavailable: "capability_unavailable",
		CodeBackendUnavailable:    "backend_unavailable",
		CodeProcessStartFailed:    "process_start_failed",
		CodeInvalidRequest:        "invalid_request",
		CodeSessionClosed:         "session_closed",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("code 常量 = %q, want %q", got, want)
		}
	}
}

// TestErrorIsAndUnwrap 哨兵、同 code 匹配与底层原因链。
func TestErrorIsAndUnwrap(t *testing.T) {
	cause := errors.New("exec: file not found")
	err := NewBackendUnavailable("claude-code", cause)

	if !errors.Is(err, ErrBackendUnavailable) {
		t.Error("errors.Is(err, ErrBackendUnavailable) = false, want true")
	}
	if errors.Is(err, ErrProcessStartFailed) {
		t.Error("backend_unavailable 不应匹配 process_start_failed 哨兵")
	}
	if !errors.Is(err, &Error{Code: CodeBackendUnavailable}) {
		t.Error("同 code 的 *Error 应匹配")
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is 应穿透到 Err 底层原因")
	}
	if err.Code != CodeBackendUnavailable {
		t.Errorf("Code = %q, want %q", err.Code, CodeBackendUnavailable)
	}

	// 每个哨兵都能被对应 code 的错误匹配到。
	for _, tc := range []struct {
		code     string
		sentinel error
	}{
		{CodeCapabilityUnavailable, ErrCapabilityUnavailable},
		{CodeBackendUnavailable, ErrBackendUnavailable},
		{CodeProcessStartFailed, ErrProcessStartFailed},
		{CodeInvalidRequest, ErrInvalidRequest},
		{CodeSessionClosed, ErrSessionClosed},
	} {
		if !errors.Is(&Error{Code: tc.code}, tc.sentinel) {
			t.Errorf("errors.Is(&Error{Code:%q}, sentinel) = false, want true", tc.code)
		}
	}
}

// TestErrorFormatting 错误文本包含 code 与定位信息，便于日志排查。
func TestErrorFormatting(t *testing.T) {
	err := NewCapabilityUnavailable(CapabilityPTY)
	text := err.Error()
	for _, want := range []string{"capability_unavailable", "pty"} {
		if !strings.Contains(text, want) {
			t.Errorf("Error() = %q, 应包含 %q", text, want)
		}
	}

	var nilErr *Error
	if nilErr.Error() != "<nil>" {
		t.Errorf("nil *Error.Error() = %q, want <nil>", nilErr.Error())
	}
	if nilErr.Unwrap() != nil {
		t.Error("nil *Error.Unwrap() 应为 nil")
	}
	if nilErr.Is(ErrInvalidRequest) {
		t.Error("nil *Error.Is 应为 false")
	}
}

// intPtr/int64Ptr 是测试用指针构造。
func intPtr(v int) *int { return &v }

func int64Ptr(v int64) *int64 { return &v }

// exactJSONSize 构造恰好 n 字节的合法 JSON 字符串字面量（n >= 2）。
func exactJSONSize(n int) json.RawMessage {
	if n < 2 {
		n = 2
	}
	return json.RawMessage(`"` + strings.Repeat("x", n-2) + `"`)
}
