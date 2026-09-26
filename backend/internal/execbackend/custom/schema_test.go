package custom

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件是 T4.03.a 的 schema ↔ Go 漂移测试（§15 T4.03、§28 T4.03.a）。
//
// 事实来源分工（冻结）：
//   - backend/schemas/custom-backend.schema.json 是线协议契约；
//   - Go 结构体的 json tag 是解码/编码实现的契约；
//   - execbackend 的枚举常量（All* 助手与源码常量）是语义契约。
//
// 三者任何一处单独改动都必须让本文件失败，从而强制同变更更新 schema、Go 类型与 fixtures。

const (
	// customSchemaFile 是自定义执行器协议的帧 schema（相对 backend/internal/execbackend/custom）。
	customSchemaFile = "../../../schemas/custom-backend.schema.json"
	// identitySchemaFile 是被 start.identity $ref 复用的身份 schema。
	identitySchemaFile = "../../../schemas/execution-identity.schema.json"
	// execbackendSourceDir 是 execbackend 包源码目录（读取枚举常量的地方）。
	execbackendSourceDir = ".."
)

// schemaNode 是 schema 的一个节点；只需要漂移测试用到的关键字。
type schemaNode struct {
	Ref                  string                `json:"$ref"`
	Type                 json.RawMessage       `json:"type"`
	Enum                 []string              `json:"enum"`
	Const                json.RawMessage       `json:"const"`
	Required             []string              `json:"required"`
	Properties           map[string]schemaNode `json:"properties"`
	Items                *schemaNode           `json:"items"`
	AdditionalProperties *bool                 `json:"additionalProperties"`
	AllOf                []schemaNode          `json:"allOf"`
}

// customSchema 是 custom-backend.schema.json 的顶层视图。
type customSchema struct {
	Schema  string                `json:"$schema"`
	ID      string                `json:"$id"`
	OneOf   []schemaNode          `json:"oneOf"`
	Defs    map[string]schemaNode `json:"definitions"`
	rawText string
}

// identitySchema 是 execution-identity.schema.json 的视图。
type identitySchema struct {
	Required   []string              `json:"required"`
	Properties map[string]schemaNode `json:"properties"`
}

// readCustomSchema 读取并解析帧 schema。
func readCustomSchema(t *testing.T) customSchema {
	t.Helper()
	raw, err := os.ReadFile(customSchemaFile)
	if err != nil {
		t.Fatalf("read %s: %v", customSchemaFile, err)
	}
	var schema customSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse %s: %v", customSchemaFile, err)
	}
	schema.rawText = string(raw)
	if schema.ID != filepath.Base(customSchemaFile) {
		t.Fatalf("schema $id = %q, want %q (validate-fixtures.mjs 按文件名 getSchema)", schema.ID, filepath.Base(customSchemaFile))
	}
	if !strings.Contains(schema.Schema, "draft-07") {
		t.Fatalf("schema must be draft-07, got %q", schema.Schema)
	}
	return schema
}

// readIdentitySchema 读取身份 schema。
func readIdentitySchema(t *testing.T) identitySchema {
	t.Helper()
	raw, err := os.ReadFile(identitySchemaFile)
	if err != nil {
		t.Fatalf("read %s: %v", identitySchemaFile, err)
	}
	var schema identitySchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse %s: %v", identitySchemaFile, err)
	}
	return schema
}

// resolveLocal 沿着同文档的 #/definitions/<name> $ref 解引用；其余 $ref 原样返回。
func resolveLocal(schema customSchema, node schemaNode) schemaNode {
	seen := 0
	for node.Ref != "" && strings.HasPrefix(node.Ref, "#/definitions/") {
		name := strings.TrimPrefix(node.Ref, "#/definitions/")
		target, ok := schema.Defs[name]
		if !ok {
			return node
		}
		node = target
		seen++
		if seen > 8 {
			return node
		}
	}
	return node
}

// enumOf 返回节点（自动解引用本地 $ref）的枚举值；没有枚举时 fatal。
func enumOf(t *testing.T, schema customSchema, where string, node schemaNode) []string {
	t.Helper()
	node = resolveLocal(schema, node)
	if len(node.Enum) == 0 {
		t.Fatalf("%s: expected an enum (got %+v)", where, node)
	}
	return node.Enum
}

// refOf 返回节点的 $ref；没有 $ref 时 fatal。
func refOf(t *testing.T, where string, node schemaNode) string {
	t.Helper()
	if node.Ref == "" {
		t.Fatalf("%s: expected a $ref, got %+v", where, node)
	}
	return node.Ref
}

// typeNames 返回节点声明的 JSON 类型名（字符串或数组）。
func typeNames(t *testing.T, where string, node schemaNode) []string {
	t.Helper()
	if len(node.Type) == 0 {
		return nil
	}
	var single string
	if err := json.Unmarshal(node.Type, &single); err == nil {
		return []string{single}
	}
	var many []string
	if err := json.Unmarshal(node.Type, &many); err != nil {
		t.Fatalf("%s: cannot parse type %s: %v", where, node.Type, err)
	}
	return many
}

// constString 返回节点的 const（要求是字符串）；自动解引用本地 $ref。
func constString(t *testing.T, schema customSchema, where string, node schemaNode) string {
	t.Helper()
	node = resolveLocal(schema, node)
	if len(node.Const) == 0 {
		t.Fatalf("%s: expected a const", where)
	}
	var s string
	if err := json.Unmarshal(node.Const, &s); err != nil {
		t.Fatalf("%s: const is not a string: %s", where, node.Const)
	}
	return s
}

// constInt 返回节点的 const（要求是整数）。
func constInt(t *testing.T, schema customSchema, where string, node schemaNode) int {
	t.Helper()
	node = resolveLocal(schema, node)
	if len(node.Const) == 0 {
		t.Fatalf("%s: expected a const", where)
	}
	var n int
	if err := json.Unmarshal(node.Const, &n); err != nil {
		t.Fatalf("%s: const is not an integer: %s", where, node.Const)
	}
	return n
}

// goStrings 把 Go 枚举切片渲染为字符串切片，便于与 schema enum 对比。
func goStrings[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	return out
}

// sortedSchemaKeys 返回 map 的键按字典序排列的结果。
func sortedSchemaKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// 帧类型 / 枚举
// ---------------------------------------------------------------------------

// TestSchemaFrameTypeEnumMatchesGo 断言 frame_type 枚举 == Go 帧类型常量（顺序一致）。
func TestSchemaFrameTypeEnumMatchesGo(t *testing.T) {
	schema := readCustomSchema(t)
	node := schema.Defs["frame_type"]
	if node.Enum == nil {
		t.Fatal("definitions.frame_type is missing")
	}
	want := goStrings(AllFrameTypes())
	if len(node.Enum) != len(want) {
		t.Fatalf("frame_type enum = %v, want %v", node.Enum, want)
	}
	for i := range want {
		if node.Enum[i] != want[i] {
			t.Errorf("frame_type enum[%d] = %q, want %q", i, node.Enum[i], want[i])
		}
		if !FrameType(want[i]).Valid() {
			t.Errorf("FrameType(%q).Valid() = false", want[i])
		}
	}
	if FrameType("telemetry").Valid() {
		t.Error("FrameType(telemetry).Valid() = true, want false")
	}
}

// TestSchemaObservationKindEnumMatchesGo 断言 observation.kind 枚举 == AllObservationKinds()
// 去掉终结值 exited（终结只能走 exit 帧）。
func TestSchemaObservationKindEnumMatchesGo(t *testing.T) {
	schema := readCustomSchema(t)
	where := "definitions.observation.properties.kind"
	enum := enumOf(t, schema, where, schema.Defs["observation"].Properties["kind"])

	var want []string
	for _, kind := range execbackend.AllObservationKinds() {
		if kind == execbackend.ObservationExited {
			continue
		}
		want = append(want, string(kind))
	}
	assertSameStrings(t, where, enum, want)

	if containsString(enum, string(execbackend.ObservationExited)) {
		t.Error("observation.kind must not allow the terminal kind exited: use the exit frame")
	}
	// 源码常量与 All* 助手必须同步（防止新增 kind 只改了一处）。
	astKinds := constValuesOfType(t, "ObservationKind")
	assertSameStrings(t, "execbackend ObservationKind consts", astKinds, goStrings(execbackend.AllObservationKinds()))
}

// TestSchemaCapabilityEnumMatchesGo 断言能力枚举 == execbackend.AllCapabilities()，
// 且出现在 hello.capabilities / hello.requires / start.required 三处。
func TestSchemaCapabilityEnumMatchesGo(t *testing.T) {
	schema := readCustomSchema(t)
	want := goStrings(execbackend.AllCapabilities())
	if got := constValuesOfType(t, "Capability"); len(got) != len(want) {
		t.Fatalf("execbackend Capability consts = %v, want %v", got, want)
	}

	places := map[string]schemaNode{
		"definitions.capability":                          schema.Defs["capability"],
		"definitions.hello.properties.capabilities.items": *schema.Defs["hello"].Properties["capabilities"].Items,
		"definitions.hello.properties.requires.items":     *schema.Defs["hello"].Properties["requires"].Items,
		"definitions.start.properties.required.items":     *schema.Defs["start"].Properties["required"].Items,
	}
	for where, node := range places {
		assertSameStrings(t, where, enumOf(t, schema, where, node), want)
		if node.Ref == "" && where != "definitions.capability" {
			t.Errorf("%s: expected a $ref to #/definitions/capability", where)
		}
	}
}

// TestSchemaExecbackendEnumsMatchGo 断言 exit reason / usage quality / cancel mode 枚举与
// execbackend 源码常量一致（这几个枚举没有 All* 助手，直接从源码常量取事实）。
func TestSchemaExecbackendEnumsMatchGo(t *testing.T) {
	schema := readCustomSchema(t)
	cases := []struct {
		where    string
		node     schemaNode
		typeName string
	}{
		{"definitions.exit_reason", schema.Defs["exit_reason"], "ExitReason"},
		{"definitions.usage_quality", schema.Defs["usage_quality"], "UsageQuality"},
		{"definitions.cancel_mode", schema.Defs["cancel_mode"], "CancelMode"},
	}
	for _, tc := range cases {
		want := constValuesOfType(t, tc.typeName)
		if len(want) == 0 {
			t.Fatalf("%s: no execbackend %s constants found in %s", tc.where, tc.typeName, execbackendSourceDir)
		}
		got := sortedCopy(enumOf(t, schema, tc.where, tc.node))
		assertSameStrings(t, tc.where, got, sortedCopy(want))
	}
	// exit.status 字段必须引用 exit_reason 定义，不能就地写枚举。
	if ref := refOf(t, "exit_result.properties.reason", schema.Defs["exit_result"].Properties["reason"]); ref != "#/definitions/exit_reason" {
		t.Errorf("exit_result.reason $ref = %q, want #/definitions/exit_reason", ref)
	}
}

// TestSchemaToolEffectEnumMatchesGo 断言工具效果枚举 == AllToolEffectKinds()。
func TestSchemaToolEffectEnumMatchesGo(t *testing.T) {
	schema := readCustomSchema(t)
	where := "definitions.tool_effect_kind"
	assertSameStrings(t, where, enumOf(t, schema, where, schema.Defs["tool_effect_kind"]), goStrings(AllToolEffectKinds()))
	items := schema.Defs["tool"].Properties["effects"].Items
	if items == nil {
		t.Fatal("definitions.tool.properties.effects.items is missing")
	}
	if ref := refOf(t, "definitions.tool.properties.effects.items", *items); ref != "#/definitions/tool_effect_kind" {
		t.Errorf("tool.effects.items $ref = %q, want #/definitions/tool_effect_kind", ref)
	}
}

// TestIdentityMirrorsGo 断言本包的 Identity 结构镜像 identity schema，
// 且 start.identity 是指向 execution-identity.schema.json 的 $ref。
func TestIdentityMirrorsGo(t *testing.T) {
	schema := readCustomSchema(t)
	where := "definitions.start.properties.identity"
	if ref := refOf(t, where, schema.Defs["start"].Properties["identity"]); ref != "execution-identity.schema.json" {
		t.Fatalf("%s $ref = %q, want execution-identity.schema.json", where, ref)
	}

	identity := readIdentitySchema(t)
	assertShape(t, "Identity (custom.start) vs execution-identity.schema.json",
		identity.Properties, FrameJSONFields(Identity{}), identity.Required)
	assertShape(t, "IdentityActor (custom.start) vs execution-identity.schema.json",
		identity.Properties["actor"].Properties, FrameJSONFields(IdentityActor{}), identity.Properties["actor"].Required)
	assertSameStrings(t, "execution-identity actor.type",
		enumOf(t, customSchema{}, "execution-identity actor.type", identity.Properties["actor"].Properties["type"]),
		goStrings(AllActorTypes()))
}

// ---------------------------------------------------------------------------
// 每个帧/嵌套定义的字段集合与必填项
// ---------------------------------------------------------------------------

// TestSchemaFrameShapesMatchGo 断言每个帧与嵌套定义的 properties/required 与 Go 结构体
// 的 json tag、omitempty 一一对应（新增字段必须两边同时改）。
func TestSchemaFrameShapesMatchGo(t *testing.T) {
	schema := readCustomSchema(t)
	shapes := []struct {
		def     string
		goValue any
	}{
		{"hello", Hello{}},
		{"observation", ObservationFrame{}},
		{"exit", ExitFrame{}},
		{"start", StartFrame{}},
		{"approval_response", ApprovalResponseFrame{}},
		{"cancel", CancelFrame{}},
		{"backend_info", BackendInfo{}},
		{"tool", ToolEffect{}},
		{"process_spec", ProcessSpec{}},
		{"frozen_input", execbackend.FrozenInput{}},
		{"exit_result", execbackend.ExitResult{}},
		{"usage", execbackend.Usage{}},
		{"approval_decision", execbackend.ApprovalDecision{}},
	}
	for _, shape := range shapes {
		node, ok := schema.Defs[shape.def]
		if !ok {
			t.Errorf("definitions.%s is missing", shape.def)
			continue
		}
		assertShape(t, "definitions."+shape.def, node.Properties, FrameJSONFields(shape.goValue), node.Required)
	}
}

// assertShape 断言 schema 定义的字段集合与 Go 结构体一致：
// 属性名 == json tag，required == 非 omitempty 字段。
func assertShape(t *testing.T, where string, props map[string]schemaNode, goFields map[string]bool, required []string) {
	t.Helper()
	if goFields == nil {
		t.Fatalf("%s: no Go field set for this shape", where)
	}
	var wantProps, wantRequired []string
	for name, omitempty := range goFields {
		wantProps = append(wantProps, name)
		if !omitempty {
			wantRequired = append(wantRequired, name)
		}
	}
	sort.Strings(wantProps)
	sort.Strings(wantRequired)
	gotProps := sortedSchemaKeys(props)
	if !equalStrings(gotProps, wantProps) {
		t.Errorf("%s: schema properties = %v, Go json tags = %v", where, gotProps, wantProps)
	}
	gotRequired := append([]string(nil), required...)
	sort.Strings(gotRequired)
	if !equalStrings(gotRequired, wantRequired) {
		t.Errorf("%s: schema required = %v, Go non-omitempty fields = %v", where, gotRequired, wantRequired)
	}
	for name := range props {
		if _, ok := goFields[name]; !ok && containsString(wantProps, name) {
			t.Errorf("%s: schema property %q has no Go field", where, name)
		}
	}
}

// TestSchemaFramesPinEnvelope 断言 6 个帧都用 const 钉住自己的 type、引用同一个
// schema_version 定义，且定义值为 ProtocolVersion。
func TestSchemaFramesPinEnvelope(t *testing.T) {
	schema := readCustomSchema(t)
	if got := constInt(t, schema, "definitions.schema_version", schema.Defs["schema_version"]); got != ProtocolVersion {
		t.Errorf("definitions.schema_version const = %d, want %d", got, ProtocolVersion)
	}
	if got := constInt(t, schema, "definitions.protocol_version", schema.Defs["protocol_version"]); got != ProtocolVersion {
		t.Errorf("definitions.protocol_version const = %d, want %d", got, ProtocolVersion)
	}
	if len(schema.OneOf) != len(AllFrameTypes()) {
		t.Fatalf("oneOf has %d branches, want %d frame types", len(schema.OneOf), len(AllFrameTypes()))
	}
	seen := map[string]bool{}
	for i, branch := range schema.OneOf {
		ref := refOf(t, "oneOf["+strconv.Itoa(i)+"]", branch)
		name := strings.TrimPrefix(ref, "#/definitions/")
		if ref == name {
			t.Fatalf("oneOf[%d] $ref = %q, want #/definitions/<frame>", i, ref)
		}
		seen[name] = true
		frame, ok := schema.Defs[name]
		if !ok {
			t.Fatalf("oneOf[%d] references missing definition %q", i, name)
		}
		if !FrameType(name).Valid() {
			t.Errorf("oneOf[%d] references %q which is not a Go frame type", i, name)
		}
		if ref := refOf(t, name+".properties.schema_version", frame.Properties["schema_version"]); ref != "#/definitions/schema_version" {
			t.Errorf("%s.schema_version $ref = %q", name, ref)
		}
		typeProp := frame.Properties["type"]
		if len(typeProp.AllOf) != 2 {
			t.Fatalf("%s.type must pin the type: allOf[enum-ref, const], got %+v", name, typeProp)
		}
		if ref := refOf(t, name+".properties.type.allOf[0]", typeProp.AllOf[0]); ref != "#/definitions/frame_type" {
			t.Errorf("%s.type.allOf[0] $ref = %q, want #/definitions/frame_type", name, ref)
		}
		if got := constString(t, schema, name+".properties.type.allOf[1]", typeProp.AllOf[1]); got != name {
			t.Errorf("%s.type const = %q, want %q", name, got, name)
		}
	}
	for _, ft := range AllFrameTypes() {
		if !seen[string(ft)] {
			t.Errorf("frame %q is not reachable through oneOf", ft)
		}
	}
}

// TestSchemaEveryObjectRejectsAdditionalProperties 断言 schema 里每个对象都
// additionalProperties:false（未知字段拒绝而不是忽略），且文件里没有 true 的写法。
func TestSchemaEveryObjectRejectsAdditionalProperties(t *testing.T) {
	schema := readCustomSchema(t)
	count := 0
	var walk func(where string, node schemaNode)
	walk = func(where string, node schemaNode) {
		if node.Properties != nil || containsString(typeNames(t, where, node), "object") {
			count++
			if node.AdditionalProperties == nil {
				t.Errorf("%s: object definition must set additionalProperties:false", where)
			} else if *node.AdditionalProperties {
				t.Errorf("%s: additionalProperties must be false, not true", where)
			}
		}
		if node.AdditionalProperties != nil && *node.AdditionalProperties {
			t.Errorf("%s: additionalProperties must be false, not true", where)
		}
		for _, name := range sortedSchemaKeys(node.Properties) {
			walk(where+".properties."+name, node.Properties[name])
		}
		if node.Items != nil {
			walk(where+".items", *node.Items)
		}
		for i, sub := range node.AllOf {
			walk(where+".allOf["+strconv.Itoa(i)+"]", sub)
		}
	}
	for _, name := range sortedSchemaKeys(schema.Defs) {
		walk("definitions."+name, schema.Defs[name])
	}
	for i, branch := range schema.OneOf {
		walk("oneOf["+strconv.Itoa(i)+"]", branch)
	}
	if count < 10 {
		t.Errorf("walked only %d object definitions; the walk is probably not covering the schema", count)
	}
}

// TestSchemaObservationPayloadIsUnconstrained 断言 observation.payload 不限制类型与
// 键集合：它是后端原生 JSON（json.RawMessage），服务端原样保留后映射。
func TestSchemaObservationPayloadIsUnconstrained(t *testing.T) {
	schema := readCustomSchema(t)
	payload, ok := schema.Defs["observation"].Properties["payload"]
	if !ok {
		t.Fatal("definitions.observation.properties.payload is missing")
	}
	if len(payload.Type) != 0 {
		t.Errorf("payload must not declare a JSON type (json.RawMessage 可为任意 JSON 值), got %s", payload.Type)
	}
	if payload.AdditionalProperties != nil {
		t.Error("payload must not constrain its keys: it is backend-native JSON")
	}
	if payload.Properties != nil {
		t.Error("payload must not declare properties: it is backend-native JSON")
	}
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// constValuesOfType 从 execbackend 源码里取出所有 <typeName> 类型的字符串常量值（按声明顺序）。
//
// 这些枚举（ExitReason/UsageQuality/CancelMode/ObservationKind/Capability）在 execbackend
// 里没有 All* 助手或助手可能漏项，因此直接以源码为事实来源，保证 schema 枚举漏掉一个新值
// 时本测试必红。
func constValuesOfType(t *testing.T, typeName string) []string {
	t.Helper()
	entries, err := os.ReadDir(execbackendSourceDir)
	if err != nil {
		t.Fatalf("read %s: %v", execbackendSourceDir, err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	fset := token.NewFileSet()
	var out []string
	for _, name := range names {
		file, err := parser.ParseFile(fset, filepath.Join(execbackendSourceDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				ident, ok := value.Type.(*ast.Ident)
				if !ok || ident.Name != typeName {
					continue
				}
				for i, expr := range value.Values {
					if i >= len(value.Names) {
						break
					}
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					decoded, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: const %s value %s is not a string literal: %v", name, value.Names[i].Name, lit.Value, err)
					}
					out = append(out, decoded)
				}
			}
		}
	}
	return out
}

// assertSameStrings 断言 got 与 want 逐位相等（顺序敏感）。
func assertSameStrings(t *testing.T, where string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", where, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", where, i, got[i], want[i])
		}
	}
}

// equalStrings 报告两个已排序切片是否相等。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// containsString 报告切片里是否含 value。
func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// sortedCopy 返回排序后的副本。
func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
