package execbackend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// 本文件是 T1.07.b 第 1 组在 execbackend 侧的端口测试（§15 T1.07、§28 T1.07.b）。
// 它不依赖 hooks / run hooks：端口语义全部由本包内的假实现验证：
//   - before 拒绝 → execute 一次都不执行、结果 denied、errors.Is(ErrHookDenied)；
//   - 端口缺失 → before 拒绝（失败即关闭），after 只警告；
//   - after 失败 → 警告恰好一次、execute 恰好一次、返回值不变（不重跑工具）；
//   - 指纹：键序/空白等价、参数不同则不同、非法 JSON 报错；
//   - RunStart 拒绝 → start 回调 0 次；RunFinish 失败只警告。

// fakeHookPort 是 HookPort 的测试假实现：记录调用次数与最后一次请求，
// before 类按 err 拒绝，after 类按 err 报错。
type fakeHookPort struct {
	mu sync.Mutex

	beforeToolErr    error
	afterToolErr     error
	beforeRunErr     error
	afterRunErr      error
	beforeToolCalls  int
	afterToolCalls   int
	beforeRunCalls   int
	afterRunCalls    int
	lastToolRequest  ToolCallRequest
	lastToolResult   ToolCallResult
	lastRunRequest   RunLifecycleRequest
	lastAfterRunArgs RunLifecycleRequest
}

func (p *fakeHookPort) BeforeTool(_ context.Context, req ToolCallRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.beforeToolCalls++
	p.lastToolRequest = req
	return p.beforeToolErr
}

func (p *fakeHookPort) AfterTool(_ context.Context, req ToolCallRequest, result ToolCallResult) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterToolCalls++
	p.lastToolRequest = req
	p.lastToolResult = result
	return p.afterToolErr
}

func (p *fakeHookPort) BeforeRunStart(_ context.Context, req RunLifecycleRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.beforeRunCalls++
	p.lastRunRequest = req
	return p.beforeRunErr
}

func (p *fakeHookPort) AfterRunFinish(_ context.Context, req RunLifecycleRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterRunCalls++
	p.lastAfterRunArgs = req
	return p.afterRunErr
}

func (p *fakeHookPort) counts() (beforeTool, afterTool, beforeRun, afterRun int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.beforeToolCalls, p.afterToolCalls, p.beforeRunCalls, p.afterRunCalls
}

// warnRecorder 记录警告，供"恰好一次"断言使用。
type warnRecorder struct {
	mu       sync.Mutex
	warnings []HookWarning
}

func (r *warnRecorder) sink(_ context.Context, w HookWarning) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, w)
}

func (r *warnRecorder) all() []HookWarning {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]HookWarning(nil), r.warnings...)
}

// hookTestRef 是合法的执行身份（四个字段都不透明字符串）。
func hookTestRef() RunRef {
	return RunRef{ProjectID: "project-1", RunID: "run-1", AttemptID: "attempt-1", AgentRevisionID: "revision-1"}
}

// mustToolRequest 构造一个合法工具请求。
func mustToolRequest(t *testing.T, eventID string, arguments string) ToolCallRequest {
	t.Helper()
	req, err := NewToolCallRequest(hookTestRef(), "claude_code", eventID, "toolu_1", "read_file", json.RawMessage(arguments))
	if err != nil {
		t.Fatalf("NewToolCallRequest: %v", err)
	}
	return req
}

// mustRunRequest 构造一个合法的 RunStart 请求。
func mustRunRequest(t *testing.T, eventID, status, previous string) RunLifecycleRequest {
	t.Helper()
	req, err := NewRunLifecycleRequest(hookTestRef(), "claude_code", eventID, status, previous)
	if err != nil {
		t.Fatalf("NewRunLifecycleRequest: %v", err)
	}
	return req
}

func TestToolRequestFingerprintIsStableAcrossEquivalentJSON(t *testing.T) {
	cases := [][2]string{
		{`{"path":"src/x.go","line":3}`, `{"line":3,"path":"src/x.go"}`},
		{`{"a": {"b": [1,2,3]}, "c": true}`, "{\n  \"c\": true,\n  \"a\": {\"b\": [1, 2, 3]}\n}"},
		{`{"n":1}`, `{"n":1.0}`},
		{`{"n":1e2}`, `{"n":100}`},
		{`{"s":"abc"}`, `{"s":"abc"}`},
		{`[{"b":2,"a":1}]`, `[ { "a" : 1 , "b" : 2 } ]`},
		{`{"n":1.50}`, `{"n":1.5}`},
		{`{"n":-0}`, `{"n":0.000e7}`},
		{`{"n":10e-1}`, `{"n":1}`},
		{`{"n":0.001}`, `{"n":1E-3}`},
		{`{"n":123456789012345678901234567890}`, `{"n":1.2345678901234567890123456789e29}`},
		{`{"s":"A"}`, `{"s":"A"}`},
	}
	for i, pair := range cases {
		left, err := ToolRequestFingerprint("read_file", json.RawMessage(pair[0]))
		if err != nil {
			t.Fatalf("case %d: left fingerprint: %v", i, err)
		}
		right, err := ToolRequestFingerprint("read_file", json.RawMessage(pair[1]))
		if err != nil {
			t.Fatalf("case %d: right fingerprint: %v", i, err)
		}
		if left != right {
			t.Errorf("case %d: equivalent JSON produced different fingerprints:\n  %s\n  %s\n  %s vs %s", i, pair[0], pair[1], left, right)
		}
		if !strings.HasPrefix(left, "sha256:") || len(left) != len("sha256:")+64 {
			t.Errorf("case %d: fingerprint %q is not \"sha256:\" + 64 hex characters", i, left)
		}
		if left != strings.ToLower(left) {
			t.Errorf("case %d: fingerprint %q is not lowercase", i, left)
		}
	}
}

func TestToolRequestFingerprintChangesWithContent(t *testing.T) {
	base, err := ToolRequestFingerprint("read_file", json.RawMessage(`{"path":"a"}`))
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	otherTool, err := ToolRequestFingerprint("write_file", json.RawMessage(`{"path":"a"}`))
	if err != nil {
		t.Fatalf("other tool: %v", err)
	}
	otherArgs, err := ToolRequestFingerprint("read_file", json.RawMessage(`{"path":"b"}`))
	if err != nil {
		t.Fatalf("other args: %v", err)
	}
	if base == otherTool {
		t.Error("changing the tool must change the fingerprint")
	}
	if base == otherArgs {
		t.Error("changing the arguments must change the fingerprint")
	}
}

func TestToolRequestFingerprintRejectsMalformedJSON(t *testing.T) {
	for _, raw := range []string{``, `   `, `{"path":`, `not json`, `{"a":1,}`} {
		fingerprint, err := ToolRequestFingerprint("read_file", json.RawMessage(raw))
		if err == nil {
			t.Fatalf("malformed arguments %q accepted with fingerprint %q", raw, fingerprint)
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("error for %q is not an *Error: %v", raw, err)
		}
		if apiErr.Code != CodeInvalidRequest || apiErr.Field != "arguments" {
			t.Errorf("error for %q = code %s field %s, want invalid_request/arguments", raw, apiErr.Code, apiErr.Field)
		}
		if strings.Contains(err.Error(), raw) && strings.TrimSpace(raw) != "" {
			t.Errorf("error text leaks the arguments content: %v", err)
		}
	}
}

// TestToolRequestFingerprintHasNoCollisions 指纹以后要被审批绑定：两个含义不同的
// 请求绝不能算出同一个指纹。数字按十进制值精确归一、不经过 float64，所以超过 2^53
// 的整数、超过 17 位有效数字的小数、下溢到 0 的数都保持可区分。
func TestToolRequestFingerprintHasNoCollisions(t *testing.T) {
	cases := [][2]string{
		{`{"n":9007199254740993.0}`, `{"n":9007199254740992}`},
		{`{"n":18446744073709551616}`, `{"n":18446744073709551617}`},
		{`{"n":1000000000000000001e0}`, `{"n":1000000000000000000}`},
		{`{"n":0.1000000000000000000001}`, `{"n":0.1}`},
		{`{"n":1e-400}`, `{"n":0}`},
		{`{"n":-1}`, `{"n":1}`},
	}
	for i, pair := range cases {
		left, err := ToolRequestFingerprint("run_shell", json.RawMessage(pair[0]))
		if err != nil {
			t.Fatalf("case %d: left fingerprint for %s: %v", i, pair[0], err)
		}
		right, err := ToolRequestFingerprint("run_shell", json.RawMessage(pair[1]))
		if err != nil {
			t.Fatalf("case %d: right fingerprint for %s: %v", i, pair[1], err)
		}
		if left == right {
			t.Errorf("case %d: different requests share a fingerprint:\n  %s\n  %s\n  both %s", i, pair[0], pair[1], left)
		}
	}
}

// TestToolRequestFingerprintRejectsAmbiguousArguments 含义取决于解析器的参数无法
// 指纹：重复的键（encoding/json 取最后一个，别的解析器可能取第一个），以及非法
// UTF-8 与落单的 UTF-16 代理项转义（两者都被解成 U+FFFD，不同的字节会撞成同一个
// 字符串）。它们一律被拒绝（失败即关闭）；外形相近但没有歧义的输入照常接受。
func TestToolRequestFingerprintRejectsAmbiguousArguments(t *testing.T) {
	for _, raw := range []string{
		`{"cmd":"rm -rf ~","cmd":"ls"}`,
		`{"a":{"x":1,"x":2}}`,
		`[{"k":1,"k":1}]`,
		"{\"p\":\"\xff\"}",
		`{"p":"\ud800"}`,
		`{"p":"\udc00x"}`,
		`{"p":"\ud800A"}`,
		`{"\ud800":1}`,
		`{"n":1e99999999999}`,
	} {
		fingerprint, err := ToolRequestFingerprint("run_shell", json.RawMessage(raw))
		if err == nil {
			t.Errorf("ambiguous arguments %q accepted with fingerprint %s", raw, fingerprint)
			continue
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidRequest || apiErr.Field != "arguments" {
			t.Errorf("error for %q = %v, want invalid_request on arguments", raw, err)
		}
		if strings.Contains(err.Error(), "rm -rf") {
			t.Errorf("error text leaks the arguments content: %v", err)
		}
	}
	for _, raw := range []string{
		`[{"k":1},{"k":2}]`,    // the same key in two different objects
		`{"a":1,"A":2}`,        // keys are case-sensitive
		`{"a":{"a":{"a":1}}}`,  // the same key at different depths
		`{"p":"\ud83d\ude00"}`, // a paired surrogate escape
		`{"p":"\\ud800"}`,      // an escaped backslash followed by plain text
		`{"p":"�"}`,            // U+FFFD itself is not ambiguous
		`{"p":"\ufffd"}`,       // ... nor is its escape
	} {
		if _, err := ToolRequestFingerprint("run_shell", json.RawMessage(raw)); err != nil {
			t.Errorf("unambiguous arguments %q rejected: %v", raw, err)
		}
	}
}

func TestNewToolCallRequestValidatesAndFingerprints(t *testing.T) {
	req := mustToolRequest(t, "event-1", `{"path":"a"}`)
	want, err := ToolRequestFingerprint("read_file", json.RawMessage(`{"path":"a"}`))
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if req.Fingerprint != want {
		t.Errorf("Fingerprint = %q, want %q", req.Fingerprint, want)
	}
	if err := req.Validate(); err != nil {
		t.Errorf("constructed request rejected: %v", err)
	}

	// 手写一个指纹不匹配的请求：必须被拒绝（调用方不能自己编指纹）。
	forged := req
	forged.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	var apiErr *Error
	if err := forged.Validate(); !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidRequest || apiErr.Field != "fingerprint" {
		t.Errorf("forged fingerprint: err = %v, want invalid_request/fingerprint", err)
	}

	// 缺字段逐个拒绝。
	for _, tc := range []struct {
		name   string
		mutate func(*ToolCallRequest)
		field  string
	}{
		{"no project", func(r *ToolCallRequest) { r.Run.ProjectID = "" }, "run.project_id"},
		{"no run", func(r *ToolCallRequest) { r.Run.RunID = "" }, "run.run_id"},
		{"no attempt", func(r *ToolCallRequest) { r.Run.AttemptID = "" }, "run.attempt_id"},
		{"no revision", func(r *ToolCallRequest) { r.Run.AgentRevisionID = "" }, "run.agent_revision_id"},
		{"no backend", func(r *ToolCallRequest) { r.Backend = "" }, "backend"},
		{"no event", func(r *ToolCallRequest) { r.EventID = "" }, "event_id"},
		{"no tool call id", func(r *ToolCallRequest) { r.ToolCallID = "" }, "tool_call_id"},
		{"no tool", func(r *ToolCallRequest) { r.Tool = "" }, "tool"},
		{"oversized arguments", func(r *ToolCallRequest) {
			r.Arguments = json.RawMessage(`"` + strings.Repeat("x", MaxControlFrameBytes) + `"`)
			r.Fingerprint, _ = ToolRequestFingerprint(r.Tool, r.Arguments)
		}, "arguments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broken := req
			tc.mutate(&broken)
			err := broken.Validate()
			if !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidRequest {
				t.Fatalf("Validate() = %v, want an invalid_request *Error", err)
			}
			if apiErr.Field != tc.field {
				t.Errorf("Validate() field = %q, want %q (%v)", apiErr.Field, tc.field, err)
			}
		})
	}
}

func TestNewRunLifecycleRequestValidates(t *testing.T) {
	req := mustRunRequest(t, "event-1", "starting", "")
	if req.PreviousStatus != "" || req.Status != "starting" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if _, err := NewRunLifecycleRequest(hookTestRef(), "claude_code", "event-1", "starting", "starting"); err == nil {
		t.Error("previous_status == status must be rejected (a transition moves between states)")
	}
	if _, err := NewRunLifecycleRequest(hookTestRef(), "claude_code", "", "starting", ""); err == nil {
		t.Error("empty event_id must be rejected")
	}
	if _, err := NewRunLifecycleRequest(hookTestRef(), "claude_code", "event-1", "", ""); err == nil {
		t.Error("empty status must be rejected")
	}
}

// TestBeforeHookDenyPreventsProcessPortLevel 是 T1.07.c 点名测试在端口层的等价物：
// before 拒绝时 execute 一次都不执行。
func TestBeforeHookDenyPreventsProcessPortLevel(t *testing.T) {
	denied := errors.New("policy says no")
	port := &fakeHookPort{beforeToolErr: denied}
	warn := &warnRecorder{}
	req := mustToolRequest(t, "event-1", `{"path":"a"}`)
	executions := 0

	result, err := RunTool(context.Background(), port, warn.sink, req, func(context.Context) (ToolCallResult, error) {
		executions++
		return ToolCallResult{Status: ToolResultStatusOK}, nil
	})

	if executions != 0 {
		t.Fatalf("execute ran %d times after a before-hook denial, want 0", executions)
	}
	if result.Status != ToolResultStatusDenied {
		t.Errorf("result status = %q, want %q", result.Status, ToolResultStatusDenied)
	}
	if !errors.Is(err, ErrHookDenied) {
		t.Fatalf("error %v does not wrap ErrHookDenied", err)
	}
	if !errors.Is(err, denied) {
		t.Errorf("error %v does not unwrap to the hook failure", err)
	}
	if before, after, _, _ := port.counts(); before != 1 || after != 0 {
		t.Errorf("hook calls before=%d after=%d, want 1/0 (a denied call must not report a result)", before, after)
	}
	if got := warn.all(); len(got) != 0 {
		t.Errorf("a denial must not emit a warning, got %+v", got)
	}
}

// TestHookPortMissingIsFailClosed 端口缺失（接线漏了）必须拒绝，而不是放行。
func TestHookPortMissingIsFailClosed(t *testing.T) {
	warn := &warnRecorder{}
	req := mustToolRequest(t, "event-1", `{"path":"a"}`)
	executions := 0
	result, err := RunTool(context.Background(), nil, warn.sink, req, func(context.Context) (ToolCallResult, error) {
		executions++
		return ToolCallResult{Status: ToolResultStatusOK}, nil
	})
	if executions != 0 {
		t.Fatalf("execute ran %d times with a nil port, want 0", executions)
	}
	if result.Status != ToolResultStatusDenied || !errors.Is(err, ErrHookDenied) || !errors.Is(err, ErrHookPortMissing) {
		t.Fatalf("nil port = (%q, %v), want denied + ErrHookDenied + ErrHookPortMissing", result.Status, err)
	}

	// after 类没有端口时只警告，不返回错误。
	ReportToolResult(context.Background(), nil, warn.sink, req, ToolCallResult{Status: ToolResultStatusOK})
	ReportRunFinish(context.Background(), nil, warn.sink, mustRunRequest(t, "event-2", "completed", "running"))
	if got := warn.all(); len(got) != 2 {
		t.Fatalf("warnings = %+v, want exactly 2 (one per report on a nil port)", got)
	}
	if got := warn.all()[0]; got.Hook != HookPointPostToolUse || !errors.Is(got.Err, ErrHookPortMissing) {
		t.Errorf("tool warning = %+v, want post-tool/ErrHookPortMissing", got)
	}
	if got := warn.all()[1]; got.Hook != HookPointRunFinish || !errors.Is(got.Err, ErrHookPortMissing) {
		t.Errorf("run warning = %+v, want run-finish/ErrHookPortMissing", got)
	}
}

// TestAfterHookFailureDoesNotReplayToolPortLevel 是 T1.07.c 点名测试在端口层的
// 等价物：after 失败只产生一条警告，execute 仍然只调用一次，返回值不变。
func TestAfterHookFailureDoesNotReplayToolPortLevel(t *testing.T) {
	postErr := errors.New("post hook exploded")
	port := &fakeHookPort{afterToolErr: postErr}
	warn := &warnRecorder{}
	req := mustToolRequest(t, "event-1", `{"path":"a"}`)
	exit := 3
	executions := 0

	result, err := RunTool(context.Background(), port, warn.sink, req, func(context.Context) (ToolCallResult, error) {
		executions++
		return ToolCallResult{Status: ToolResultStatusError, ExitCode: &exit, OutputRef: "spool/7", Truncated: true}, nil
	})

	if err != nil {
		t.Fatalf("RunTool returned %v; the tool's own error is nil and after-hook failures must not become the caller's error", err)
	}
	if executions != 1 {
		t.Fatalf("execute ran %d times, want exactly 1 (after failure must never replay the tool)", executions)
	}
	if result.Status != ToolResultStatusError || result.ExitCode == nil || *result.ExitCode != 3 || result.OutputRef != "spool/7" || !result.Truncated {
		t.Errorf("result = %+v, want the execute result unchanged", result)
	}
	warnings := warn.all()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want exactly 1", warnings)
	}
	if warnings[0].Hook != HookPointPostToolUse || warnings[0].EventID != "event-1" || !errors.Is(warnings[0].Err, postErr) {
		t.Errorf("warning = %+v, want post-tool/event-1/underlying error", warnings[0])
	}
	if before, after, _, _ := port.counts(); before != 1 || after != 1 {
		t.Errorf("hook calls before=%d after=%d, want 1/1", before, after)
	}
	if got := port.lastToolResult; got.Status != ToolResultStatusError || got.OutputRef != "spool/7" {
		t.Errorf("hook saw result %+v, want the sanitized summary", got)
	}
}

// TestRunToolFillsStatusOnExecuteError 只补状态，不改返回值。
func TestRunToolFillsStatusOnExecuteError(t *testing.T) {
	port := &fakeHookPort{}
	warn := &warnRecorder{}
	req := mustToolRequest(t, "event-1", `{"path":"a"}`)
	execErr := errors.New("tool failed")

	result, err := RunTool(context.Background(), port, warn.sink, req, func(context.Context) (ToolCallResult, error) {
		return ToolCallResult{}, execErr
	})
	if !errors.Is(err, execErr) {
		t.Fatalf("RunTool error = %v, want the execute error", err)
	}
	if result.Status != ToolResultStatusError {
		t.Errorf("result status = %q, want %q", result.Status, ToolResultStatusError)
	}
	if got := port.lastToolResult; got.Status != ToolResultStatusError {
		t.Errorf("post hook saw %+v, want a status-filled result", got)
	}
}

// TestWarnSinkNilDoesNotPanic 没有 sink 时写一行日志，不 panic、不改变流程。
func TestWarnSinkNilDoesNotPanic(t *testing.T) {
	port := &fakeHookPort{afterToolErr: errors.New("boom")}
	req := mustToolRequest(t, "event-1", `{"path":"a"}`)
	result, err := RunTool(context.Background(), port, nil, req, func(context.Context) (ToolCallResult, error) {
		return ToolCallResult{Status: ToolResultStatusOK}, nil
	})
	if err != nil || result.Status != ToolResultStatusOK {
		t.Fatalf("RunTool = (%+v, %v), want the execute result and no error", result, err)
	}
}

// TestInvalidRequestIsRejectedBeforeThePort 不合法的请求在触达端口之前被拒绝。
func TestInvalidRequestIsRejectedBeforeThePort(t *testing.T) {
	port := &fakeHookPort{}
	broken := mustToolRequest(t, "event-1", `{"path":"a"}`)
	broken.ToolCallID = ""
	executions := 0
	result, err := RunTool(context.Background(), port, nil, broken, func(context.Context) (ToolCallResult, error) {
		executions++
		return ToolCallResult{Status: ToolResultStatusOK}, nil
	})
	if executions != 0 {
		t.Fatalf("execute ran %d times for an invalid request, want 0", executions)
	}
	if result.Status != ToolResultStatusDenied {
		t.Errorf("result status = %q, want %q", result.Status, ToolResultStatusDenied)
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error %v does not wrap ErrInvalidRequest", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Field != "tool_call_id" {
		t.Errorf("error = %v, want an invalid_request *Error naming tool_call_id", err)
	}
	if before, after, _, _ := port.counts(); before != 0 || after != 0 {
		t.Errorf("the port was called for an invalid request: before=%d after=%d", before, after)
	}
}

// TestRunStartDenyPreventsProcess 与 TestBeforeHookDenyPreventsProcessPortLevel 对称。
func TestRunStartDenyPreventsProcess(t *testing.T) {
	denied := errors.New("no run start")
	port := &fakeHookPort{beforeRunErr: denied}
	req := mustRunRequest(t, "event-1", "starting", "queued")
	starts := 0

	err := AuthorizeRunStart(context.Background(), port, req)
	start := func() {
		starts++
	}
	if err == nil {
		start()
	}
	if starts != 0 {
		t.Fatalf("the process start callback ran %d times after a run-start denial, want 0", starts)
	}
	if !errors.Is(err, ErrHookDenied) || !errors.Is(err, denied) {
		t.Fatalf("error = %v, want ErrHookDenied wrapping the hook failure", err)
	}
	if _, _, beforeRun, _ := port.counts(); beforeRun != 1 {
		t.Errorf("BeforeRunStart called %d times, want 1", beforeRun)
	}
	hookErr := &HookDeniedError{}
	if !errors.As(err, &hookErr) || hookErr.Hook != HookPointRunStart {
		t.Errorf("error = %v, want a *HookDeniedError for %s", err, HookPointRunStart)
	}
}

// TestRunFinishFailureOnlyWarns Run 终态不因 hook 失败改变。
func TestRunFinishFailureOnlyWarns(t *testing.T) {
	port := &fakeHookPort{afterRunErr: errors.New("finish hook failed")}
	warn := &warnRecorder{}
	req := mustRunRequest(t, "event-9", "completed", "running")

	ReportRunFinish(context.Background(), port, warn.sink, req)
	ReportRunFinish(context.Background(), port, warn.sink, req)

	warnings := warn.all()
	if len(warnings) != 2 {
		t.Fatalf("warnings = %+v, want 2 (one per report)", warnings)
	}
	for _, warning := range warnings {
		if warning.Hook != HookPointRunFinish || warning.EventID != "event-9" {
			t.Errorf("warning = %+v, want run-finish/event-9", warning)
		}
	}
	if _, _, _, after := port.counts(); after != 2 {
		t.Errorf("AfterRunFinish called %d times, want 2", after)
	}
}

// TestRunFinishWithoutAttemptIsReported 排队阶段就被取消（或到期）的 Run 没有
// attempt：payload 契约对 RunFinish 只要求 Run 级身份（CA-1），所以它照常上报；
// RunStart 在认领之后才触发，attempt 与 agent 修订缺一不可，缺了就拒绝且不触达端口。
func TestRunFinishWithoutAttemptIsReported(t *testing.T) {
	ref := hookTestRef()
	ref.AttemptID = ""
	finish, err := NewRunLifecycleRequest(ref, "claude_code", "cancelled-1", "cancelled", "queued")
	if err != nil {
		t.Fatalf("NewRunLifecycleRequest for a Run cancelled in the queue: %v", err)
	}
	port := &fakeHookPort{}
	warn := &warnRecorder{}
	ReportRunFinish(context.Background(), port, warn.sink, finish)
	if _, _, _, after := port.counts(); after != 1 {
		t.Fatalf("AfterRunFinish called %d times for a Run without an attempt, want 1", after)
	}
	if warnings := warn.all(); len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}

	for name, mutate := range map[string]func(*RunRef){
		"attempt_id":        func(r *RunRef) { r.AttemptID = "" },
		"agent_revision_id": func(r *RunRef) { r.AgentRevisionID = "" },
	} {
		startRef := hookTestRef()
		mutate(&startRef)
		start, err := NewRunLifecycleRequest(startRef, "claude_code", "claimed-"+name, "starting", "queued")
		if err != nil {
			t.Fatalf("NewRunLifecycleRequest without %s: %v", name, err)
		}
		err = AuthorizeRunStart(context.Background(), port, start)
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidRequest || apiErr.Field != "run."+name {
			t.Fatalf("AuthorizeRunStart without %s = %v, want invalid_request on run.%s", name, err, name)
		}
	}
	if _, _, before, _ := port.counts(); before != 0 {
		t.Fatalf("BeforeRunStart reached %d times without a claimed identity, want 0", before)
	}

	// project_id 与 run_id 对两种生命周期 hook 都是必需的。
	for _, mutate := range []func(*RunRef){
		func(r *RunRef) { r.ProjectID = "" },
		func(r *RunRef) { r.RunID = "" },
	} {
		bad := hookTestRef()
		mutate(&bad)
		if _, err := NewRunLifecycleRequest(bad, "claude_code", "event-x", "cancelled", "queued"); err == nil {
			t.Errorf("NewRunLifecycleRequest accepted %+v without a project or run id", bad)
		}
	}
}

// TestAuthorizeToolCallForwardsFingerprint 端口看到的指纹与请求一致。
func TestAuthorizeToolCallForwardsFingerprint(t *testing.T) {
	port := &fakeHookPort{}
	req := mustToolRequest(t, "event-1", `{"path":"a","line":3}`)
	if err := AuthorizeToolCall(context.Background(), port, req); err != nil {
		t.Fatalf("AuthorizeToolCall: %v", err)
	}
	want, err := ToolRequestFingerprint("read_file", json.RawMessage(`{"line":3,"path":"a"}`))
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if got := port.lastToolRequest.Fingerprint; got != want {
		t.Errorf("port saw fingerprint %q, want %q", got, want)
	}
	if got := port.lastToolRequest.EventID; got != "event-1" {
		t.Errorf("port saw event %q, want event-1", got)
	}
}
