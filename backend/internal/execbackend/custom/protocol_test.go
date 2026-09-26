package custom

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件是 T4.03.a 的无状态协议测试（§15 T4.03、§28 T4.03.a）：
//   - DecodeBackendFrame/DecodeHostFrame：长度、JSON 形状、信封、保留字段、未知字段。
//   - EncodeBackendFrame/EncodeHostFrame：信封写入、末尾 "\n"、长度上限。
//   - Negotiate：协议版本、未知必需能力、required 未声明、重复/非法能力。
//   - Observation/ExitResult 映射与 execbackend 校验一致。
//
// 所有用例都不启动任何进程、不联网。

// sampleHash 是合法的 snapshot_hash（"sha256:" + 64 位小写 hex）。
const sampleHash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// sampleWorkDir 是合法的 work_dir：绝对且已按宿主平台 Clean。
// execbackend.StartRequest.Validate 用 filepath.Clean 校验，Windows 上 "D:/a/b" 的
// Clean 结果是 "D:\\a\\b"，因此夹具必须用平台原生路径而不是手写正斜杠。
var sampleWorkDir = filepath.Clean(filepath.Join(os.TempDir(), "codeflow-custom-r31", "ws", "run_88"))

// jsonString 把宿主路径编码为 JSON 字符串字面量（Windows 反斜杠必须转义）。
func jsonString(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// validHelloLine 是最小 hello 帧。
const validHelloLine = `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"demo","version":"0.1.0"}}`

// decodeErr 断言 err 是 *ProtocolError 且 code 等于 want，返回该错误。
func decodeErr(t *testing.T, err error, want string) *ProtocolError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", want)
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
	if pe.Code != want {
		t.Fatalf("error code = %q (field=%q msg=%q), want %q", pe.Code, pe.Field, pe.Message, want)
	}
	return pe
}

// mustEncodeHost 编码一帧 host 帧，失败即 fatal。
func mustEncodeHost(t *testing.T, f HostFrame) []byte {
	t.Helper()
	line, err := EncodeHostFrame(f)
	if err != nil {
		t.Fatalf("EncodeHostFrame(%s) error: %v", f.FrameType(), err)
	}
	return line
}

// sampleStartFrame 返回一个合法的 start 帧。
func sampleStartFrame() StartFrame {
	deadline := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	return StartFrame{
		Identity: Identity{
			ProjectID:       "p_123",
			RunID:           "run_88",
			AttemptID:       "att_1",
			AgentRevisionID: "ar_12",
			Actor:           IdentityActor{Type: ActorAgent, ID: "agent_claude_bugfix"},
		},
		Input: execbackend.FrozenInput{
			SnapshotID:   "snap_1",
			SnapshotHash: sampleHash,
			Prompt:       "fix the failing test",
		},
		WorkDir: sampleWorkDir,
		Process: ProcessSpec{GracePeriodMS: 5000, HardDeadline: &deadline, MaxOutputBytes: 262144},
		Required: []execbackend.Capability{
			execbackend.CapabilityNonInteractive,
			execbackend.CapabilityJSONStream,
		},
	}
}

// ---------------------------------------------------------------------------
// 长度上限
// ---------------------------------------------------------------------------

// TestDecodeBackendFrameFrameTooLarge 覆盖整行超过 MaxFrameBytes 的拒绝。
func TestDecodeBackendFrameFrameTooLarge(t *testing.T) {
	line := make([]byte, MaxFrameBytes+1)
	for i := range line {
		line[i] = 'x'
	}
	pe := decodeErr(t, mustDecodeBackendErr(t, line), CodeFrameTooLarge)
	if !strings.Contains(pe.Message, "limit") {
		t.Errorf("message %q should state the limit", pe.Message)
	}
}

// TestDecodeBackendFrameControlFrameTooLarge 覆盖控制帧（hello/exit）超过 64 KiB 的拒绝：
// 行仍小于 MaxFrameBytes，但超过控制帧上限。
func TestDecodeBackendFrameControlFrameTooLarge(t *testing.T) {
	pad := strings.Repeat("x", MaxControlFrameBytes)
	hello := `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"` + pad + `"}}`
	if len(hello) <= MaxControlFrameBytes || len(hello) > MaxFrameBytes {
		t.Fatalf("test frame length %d not in (control=%d, max=%d)", len(hello), MaxControlFrameBytes, MaxFrameBytes)
	}
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(hello)), CodeFrameTooLarge)
	if !strings.Contains(pe.Message, "control frame") {
		t.Errorf("message %q should name the control-frame limit", pe.Message)
	}
}

// TestDecodeBackendFrameControlKindUsesControlLimit 覆盖 observation 帧按 kind 选上限：
// 控制类 kind（process_started）超 64 KiB 拒绝，非控制类 kind（output）同样长度仍被接受
// （直到 payload 自己的 256 KiB 上限）。
func TestDecodeBackendFrameControlKindUsesControlLimit(t *testing.T) {
	pad := strings.Repeat("x", MaxControlFrameBytes)
	payload := `"` + pad + `"`
	control := `{"schema_version":1,"type":"observation","kind":"process_started","observed_at":"2026-09-26T08:00:00Z","payload":` + payload + `}`
	output := `{"schema_version":1,"type":"observation","kind":"output","observed_at":"2026-09-26T08:00:00Z","payload":` + payload + `}`
	if len(control) <= MaxControlFrameBytes || len(output) <= MaxControlFrameBytes {
		t.Fatalf("test frames must exceed the control limit")
	}
	decodeErr(t, mustDecodeBackendErr(t, []byte(control)), CodeFrameTooLarge)

	frame, err := DecodeBackendFrame([]byte(output))
	if err != nil {
		t.Fatalf("output frame of %d bytes should be accepted, got %v", len(output), err)
	}
	obs, ok := frame.(ObservationFrame)
	if !ok {
		t.Fatalf("decoded frame is %T, want ObservationFrame", frame)
	}
	if len(obs.Payload) != len(payload) {
		t.Errorf("payload = %d bytes, want %d (payload must be preserved verbatim)", len(obs.Payload), len(payload))
	}
}

// TestEncodeBackendFrameTooLarge 覆盖编码侧的超长拒绝（T4.03.c 据此终止进程）。
//
// 非控制类帧：payload 上限（256 KiB）比帧上限（256 KiB + 16 KiB）先命中，因此
// 合法 payload 永远填不满帧上限，编码侧报的是 payload 超限。
// 控制类帧：整行 64 KiB 上限先命中——payload 本身仍在 64 KiB 之内，但加上信封就超了。
func TestEncodeBackendFrameTooLarge(t *testing.T) {
	frame := ObservationFrame{
		Kind:       execbackend.ObservationOutput,
		ObservedAt: time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC),
		Payload:    json.RawMessage(`"` + strings.Repeat("x", execbackend.MaxObservationBytes+1) + `"`),
	}
	pe := decodeErr(t, encodeBackendErr(t, frame), CodeInvalidFrame)
	if pe.Field != "payload" {
		t.Errorf("field = %q, want payload", pe.Field)
	}

	control := ObservationFrame{
		Kind:       execbackend.ObservationProtocolWarning,
		ObservedAt: time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC),
		Payload:    json.RawMessage(`"` + strings.Repeat("x", MaxControlFrameBytes-64) + `"`),
	}
	decodeErr(t, encodeBackendErr(t, control), CodeFrameTooLarge)
}

// encodeBackendErr 编码一帧并返回错误（nil 时 fatal）。
func encodeBackendErr(t *testing.T, frame BackendFrame) error {
	t.Helper()
	line, err := EncodeBackendFrame(frame)
	if err == nil {
		t.Fatalf("expected encode error, got %d bytes", len(line))
	}
	return err
}

// mustDecodeBackendErr 解码一行并返回错误（nil 时 fatal）。
func mustDecodeBackendErr(t *testing.T, line []byte) error {
	t.Helper()
	frame, err := DecodeBackendFrame(line)
	if err == nil {
		t.Fatalf("expected decode error, got frame %T", frame)
	}
	return err
}

// ---------------------------------------------------------------------------
// 信封：schema_version / type / JSON 形状
// ---------------------------------------------------------------------------

// TestDecodeBackendFrameSchemaVersion 覆盖 schema_version 缺失/不等于 1。
func TestDecodeBackendFrameSchemaVersion(t *testing.T) {
	cases := []struct {
		name string
		line string
		code string
	}{
		{"missing", `{"type":"hello","protocol_version":1,"backend":{"name":"demo"}}`, CodeInvalidFrame},
		{"wrong-number", `{"schema_version":2,"type":"hello","protocol_version":1,"backend":{"name":"demo"}}`, CodeSchemaVersionMismatch},
		{"wrong-string", `{"schema_version":"1","type":"hello","protocol_version":1,"backend":{"name":"demo"}}`, CodeSchemaVersionMismatch},
		{"zero", `{"schema_version":0,"type":"hello","protocol_version":1,"backend":{"name":"demo"}}`, CodeSchemaVersionMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), tc.code)
			if tc.code == CodeInvalidFrame && pe.Field != "schema_version" {
				t.Errorf("field = %q, want schema_version", pe.Field)
			}
		})
	}
}

// TestDecodeBackendFrameUnknownType 覆盖 type 缺失/未知/方向错误。
func TestDecodeBackendFrameUnknownType(t *testing.T) {
	cases := []struct {
		name string
		line string
		code string
	}{
		{"missing", `{"schema_version":1,"protocol_version":1,"backend":{"name":"demo"}}`, CodeInvalidFrame},
		{"not-string", `{"schema_version":1,"type":7}`, CodeInvalidFrame},
		{"unknown", `{"schema_version":1,"type":"telemetry"}`, CodeUnknownFrameType},
		{"host-frame-on-backend-channel", `{"schema_version":1,"type":"cancel","mode":"force"}`, CodeInvalidFrame},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), tc.code)
		})
	}

	// 反向：后端帧不得出现在 host 通道。
	decodeErr(t, mustDecodeHostErr(t, []byte(validHelloLine)), CodeInvalidFrame)
}

// TestDecodeRejectsMalformedJSON 覆盖非法 JSON、多个值、尾随垃圾、空行、非对象。
func TestDecodeRejectsMalformedJSON(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"empty", ``},
		{"whitespace", "  \n\t"},
		{"truncated", `{"schema_version":1,"type":"hel`},
		{"not-object", `[1,2,3]`},
		{"null", `null`},
		{"two-values", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"}} {"schema_version":1}`},
		{"trailing-garbage", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"}} x`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), CodeInvalidFrame)
		})
	}
}

// TestDecodeRejectsUnknownField 覆盖未知字段（含嵌套）。
func TestDecodeRejectsUnknownField(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		field string
	}{
		{"top-level", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"surprise":1}`, "surprise"},
		{"nested", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a","build":"42"}}`, "build"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), CodeInvalidFrame)
			if pe.Field != tc.field {
				t.Errorf("field = %q, want %q", pe.Field, tc.field)
			}
		})
	}
}

// TestDecodeRejectsWrongJSONType 覆盖字段类型错误。
func TestDecodeRejectsWrongJSONType(t *testing.T) {
	line := `{"schema_version":1,"type":"hello","protocol_version":"one","backend":{"name":"a"}}`
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeInvalidFrame)
	if pe.Field != "protocol_version" {
		t.Errorf("field = %q, want protocol_version", pe.Field)
	}
}

// ---------------------------------------------------------------------------
// 保留字段（禁止自报序号/身份/审计）
// ---------------------------------------------------------------------------

// TestDecodeRejectsReservedFields 覆盖每个保留字段名在顶层与嵌套层级的拒绝。
func TestDecodeRejectsReservedFields(t *testing.T) {
	for _, name := range ReservedFieldNames() {
		t.Run("top-level/"+name, func(t *testing.T) {
			line := `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"` + name + `":"x"}`
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeForbiddenField)
			if pe.Field != name {
				t.Errorf("field = %q, want %q", pe.Field, name)
			}
		})
		t.Run("nested/"+name, func(t *testing.T) {
			line := `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a","` + name + `":1}}`
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeForbiddenField)
			if pe.Field != "backend."+name {
				t.Errorf("field = %q, want %q", pe.Field, "backend."+name)
			}
		})
	}

	// 大小写不敏感：Sequence/IDENTITY 同样是自报尝试。
	for _, name := range []string{"Sequence", "IDENTITY", "Project_Seq", "Event_ID"} {
		line := `{"schema_version":1,"type":"observation","kind":"output","observed_at":"2026-09-26T08:00:00Z","` + name + `":1}`
		decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeForbiddenField)
	}
}

// TestDecodeReservedFieldInsideArrayIsRejected 覆盖数组内嵌套对象里的保留字段。
func TestDecodeReservedFieldInsideArrayIsRejected(t *testing.T) {
	line := `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},` +
		`"tools":[{"name":"read_file","effects":["read_workspace"],"audit":"forged"}]}`
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeForbiddenField)
	if pe.Field != "tools[0].audit" {
		t.Errorf("field = %q, want tools[0].audit", pe.Field)
	}
}

// TestDecodeReservedFieldPrecedesUnknownField 覆盖优先级：保留字段先于未知字段报错，
// 否则 project_seq 会被笼统报成 invalid_frame。
func TestDecodeReservedFieldPrecedesUnknownField(t *testing.T) {
	line := `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"zzz_unknown":1,"project_seq":7}`
	decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeForbiddenField)
}

// TestDecodeObservationPayloadKeepsNativeKeys 是边界裁定：observation 顶层 payload 是
// 后端原生 JSON，原样保留、不因里面的键被拒（服务端不会把它当身份/序号用）。
func TestDecodeObservationPayloadKeepsNativeKeys(t *testing.T) {
	payload := `{"tool_call_id":"call_1","sequence":9,"project_id":"p_evil","audit":{"actor":"x"},"identity":"me"}`
	line := `{"schema_version":1,"type":"observation","kind":"tool_requested","observed_at":"2026-09-26T08:00:00Z","provider_ref":"call_1","payload":` + payload + `}`
	frame, err := DecodeBackendFrame([]byte(line))
	if err != nil {
		t.Fatalf("payload subtree must be exempt from the reserved-field scan, got %v", err)
	}
	obs, ok := frame.(ObservationFrame)
	if !ok {
		t.Fatalf("decoded frame is %T, want ObservationFrame", frame)
	}
	if string(obs.Payload) != payload {
		t.Errorf("payload = %s, want it preserved verbatim: %s", obs.Payload, payload)
	}
	if _, err := obs.Observation(); err != nil {
		t.Errorf("mapped observation should validate, got %v", err)
	}

	// 例外只限 payload：同一帧顶层带 identity 仍拒绝。
	bad := `{"schema_version":1,"type":"observation","kind":"output","observed_at":"2026-09-26T08:00:00Z","identity":"me","payload":` + payload + `}`
	decodeErr(t, mustDecodeBackendErr(t, []byte(bad)), CodeForbiddenField)
}

// ---------------------------------------------------------------------------
// hello / 能力 / 工具
// ---------------------------------------------------------------------------

// TestDecodeHelloMinimalAndFull 覆盖最小与完整 hello。
func TestDecodeHelloMinimalAndFull(t *testing.T) {
	frame, err := DecodeBackendFrame([]byte(validHelloLine))
	if err != nil {
		t.Fatalf("minimal hello: %v", err)
	}
	hello, ok := frame.(Hello)
	if !ok {
		t.Fatalf("decoded frame is %T, want Hello", frame)
	}
	if hello.Backend.Name != "demo" || hello.ProtocolVersion != 1 {
		t.Errorf("hello = %+v", hello)
	}
	if len(hello.Capabilities) != 0 || len(hello.Requires) != 0 || len(hello.Tools) != 0 {
		t.Errorf("minimal hello should declare nothing, got %+v", hello)
	}

	full := `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"demo","version":"0.1.0"},` +
		`"capabilities":["non_interactive","json_stream","approval_hook","usage_tokens"],` +
		`"requires":["approval_hook"],` +
		`"tools":[{"name":"read_file","effects":["read_workspace"]},{"name":"run_tests","effects":["read_workspace","execute_process"]}]}`
	decoded, err := DecodeBackendFrame([]byte(full))
	if err != nil {
		t.Fatalf("full hello: %v", err)
	}
	hello, ok = decoded.(Hello)
	if !ok {
		t.Fatalf("decoded frame is %T, want Hello", decoded)
	}
	if len(hello.Tools) != 2 || hello.Tools[1].Effects[1] != ToolEffectExecuteProcess {
		t.Errorf("tools = %+v", hello.Tools)
	}
}

// TestHelloCapabilityValidation 覆盖 capabilities/requires 的未知值与重复值。
func TestHelloCapabilityValidation(t *testing.T) {
	cases := []struct {
		name string
		line string
		code string
	}{
		{"unknown-capability", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"capabilities":["telepathy"]}`, CodeUnknownCapability},
		{"unknown-required", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"requires":["telepathy"]}`, CodeUnknownRequiredCapability},
		{"duplicate-capability", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"capabilities":["mcp","mcp"]}`, CodeDuplicateCapability},
		{"duplicate-required", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"requires":["mcp","mcp"]}`, CodeDuplicateCapability},
		{"empty-name", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":""}}`, CodeInvalidFrame},
		{"missing-backend", `{"schema_version":1,"type":"hello","protocol_version":1}`, CodeInvalidFrame},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), tc.code)
			if tc.code == CodeUnknownRequiredCapability && !strings.Contains(pe.Message, "telepathy") {
				t.Errorf("message %q should name the offending capability", pe.Message)
			}
		})
	}
}

// TestHelloToolValidation 覆盖工具名/效果的校验。
func TestHelloToolValidation(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"empty-effects", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"tools":[{"name":"t","effects":[]}]}`},
		{"unknown-effect", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"tools":[{"name":"t","effects":["delete_everything"]}]}`},
		{"duplicate-effect", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"tools":[{"name":"t","effects":["network","network"]}]}`},
		{"duplicate-tool", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"tools":[{"name":"t","effects":["network"]},{"name":"t","effects":["network"]}]}`},
		{"empty-name", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"a"},"tools":[{"name":"","effects":["network"]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), CodeInvalidFrame)
		})
	}
}

// ---------------------------------------------------------------------------
// observation / exit
// ---------------------------------------------------------------------------

// TestDecodeObservationKinds 覆盖 7 个非终结 kind 全部可解码，exited 必须走 exit 帧。
func TestDecodeObservationKinds(t *testing.T) {
	for _, kind := range execbackend.AllObservationKinds() {
		line := `{"schema_version":1,"type":"observation","kind":"` + string(kind) + `","observed_at":"2026-09-26T08:00:00Z"}`
		frame, err := DecodeBackendFrame([]byte(line))
		if kind == execbackend.ObservationExited {
			decodeErr(t, err, CodeUnknownObservationKind)
			continue
		}
		if err != nil {
			t.Fatalf("kind %s: %v", kind, err)
		}
		obs, ok := frame.(ObservationFrame)
		if !ok {
			t.Fatalf("kind %s: decoded frame is %T", kind, frame)
		}
		mapped, err := obs.Observation()
		if err != nil {
			t.Fatalf("kind %s: mapping to execbackend.Observation: %v", kind, err)
		}
		if mapped.Kind != kind {
			t.Errorf("mapped kind = %s, want %s", mapped.Kind, kind)
		}
	}
}

// TestDecodeObservationBadKind 覆盖未知 kind 与缺失 observed_at。
func TestDecodeObservationBadKind(t *testing.T) {
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(
		`{"schema_version":1,"type":"observation","kind":"thinking","observed_at":"2026-09-26T08:00:00Z"}`)), CodeUnknownObservationKind)
	if pe.Field != "kind" {
		t.Errorf("field = %q, want kind", pe.Field)
	}
	pe = decodeErr(t, mustDecodeBackendErr(t, []byte(
		`{"schema_version":1,"type":"observation","kind":"output"}`)), CodeInvalidFrame)
	if pe.Field != "observed_at" {
		t.Errorf("field = %q, want observed_at", pe.Field)
	}
	decodeErr(t, mustDecodeBackendErr(t, []byte(
		`{"schema_version":1,"type":"observation","kind":"output","observed_at":"yesterday"}`)), CodeInvalidFrame)
}

// TestObservationFrameProviderRefLimit 覆盖 provider_ref 的 256 字节上限。
func TestObservationFrameProviderRefLimit(t *testing.T) {
	ok := `{"schema_version":1,"type":"observation","kind":"output","observed_at":"2026-09-26T08:00:00Z","provider_ref":"` +
		strings.Repeat("r", execbackend.MaxProviderRefBytes) + `"}`
	if _, err := DecodeBackendFrame([]byte(ok)); err != nil {
		t.Fatalf("provider_ref at the limit should be accepted, got %v", err)
	}
	tooLong := strings.Replace(ok, strings.Repeat("r", execbackend.MaxProviderRefBytes), strings.Repeat("r", execbackend.MaxProviderRefBytes+1), 1)
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(tooLong)), CodeInvalidFrame)
	if pe.Field != "provider_ref" {
		t.Errorf("field = %q, want provider_ref", pe.Field)
	}
}

// TestObservationFramePayloadLimit 覆盖 payload 自身的 256 KiB 上限（帧仍在上限内）。
func TestObservationFramePayloadLimit(t *testing.T) {
	pad := strings.Repeat("x", execbackend.MaxObservationBytes)
	line := `{"schema_version":1,"type":"observation","kind":"output","observed_at":"2026-09-26T08:00:00Z","payload":"` + pad + `"}`
	if len(line) > MaxFrameBytes {
		t.Fatalf("test frame %d bytes exceeds MaxFrameBytes %d; the payload limit must be reached first", len(line), MaxFrameBytes)
	}
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeInvalidFrame)
	if pe.Field != "payload" {
		t.Errorf("field = %q, want payload", pe.Field)
	}
}

// TestDecodeExitCompleted 覆盖 exit 帧解码与终结观察映射。
func TestDecodeExitCompleted(t *testing.T) {
	line := `{"schema_version":1,"type":"exit","result":{"exit_code":0,"reason":"completed","retryable":false,` +
		`"usage":{"input_tokens":1200,"output_tokens":340,"quality":"reported"}}}`
	frame, err := DecodeBackendFrame([]byte(line))
	if err != nil {
		t.Fatalf("exit frame: %v", err)
	}
	exit, ok := frame.(ExitFrame)
	if !ok {
		t.Fatalf("decoded frame is %T, want ExitFrame", frame)
	}
	obs, err := exit.Observation()
	if err != nil {
		t.Fatalf("exit mapping: %v", err)
	}
	if obs.Kind != execbackend.ObservationExited {
		t.Errorf("mapped kind = %s, want exited", obs.Kind)
	}
	if obs.ObservedAt.IsZero() != true {
		t.Errorf("terminal observation must not carry an ObservedAt (server stamps it), got %v", obs.ObservedAt)
	}
	var result execbackend.ExitResult
	if err := json.Unmarshal(obs.Payload, &result); err != nil {
		t.Fatalf("terminal payload is not an ExitResult: %v", err)
	}
	if result.Reason != execbackend.ExitReasonCompleted || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("result = %+v", result)
	}
}

// TestDecodeExitValidation 覆盖 exit 帧的非法结果。
func TestDecodeExitValidation(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		field string
	}{
		{"unknown-reason", `{"schema_version":1,"type":"exit","result":{"reason":"exploded","retryable":false,"usage":{"quality":"unknown"}}}`, "reason"},
		{"missing-usage", `{"schema_version":1,"type":"exit","result":{"reason":"completed","retryable":false}}`, "usage.quality"},
		{"bad-quality", `{"schema_version":1,"type":"exit","result":{"reason":"completed","retryable":false,"usage":{"quality":"maybe"}}}`, "usage.quality"},
		{"negative-exit-code", `{"schema_version":1,"type":"exit","result":{"exit_code":-1,"reason":"failed","retryable":false,"usage":{"quality":"unknown"}}}`, "exit_code"},
		{"cost-without-currency", `{"schema_version":1,"type":"exit","result":{"reason":"completed","retryable":false,"usage":{"quality":"reported","cost_minor":10}}}`, "usage.currency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), CodeInvalidFrame)
			if pe.Field != tc.field {
				t.Errorf("field = %q, want %q", pe.Field, tc.field)
			}
		})
	}
}

// TestExitFrameUnknownFieldRejected 覆盖 exit 帧里的未知字段（含伪造的 audit 字段：
// 先被保留字段扫描拦下）。
func TestExitFrameUnknownFieldRejected(t *testing.T) {
	line := `{"schema_version":1,"type":"exit","result":{"reason":"completed","retryable":false,"usage":{"quality":"unknown"},"audit":"forged"}}`
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(line)), CodeForbiddenField)
	if pe.Field != "result.audit" {
		t.Errorf("field = %q, want result.audit", pe.Field)
	}
}

// ---------------------------------------------------------------------------
// host 帧：start / approval_response / cancel
// ---------------------------------------------------------------------------

// TestEncodeDecodeStartFrame 覆盖 start 帧的编码-解码往返与信封写入。
func TestEncodeDecodeStartFrame(t *testing.T) {
	line := mustEncodeHost(t, sampleStartFrame())
	if line[len(line)-1] != '\n' {
		t.Fatalf("encoded frame must end with a newline, got %q", line[len(line)-1])
	}
	if bytes := strings.Count(string(line), "\n"); bytes != 1 {
		t.Fatalf("encoded frame must be exactly one line, got %d newlines", bytes)
	}
	frame, err := DecodeHostFrame(line)
	if err != nil {
		t.Fatalf("DecodeHostFrame: %v", err)
	}
	start, ok := frame.(StartFrame)
	if !ok {
		t.Fatalf("decoded frame is %T, want StartFrame", frame)
	}
	if start.Identity.ProjectID != "p_123" || start.Identity.Actor.Type != ActorAgent {
		t.Errorf("identity = %+v", start.Identity)
	}
	if start.WorkDir != sampleStartFrame().WorkDir {
		t.Errorf("work_dir = %q", start.WorkDir)
	}
	if start.Process.GracePeriodMS != 5000 || start.Process.HardDeadline == nil {
		t.Errorf("process = %+v", start.Process)
	}
	if len(start.Required) != 2 || start.Required[1] != execbackend.CapabilityJSONStream {
		t.Errorf("required = %v", start.Required)
	}
	// 协议里没有 Env：编码结果里不得出现 env 键。
	if strings.Contains(string(line), `"env"`) {
		t.Error("start frame must not carry an env field")
	}
	// 保留字段是服务端发给后端的只读上下文，不是后端自报：host 通道不扫描。
	if !strings.Contains(string(line), `"project_id"`) {
		t.Error("start frame should carry the read-only identity")
	}
}

// TestEncodeStartFrameFillsEnvelope 覆盖 Encode 自动写入 schema_version/type。
func TestEncodeStartFrameFillsEnvelope(t *testing.T) {
	frame := sampleStartFrame()
	frame.SchemaVersion, frame.Type = 0, ""
	line := mustEncodeHost(t, frame)
	if !strings.Contains(string(line), `"schema_version":1`) {
		t.Errorf("encoded frame must carry schema_version 1: %s", line)
	}
	if !strings.Contains(string(line), `"type":"start"`) {
		t.Errorf("encoded frame must carry type start: %s", line)
	}
}

// TestStartFrameValidation 覆盖 start 帧的非法输入（复用 execbackend.StartRequest.Validate）。
func TestStartFrameValidation(t *testing.T) {
	cases := []struct {
		name        string
		mut         func(*StartFrame)
		field       string
		wrapsExecbk bool
	}{
		// Identity.Validate 先跑（只读身份上下文先于 start 载荷校验）。
		{"missing-project-id", func(f *StartFrame) { f.Identity.ProjectID = "" }, "identity.project_id", false},
		{"missing-run-id", func(f *StartFrame) { f.Identity.RunID = "" }, "run.run_id", true},
		{"missing-attempt-id", func(f *StartFrame) { f.Identity.AttemptID = "" }, "run.attempt_id", true},
		{"missing-agent-revision", func(f *StartFrame) { f.Identity.AgentRevisionID = "" }, "run.agent_revision_id", true},
		{"unknown-actor-type", func(f *StartFrame) { f.Identity.Actor.Type = "robot" }, "identity.actor.type", false},
		{"missing-actor-id", func(f *StartFrame) { f.Identity.Actor.ID = "" }, "identity.actor.id", false},
		{"bad-snapshot-hash", func(f *StartFrame) { f.Input.SnapshotHash = "sha256:zz" }, "input.snapshot_hash", true},
		{"relative-work-dir", func(f *StartFrame) { f.WorkDir = filepath.Join("ws", "run_88") }, "work_dir", true},
		{"unclean-work-dir", func(f *StartFrame) {
			f.WorkDir = sampleWorkDir + string(filepath.Separator) + ".." + string(filepath.Separator) + "b"
		}, "work_dir", true},
		{"negative-grace", func(f *StartFrame) { f.Process.GracePeriodMS = -1 }, "process.grace_period", true},
		{"negative-max-output", func(f *StartFrame) { f.Process.MaxOutputBytes = -1 }, "process.max_output_bytes", true},
		{"unknown-required", func(f *StartFrame) { f.Required = []execbackend.Capability{"telepathy"} }, "required", true},
		{"duplicate-required", func(f *StartFrame) {
			f.Required = []execbackend.Capability{execbackend.CapabilityMCP, execbackend.CapabilityMCP}
		}, "required", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := sampleStartFrame()
			tc.mut(&frame)
			_, err := EncodeHostFrame(frame)
			// 缺失的身份字段由编码阶段拦下（decoder 侧对应 required 关键字）。
			pe := decodeErr(t, err, CodeInvalidFrame)
			if tc.field == "identity.project_id" || tc.field == "identity.actor.type" || tc.field == "identity.actor.id" {
				pe = decodeErr(t, err, CodeInvalidFrame)
			}
			if pe.Field != tc.field {
				t.Errorf("field = %q, want %q", pe.Field, tc.field)
			}
			var ee *execbackend.Error
			if tc.wrapsExecbk {
				// 底层原因必须仍是 execbackend 的错误，便于复用同一套失败 code。
				if !errors.As(err, &ee) {
					t.Errorf("error must wrap *execbackend.Error, got %v", err)
				}
			} else if errors.As(err, &ee) {
				t.Errorf("identity-level failure should not wrap *execbackend.Error, got %v", err)
			}
		})
	}
}

// TestDecodeHostFrameStartMissingIdentity 覆盖 host 侧 start 帧缺必填字段。
func TestDecodeHostFrameStartMissingIdentity(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		field string
	}{
		// work_dir 用 JSON 转义后的宿主路径（Windows 上 Clean 结果是反斜杠，必须转义）。
		{"missing-identity", `{"schema_version":1,"type":"start","input":{"snapshot_id":"s","snapshot_hash":"` + sampleHash + `","prompt":""},"work_dir":` + jsonString(sampleWorkDir) + `,"process":{"grace_period_ms":0,"max_output_bytes":0}}`, "identity.project_id"},
		{"missing-work-dir", `{"schema_version":1,"type":"start","identity":{"project_id":"p","run_id":"r","attempt_id":"a","agent_revision_id":"g","actor":{"type":"agent","id":"x"}},"input":{"snapshot_id":"s","snapshot_hash":"` + sampleHash + `","prompt":""},"process":{"grace_period_ms":0,"max_output_bytes":0}}`, "work_dir"},
		{"bad-actor", `{"schema_version":1,"type":"start","identity":{"project_id":"p","run_id":"r","attempt_id":"a","agent_revision_id":"g","actor":{"type":"robot","id":"x"}},"input":{"snapshot_id":"s","snapshot_hash":"` + sampleHash + `","prompt":""},"work_dir":` + jsonString(sampleWorkDir) + `,"process":{"grace_period_ms":0,"max_output_bytes":0}}`, "identity.actor.type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 经过 JSON 往返才断言：直接构造 Go 值会让"缺失"被零值掩盖。
			pe := decodeErr(t, mustDecodeHostErr(t, []byte(tc.line)), CodeInvalidFrame)
			if pe.Field != tc.field {
				t.Errorf("field = %q, want %q", pe.Field, tc.field)
			}
		})
	}
}

// TestDecodeHostFrameStartCarriesIdentityKeys 明确 host 通道不扫描保留字段名：
// project_id/run_id/attempt_id/agent_revision_id/identity 是服务端发出的只读上下文。
func TestDecodeHostFrameStartCarriesIdentityKeys(t *testing.T) {
	line := mustEncodeHost(t, sampleStartFrame())
	for _, key := range []string{"identity", "project_id", "run_id", "attempt_id", "agent_revision_id"} {
		if !strings.Contains(string(line), `"`+key+`"`) {
			t.Errorf("start frame should carry %q", key)
		}
	}
	if _, err := DecodeHostFrame(line); err != nil {
		t.Fatalf("DecodeHostFrame: %v", err)
	}
}

// TestApprovalResponseFrame 覆盖 approval_response 的往返与校验。
func TestApprovalResponseFrame(t *testing.T) {
	decision := execbackend.ApprovalDecision{
		ApprovalID:  "ap_1",
		ProviderRef: "call_1",
		Approved:    true,
	}
	line := mustEncodeHost(t, ApprovalResponseFrame{Decision: decision})
	frame, err := DecodeHostFrame(line)
	if err != nil {
		t.Fatalf("DecodeHostFrame: %v", err)
	}
	got, ok := frame.(ApprovalResponseFrame)
	if !ok {
		t.Fatalf("decoded frame is %T, want ApprovalResponseFrame", frame)
	}
	if got.Decision.ApprovalID != "ap_1" || !got.Decision.Approved {
		t.Errorf("decision = %+v", got.Decision)
	}

	// approved=false 是明确拒绝，不能因为零值被当成"未填写"：字段必填。
	if _, err := EncodeHostFrame(ApprovalResponseFrame{Decision: execbackend.ApprovalDecision{ApprovalID: "ap_1"}}); err != nil {
		t.Fatalf("explicit false must encode fine: %v", err)
	}
	_, err = EncodeHostFrame(ApprovalResponseFrame{Decision: decision})
	if err != nil {
		t.Fatalf("valid decision: %v", err)
	}
	pe := decodeErr(t, func() error {
		_, err := EncodeHostFrame(ApprovalResponseFrame{Decision: execbackend.ApprovalDecision{Approved: true}})
		return err
	}(), CodeInvalidFrame)
	if pe.Field != "decision.approval_id" {
		t.Errorf("field = %q, want decision.approval_id", pe.Field)
	}
}

// TestCancelFrame 覆盖 cancel 帧的两种模式与非法模式。
func TestCancelFrame(t *testing.T) {
	for _, mode := range []execbackend.CancelMode{execbackend.CancelGraceful, execbackend.CancelForce} {
		line := mustEncodeHost(t, CancelFrame{Mode: mode})
		frame, err := DecodeHostFrame(line)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		got, ok := frame.(CancelFrame)
		if !ok {
			t.Fatalf("decoded frame is %T, want CancelFrame", frame)
		}
		if got.Mode != mode {
			t.Errorf("mode = %s, want %s", got.Mode, mode)
		}
	}
	pe := decodeErr(t, func() error {
		_, err := EncodeHostFrame(CancelFrame{Mode: "maybe"})
		return err
	}(), CodeInvalidFrame)
	if pe.Field != "mode" {
		t.Errorf("field = %q, want mode", pe.Field)
	}
	decodeErr(t, mustDecodeHostErr(t, []byte(`{"schema_version":1,"type":"cancel","mode":"maybe"}`)), CodeInvalidFrame)
}

// mustDecodeHostErr 解码一行 host 帧并返回错误（nil 时 fatal）。
func mustDecodeHostErr(t *testing.T, line []byte) error {
	t.Helper()
	frame, err := DecodeHostFrame(line)
	if err == nil {
		t.Fatalf("expected decode error, got frame %T", frame)
	}
	return err
}

// ---------------------------------------------------------------------------
// 协商
// ---------------------------------------------------------------------------

// TestNegotiate 覆盖握手协商的全部判定路径。
func TestNegotiate(t *testing.T) {
	hello := Hello{
		SchemaVersion:   ProtocolVersion,
		Type:            FrameHello,
		ProtocolVersion: ProtocolVersion,
		Backend:         BackendInfo{Name: "demo", Version: "0.1.0"},
		Capabilities: []execbackend.Capability{
			execbackend.CapabilityNonInteractive,
			execbackend.CapabilityJSONStream,
		},
		Requires: []execbackend.Capability{execbackend.CapabilityApprovalHook},
		Tools: []ToolEffect{
			{Name: "read_file", Effects: []ToolEffectKind{ToolEffectReadWorkspace}},
		},
	}

	t.Run("ok", func(t *testing.T) {
		got, err := Negotiate(hello, []execbackend.Capability{execbackend.CapabilityNonInteractive})
		if err != nil {
			t.Fatalf("Negotiate: %v", err)
		}
		if got.BackendName != "demo" || got.ProtocolVersion != ProtocolVersion {
			t.Errorf("negotiated = %+v", got)
		}
		if !got.Has(execbackend.CapabilityJSONStream) {
			t.Error("declared capability should be reported as available")
		}
		if got.Has(execbackend.CapabilitySandbox) {
			t.Error("undeclared capability must be false (§27.7)")
		}
		if got.Report.Source != capabilitySource || got.Report.Backend != "demo" {
			t.Errorf("report = %+v", got.Report)
		}
		if !got.Report.ProbedAt.IsZero() {
			t.Error("negotiation is a pure function: ProbedAt must be left for the caller to stamp")
		}
	})

	t.Run("protocol-version-mismatch", func(t *testing.T) {
		bad := hello
		bad.ProtocolVersion = 2
		_, err := Negotiate(bad, nil)
		decodeErr(t, err, CodeProtocolVersionMismatch)
	})

	t.Run("unknown-required-capability", func(t *testing.T) {
		bad := hello
		bad.Requires = []execbackend.Capability{"telepathy"}
		_, err := Negotiate(bad, nil)
		decodeErr(t, err, CodeUnknownRequiredCapability)
	})

	t.Run("required-not-declared", func(t *testing.T) {
		_, err := Negotiate(hello, []execbackend.Capability{execbackend.CapabilitySandbox})
		if err == nil {
			t.Fatal("expected capability_unavailable")
		}
		if !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
			t.Fatalf("error must be capability_unavailable, got %v", err)
		}
		var ee *execbackend.Error
		if !errors.As(err, &ee) {
			t.Fatalf("error must be *execbackend.Error, got %T", err)
		}
		if len(ee.Missing) != 1 || ee.Missing[0] != execbackend.CapabilitySandbox {
			t.Errorf("missing = %v, want [sandbox]", ee.Missing)
		}
	})

	t.Run("unknown-required-in-run", func(t *testing.T) {
		_, err := Negotiate(hello, []execbackend.Capability{"telepathy"})
		if !errors.Is(err, execbackend.ErrCapabilityUnavailable) {
			t.Fatalf("unknown required value must be missing (capability_unavailable), got %v", err)
		}
	})

	t.Run("invalid-hello", func(t *testing.T) {
		bad := hello
		bad.Backend.Name = ""
		_, err := Negotiate(bad, nil)
		decodeErr(t, err, CodeInvalidFrame)
	})

	t.Run("duplicate-capability", func(t *testing.T) {
		bad := hello
		bad.Capabilities = []execbackend.Capability{execbackend.CapabilityMCP, execbackend.CapabilityMCP}
		_, err := Negotiate(bad, nil)
		decodeErr(t, err, CodeDuplicateCapability)
	})
}

// TestNegotiateRejectsUnsupportedSchemaVersion 覆盖手搓 Hello 的信封校验。
func TestNegotiateRejectsUnsupportedSchemaVersion(t *testing.T) {
	hello := Hello{
		SchemaVersion:   2,
		Type:            FrameHello,
		ProtocolVersion: ProtocolVersion,
		Backend:         BackendInfo{Name: "demo"},
	}
	_, err := Negotiate(hello, nil)
	decodeErr(t, err, CodeSchemaVersionMismatch)
}

// ---------------------------------------------------------------------------
// 错误形状
// ---------------------------------------------------------------------------

// TestProtocolErrorShape 覆盖错误码、errors.Is/As 与不回显帧内容。
func TestProtocolErrorShape(t *testing.T) {
	line := `{"schema_version":1,"type":"observation","kind":"output","observed_at":"2026-09-26T08:00:00Z","project_seq":7,"payload":{"secret":"top-secret-prompt"}}`
	err := mustDecodeBackendErr(t, []byte(line))
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("error is %T, want *ProtocolError", err)
	}
	if !errors.Is(err, &ProtocolError{Code: CodeForbiddenField}) {
		t.Errorf("errors.Is with the same code must match, got %v", err)
	}
	if errors.Is(err, &ProtocolError{Code: CodeInvalidFrame}) {
		t.Errorf("errors.Is must not match a different code")
	}
	for _, secret := range []string{"top-secret-prompt", "project_seq\":7"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error text %q must not echo frame content", err.Error())
		}
	}
	if pe.Field != "project_seq" {
		t.Errorf("field = %q, want project_seq", pe.Field)
	}
	if !strings.Contains(pe.Error(), CodeForbiddenField) {
		t.Errorf("error text %q should name the code", pe.Error())
	}
}

// TestProtocolErrorNilSafe 覆盖 nil 接收者的错误方法。
func TestProtocolErrorNilSafe(t *testing.T) {
	var pe *ProtocolError
	if pe.Error() != "<nil>" {
		t.Errorf("nil Error() = %q", pe.Error())
	}
	if pe.Unwrap() != nil {
		t.Errorf("nil Unwrap() must be nil")
	}
	if pe.Is(&ProtocolError{Code: CodeInvalidFrame}) {
		t.Errorf("nil Is must be false")
	}
}

// TestReservedFieldNames 覆盖保留字段清单的稳定性。
func TestReservedFieldNames(t *testing.T) {
	names := ReservedFieldNames()
	want := []string{
		"actor", "agent_revision_id", "attempt_id", "audit", "event_id", "event_type",
		"identity", "occurred_at", "project_id", "project_seq", "run_id", "run_seq",
		"seq", "sequence",
	}
	if len(names) != len(want) {
		t.Fatalf("ReservedFieldNames() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("ReservedFieldNames()[%d] = %q, want %q", i, names[i], want[i])
		}
	}
	for _, name := range want {
		if !IsReservedFieldName(name) {
			t.Errorf("IsReservedFieldName(%q) = false", name)
		}
	}
	if IsReservedFieldName("tool_call_id") {
		t.Error("tool_call_id must not be reserved")
	}
}

// TestFrameTypeHelpers 覆盖帧类型枚举与方向判定。
func TestFrameTypeHelpers(t *testing.T) {
	for _, ft := range AllFrameTypes() {
		if !ft.Valid() {
			t.Errorf("%s.Valid() = false", ft)
		}
		if ft.IsBackendFrame() == ft.IsHostFrame() {
			t.Errorf("%s must belong to exactly one direction", ft)
		}
	}
	if FrameType("telemetry").Valid() {
		t.Error("unknown frame type must be invalid")
	}
	for _, ft := range BackendFrameTypes() {
		if !ft.IsBackendFrame() || ft.IsHostFrame() {
			t.Errorf("%s: direction helpers disagree", ft)
		}
	}
	for _, ft := range HostFrameTypes() {
		if !ft.IsHostFrame() || ft.IsBackendFrame() {
			t.Errorf("%s: direction helpers disagree", ft)
		}
	}
	if FrameObservation.IsControlFrame() {
		t.Error("observation is not a control frame as a whole: its kind decides")
	}
	for _, ft := range []FrameType{FrameHello, FrameExit, FrameStart, FrameApprovalResponse, FrameCancel} {
		if !ft.IsControlFrame() {
			t.Errorf("%s should be a control frame", ft)
		}
	}
}

// TestFrameJSONFields 覆盖供漂移测试使用的字段集合视图。
func TestFrameJSONFields(t *testing.T) {
	fields := FrameJSONFields(Hello{})
	if len(fields) != 7 {
		t.Fatalf("Hello fields = %v", fields)
	}
	for name, omitempty := range map[string]bool{
		"schema_version": false, "type": false, "protocol_version": false, "backend": false,
		"capabilities": true, "requires": true, "tools": true,
	} {
		got, ok := fields[name]
		if !ok {
			t.Errorf("Hello field %q missing", name)
			continue
		}
		if got != omitempty {
			t.Errorf("Hello field %q omitempty = %v, want %v", name, got, omitempty)
		}
	}
	if FrameJSONFields(FrameType("telemetry")) != nil {
		t.Error("unknown frame type must have no field set")
	}
	if FrameJSONFields(struct{}{}) != nil {
		t.Error("unknown Go type must have no field set")
	}
}

// TestEncodeNilFrame 覆盖 nil 帧的编码拒绝。
func TestEncodeNilFrame(t *testing.T) {
	if _, err := EncodeBackendFrame(nil); err == nil {
		t.Error("EncodeBackendFrame(nil) must fail")
	}
	if _, err := EncodeHostFrame(nil); err == nil {
		t.Error("EncodeHostFrame(nil) must fail")
	}
}

// TestDecodeRejectsNonCanonicalKeyCase：encoding/json 按字段名大小写不敏感匹配，
// "kind" 旁边再放一个 "KIND" 会让结构体解出后一个值，而信封视图（选长度上限用）读到
// 的是前一个。v1 字段名全是小写，非小写键一律 invalid_frame；observation 顶层
// payload 是后端原生 JSON，键名大小写不受限。
func TestDecodeRejectsNonCanonicalKeyCase(t *testing.T) {
	const at = `"observed_at":"2026-09-26T08:00:00Z"`
	cases := []struct {
		name  string
		line  string
		field string
	}{
		{"shadow kind", `{"schema_version":1,"type":"observation","kind":"output","KIND":"approval_required",` + at + `}`, "KIND"},
		{"lone mixed-case kind", `{"schema_version":1,"type":"observation","Kind":"output",` + at + `}`, "Kind"},
		{"shadow type", `{"schema_version":1,"type":"observation","Type":"hello","kind":"output",` + at + `}`, "Type"},
		{"nested backend field", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"Name":"demo"}}`, "backend.Name"},
		{"array element field", `{"schema_version":1,"type":"hello","protocol_version":1,"backend":{"name":"demo"},"tools":[{"name":"t","Effects":["read_workspace"]}]}`, "tools[0].Effects"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := decodeErr(t, mustDecodeBackendErr(t, []byte(tc.line)), CodeInvalidFrame)
			if pe.Field != tc.field {
				t.Fatalf("field = %q, want %q", pe.Field, tc.field)
			}
		})
	}

	// payload 子树的键名由后端决定，不受限。
	ok := `{"schema_version":1,"type":"observation","kind":"output",` + at + `,"payload":{"Text":"hi","Nested":{"A":[{"B":1}]}}}`
	if _, err := DecodeBackendFrame([]byte(ok)); err != nil {
		t.Fatalf("mixed-case keys inside payload must be accepted: %v", err)
	}
	// 保留名的大小写变体仍按 forbidden_field 报（保留字段扫描先于本检查）。
	decodeErr(t, mustDecodeBackendErr(t, []byte(`{"schema_version":1,"type":"observation","kind":"output",`+at+`,"Sequence":1}`)), CodeForbiddenField)
	// host 帧同样只接受小写键。
	decodeErr(t, mustDecodeHostErr(t, []byte(`{"schema_version":1,"type":"cancel","Mode":"force"}`)), CodeInvalidFrame)
	// 超长键名不进 Field，也不回显原文。
	long := strings.Repeat("K", maxTokenEchoBytes+1)
	pe := decodeErr(t, mustDecodeBackendErr(t, []byte(`{"schema_version":1,"type":"observation","kind":"output",`+at+`,"`+long+`":1}`)), CodeInvalidFrame)
	if pe.Field != "" || strings.Contains(pe.Message, long) {
		t.Fatalf("long key echoed: field=%q message=%q", pe.Field, pe.Message)
	}
}
